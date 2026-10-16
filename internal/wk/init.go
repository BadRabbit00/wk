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
	keep := f.Bool("keep", false, "keep every existing file, add only missing ones")
	offline := f.Bool("offline", false, "use cached template")
	if err := parseFlags(f, args); err != nil {
		return result{}, err
	}
	if f.NArg() != 0 {
		return result{}, errors.New("init не принимает позиционные аргументы")
	}
	if *keep && *force {
		return result{}, errors.New("--keep и --force несовместимы")
	}
	*existing = *existing || *keep
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
		return a.installIntegrations(root, old.Project, *source)
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
		ignore := strings.TrimRight(string(plan[".gitignore"]), "\n") + "\nresult\nresult-*\n.direnv\n.env\n"
		plan[".gitignore"] = []byte(strings.TrimLeft(ignore, "\n"))
	}
	toolchain, notices, err := toolchainFiles(root, *source)
	if err != nil {
		return result{}, err
	}
	if _, ok := plan["flake.nix"]; ok {
		notices = nil // the template brings its own flake
	}
	for name, data := range toolchain {
		if _, ok := plan[name]; !ok {
			plan[name] = data
		}
	}
	// --keep adopts a project as it is: nothing that exists is rewritten.
	kept := map[string]bool{}
	if *keep {
		for _, name := range sortedKeys(plan) {
			_, info, err := inspectPath(root, name)
			if err != nil {
				return result{}, fmt.Errorf("%s: %w", name, err)
			}
			if info != nil {
				kept[name] = true
				delete(plan, name)
			}
		}
	}
	// The initial block is returned by the server; wk does not compile policies.
	for _, block := range m.PolicyBlocks {
		if *existing && block.File != "AGENTS.md" && !strings.HasPrefix(block.File, "docs/") {
			return result{}, errors.New("--existing разрешает только docs/, AGENTS.md и манифест")
		}
		if kept[block.File] {
			notices = append(notices, block.File+" сохранён без изменений; блок политик добавит `wk policy sync`.")
			continue
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
	integrations, diff, err := planIntegrations(root, a.integrationBundle())
	if err != nil {
		return result{}, err
	}
	if diff != "" {
		notices = append(notices, diff)
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
	text := "Инициализирован проект " + m.Project + "; изменения не закоммичены"
	if len(kept) > 0 {
		text += fmt.Sprintf("; существующих файлов сохранено без изменений: %d", len(kept))
	}
	return result{Data: map[string]any{"project": m.Project, "changed": true, "files": sortedKeys(plan), "kept": sortedKeys(kept), "template": m.Template, "integration_diff": diff}, Text: text, Stderr: strings.Join(notices, "\n")}, nil
}

// toolchainFiles returns the missing files that make the checks run: the CI
// gate always, and with a wk flake source also flake.nix and .envrc, so Nix
// users get the pinned wk on PATH. Without a source wk comes from the installed
// package. Existing files belong to the project and are only inspected.
func toolchainFiles(root, source string) (map[string][]byte, []string, error) {
	files := map[string][]byte{}
	var notices []string
	if _, info, err := inspectPath(root, ciWorkflowFile); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", ciWorkflowFile, err)
	} else if info == nil {
		files[ciWorkflowFile] = []byte(ciWorkflow(Version))
	}
	if source == "" {
		return files, nil, nil
	}
	full, info, err := inspectPath(root, "flake.nix")
	if err != nil {
		return nil, nil, fmt.Errorf("flake.nix: %w", err)
	}
	if info == nil {
		files["flake.nix"] = []byte(projectFlake(source))
	} else {
		data, err := readLimited(full, 1<<20)
		if err != nil {
			return nil, nil, fmt.Errorf("flake.nix: %w", err)
		}
		if !strings.Contains(string(data), "wk.packages") {
			notices = append(notices, "flake.nix сохранён: добавь wk.packages.${system}.wk в devShell, иначе хуки не найдут wk в PATH.")
		}
	}
	if _, info, err := inspectPath(root, ".envrc"); err != nil {
		return nil, nil, fmt.Errorf(".envrc: %w", err)
	} else if info == nil {
		files[".envrc"] = []byte("use flake\n")
	}
	return files, notices, nil
}

const ciWorkflowFile = ".github/workflows/wk.yml"
const releaseRepo = "BadRabbit00/wk"

// ciWorkflow pins the release that created the project; a dev build takes the latest.
func ciWorkflow(version string) string {
	tag := ""
	if version != "dev" {
		tag = "v" + strings.TrimPrefix(version, "v") + " "
	}
	return `# Created by wk init. Hooks give agents fast feedback; this job is the gate
# no agent can bypass. wk refuses agent edits to this file: a human changes it.
name: wk
on:
  push:
  pull_request:
permissions:
  contents: read
jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Install wk
        env:
          GH_TOKEN: ${{ github.token }}
        run: |
          gh release download ` + tag + `--repo ` + releaseRepo + ` --pattern 'wk_*_linux_amd64.tar.gz' --pattern checksums.txt
          sha256sum --check --ignore-missing checksums.txt
          gh attestation verify wk_*_linux_amd64.tar.gz --repo ` + releaseRepo + `
          tar -xzf wk_*_linux_amd64.tar.gz wk
          sudo install -m 0755 wk /usr/local/bin/wk
      - name: wk check --strict
        env:
          WIKI_TOKEN: ${{ secrets.WIKI_TOKEN }}
        # Exit 3: documentation is clean and the wiki is unreachable from CI;
        # documentation errors still fail with 1.
        run: wk check --strict || test $? -eq 3
`
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
