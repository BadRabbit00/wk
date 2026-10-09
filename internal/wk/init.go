package wk

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed all:assets
var assets embed.FS

var defaultFlake string // Set to the immutable source path by the Nix package.

func (a App) init(ctx context.Context, args []string) (result, error) {
	f := flags("init")
	dir := f.String("dir", ".", "target directory")
	origin := f.String("url", a.WikiURL, "wiki origin")
	profile := f.String("profile", "", "wiki profile")
	template := f.String("template", "", "project template")
	project := f.String("project", "", "project ID")
	scopes := f.String("scopes", "", "comma separated scopes")
	source := f.String("wk-flake", a.Source, "flake providing wk")
	existing := f.Bool("existing", false, "existing project")
	force := f.Bool("force", false, "overwrite conflicting files")
	offline := f.Bool("offline", false, "use cached template")
	if err := parseFlags(f, args); err != nil {
		return result{}, err
	}
	if f.NArg() != 0 {
		return result{}, errors.New("init не принимает позиционные аргументы")
	}
	root := *dir
	if !filepath.IsAbs(root) {
		root = filepath.Join(a.Root, root)
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return result{}, err
	}
	path, err := safePath(root, manifestFile)
	if err != nil {
		return result{}, err
	}
	old, oldErr := loadManifest(root)
	if _, err := os.Lstat(path); err == nil && !(*force) {
		if oldErr != nil {
			return result{}, oldErr
		}
		return a.installIntegrations(root, old.Project)
	}
	entries, err := os.ReadDir(root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return result{}, err
	}
	if len(entries) > 0 && !*existing {
		return result{}, errors.New("каталог не пуст; используй --existing")
	}
	if *origin == "" && oldErr == nil {
		*origin = old.Wiki.URL
	}
	if !validOrigin(*origin) {
		return result{}, errors.New("для init нужен --url ORIGIN или WIKI_URL (http/https без пути)")
	}
	if *project == "" {
		*project = filepath.Base(root)
	}
	m := Manifest{Version: 1, Project: *project, Wiki: Wiki{URL: *origin, Profile: *profile, Scopes: []string{}}, Docs: Docs{Root: "docs"}}
	bundle := a.docs
	if bundle == nil {
		bundle = assets
	}
	plan, err := readBaseFiles(bundle)
	if err != nil {
		return result{}, err
	}
	c := Client{Origin: *origin, Token: a.Token, HTTP: a.HTTP}
	if *template != "" {
		pinned := ""
		if *offline && oldErr == nil && old.Template.ID == *template {
			pinned = old.Template.Digest
		}
		t, err := a.fetchTemplate(ctx, c, *template, *offline, pinned)
		if err != nil {
			return result{}, err
		}
		_, digest := t.canonical()
		m.Template = TemplateRef{ID: t.ID, Digest: digest}
		m.Wiki.Profile = t.Profile
		m.Wiki.Scopes = t.Scopes
		m.PolicyBlocks = t.PolicyBlocks
		for _, file := range t.Files {
			if !*existing || file.Path == "AGENTS.md" || strings.HasPrefix(file.Path, "docs/") {
				if file.Path == "AGENTS.md" {
					plan[file.Path] = append(plan[file.Path], []byte("\n\n"+file.Content)...)
				} else {
					plan[file.Path] = []byte(file.Content)
				}
			}
		}
		for _, component := range t.Components {
			templateFiles := map[string][]byte{}
			for name, data := range plan {
				if strings.HasPrefix(name, "docs/components/_template/") {
					templateFiles[strings.TrimPrefix(name, "docs/components/_template/")] = data
				}
			}
			files, err := renderComponent(templateFiles, component, false)
			if err != nil {
				return result{}, err
			}
			for name, data := range files {
				key := "docs/components/" + component.Name + "/" + name
				if _, ok := plan[key]; ok {
					return result{}, errors.New("шаблон дублирует файл компонента")
				}
				plan[key] = data
			}
			index, err := addIndexRow(plan["docs/INDEX.md"], component)
			if err != nil {
				return result{}, err
			}
			plan["docs/INDEX.md"] = index
		}
	}
	if *profile != "" {
		m.Wiki.Profile = *profile
	}
	if *scopes != "" {
		m.Wiki.Scopes = strings.Split(*scopes, ",")
	}
	if len(m.PolicyBlocks) == 0 {
		m.PolicyBlocks = []PolicyBlock{{File: "AGENTS.md", Profile: m.Wiki.Profile}}
	}
	if err := m.validate(root); err != nil {
		return result{}, err
	}
	if !*existing {
		if _, ok := plan["flake.nix"]; !ok {
			if *source == "" {
				return result{}, errors.New("укажи --wk-flake или WK_FLAKE для devShell")
			}
			plan["flake.nix"] = []byte(projectFlake(*source))
		}
		ignore := strings.TrimRight(string(plan[".gitignore"]), "\n") + "\nresult\nresult-*\n.direnv\n.env\n"
		plan[".gitignore"] = []byte(strings.TrimLeft(ignore, "\n"))
	}
	// The initial block is returned by the server; wk does not compile policies.
	for _, block := range m.PolicyBlocks {
		if *existing && block.File != "AGENTS.md" && !strings.HasPrefix(block.File, "docs/") {
			return result{}, errors.New("--existing разрешает только docs/, AGENTS.md и манифест")
		}
		if *offline {
			plan[block.File] = append(plan[block.File], []byte("\n\nПолитики не загружены: offline init; требуется wk policy sync.\n")...)
			continue
		}
		compiled, err := compilePolicies(ctx, c, block)
		if err != nil {
			return result{}, err
		}
		if strings.Contains(string(plan[block.File]), "<!-- wikisvc:") {
			return result{}, errors.New("шаблон уже содержит блок политик")
		}
		text, _ := exportPolicy(string(plan[block.File]), compiled) // marker-free plan checked above
		plan[block.File] = []byte(text)
	}
	data, _ := yaml.Marshal(m)
	plan[manifestFile] = data
	integrations, notices, err := planIntegrations(root, a.integrationBundle())
	if err != nil {
		return result{}, err
	}
	// Template files cannot replace the installed harness configuration or skills.
	delete(plan, ".claude/settings.json")
	delete(plan, ".codex/hooks.json")
	for _, dir := range skillDirs {
		for _, name := range skillNames {
			delete(plan, dir+"/"+name+"/SKILL.md")
		}
	}
	for name, data := range integrations {
		plan[name] = data
	}
	if a.Token != "" {
		for name, data := range plan {
			if strings.Contains(name, a.Token) || strings.Contains(string(data), a.Token) {
				return result{}, errors.New("credential не может быть записан на диск")
			}
		}
	}
	if err := preflight(root, plan, *force); err != nil {
		return result{}, err
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		return result{}, err
	}
	if !hasGit(root) {
		if _, err := git(ctx, root, "init", "--template="); err != nil {
			return result{}, err
		}
	}
	for _, name := range sortedKeys(plan) {
		if name == manifestFile {
			continue
		}
		if err := a.write(root, name, plan[name]); err != nil {
			return result{}, err
		}
	}
	if err := a.write(root, manifestFile, plan[manifestFile]); err != nil {
		return result{}, err
	}
	return result{Data: map[string]any{"project": m.Project, "changed": true, "files": sortedKeys(plan), "template": m.Template, "integration_diff": notices}, Text: "Инициализирован проект " + m.Project + "; изменения не закоммичены", Stderr: notices}, nil
}

func baseFiles() (map[string][]byte, error) {
	return readBaseFiles(assets)
}

func readBaseFiles(bundle fs.FS) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := fs.WalkDir(bundle, "assets/docs", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(bundle, path)
		if err != nil {
			return err
		}
		files[strings.TrimPrefix(path, "assets/")] = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	snippet, err := fs.ReadFile(bundle, "assets/AGENTS-snippet.md")
	if err != nil {
		return nil, err
	}
	files["AGENTS.md"] = snippet
	return files, nil
}

func hasGit(root string) bool {
	for dir := root; ; dir = filepath.Dir(dir) {
		marker := filepath.Join(dir, ".git")
		if info, err := os.Stat(marker); err == nil {
			if !info.IsDir() {
				data, err := readLimited(marker, 4096)
				if err == nil && strings.HasPrefix(string(data), "gitdir: ") {
					marker = strings.TrimSpace(strings.TrimPrefix(string(data), "gitdir: "))
					if !filepath.IsAbs(marker) {
						marker = filepath.Join(dir, marker)
					}
				}
			}
			if info, err := os.Stat(filepath.Join(marker, "HEAD")); err == nil && info.Mode().IsRegular() {
				return true
			}
		}
		if dir == filepath.Dir(dir) {
			return false
		}
	}
}

func projectFlake(source string) string {
	// Nix interpolation must be escaped in addition to string quoting.
	quoted := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "${", "\\${", "\n", "\\n", "\r", "\\r").Replace(source)
	return fmt.Sprintf(`{
  inputs.wk.url = "%s";
  inputs.nixpkgs.follows = "wk/nixpkgs";
  outputs = { self, nixpkgs, wk }:
    let eachSystem = nixpkgs.lib.genAttrs [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
    in { devShells = eachSystem (system: {
      default = nixpkgs.legacyPackages.${system}.mkShell {
        packages = [ wk.packages.${system}.wk nixpkgs.legacyPackages.${system}.git ];
      };
    }); };
}
`, quoted)
}
