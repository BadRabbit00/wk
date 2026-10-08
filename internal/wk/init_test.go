package wk

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestInitAndComponent(t *testing.T) {
	s := server(t, nil)
	a := testApp(t, s)
	a.WikiURL = s.URL
	args := []string{"init", "--dir", "orders", "--profile", "go-service", "--scopes", "lang:go", "--json"}
	code, out, err := invoke(t, a, args...)
	if code != 0 || err != "" || !json.Valid([]byte(out)) {
		t.Fatalf("%d %s %s", code, out, err)
	}
	root := filepath.Join(a.Root, "orders")
	for _, name := range []string{manifestFile, "AGENTS.md", "flake.nix", ".gitignore", "docs/INDEX.md", "docs/components/_template/ai.md", "docs/_meta/ids.md", ".git/HEAD"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatal(name, err)
		}
	}
	info, errRead := os.Stat(filepath.Join(root, manifestFile))
	if errRead != nil {
		t.Fatal(errRead)
	}
	code, _, err = invoke(t, a, args...)
	if code != 0 {
		t.Fatal(err)
	}
	now, _ := os.Stat(filepath.Join(root, manifestFile))
	if now.ModTime() != info.ModTime() {
		t.Fatal("idempotent init changed manifest")
	}
	data, _ := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if !strings.Contains(string(data), "<!-- wikisvc:begin version=a3f91c2e -->\n# Policies\n\nUse Go.\n<!-- wikisvc:end -->") {
		t.Fatal(string(data))
	}
	a.Root = root
	for _, kind := range []string{"svc", "lib"} {
		args := []string{"component", "add", "api-" + kind, "--kind", kind, "--lang=LANG-go"}
		if kind == "lib" {
			args = append(args, "--with-ai")
		}
		if code, _, err := invoke(t, a, args...); code != 0 {
			t.Fatal(err)
		}
		if code, _, _ := invoke(t, a, args...); code != 1 {
			t.Fatal("duplicate accepted")
		}
		path := filepath.Join(root, "docs/components/api-"+kind)
		data, _ := os.ReadFile(filepath.Join(path, "component.md"))
		for _, want := range []string{strings.ToUpper(kind) + "-api-" + kind, "status: draft", "lang: LANG-go"} {
			if !strings.Contains(string(data), want) {
				t.Fatal(want, string(data))
			}
		}
		data, _ = os.ReadFile(filepath.Join(path, "contracts.md"))
		if !strings.Contains(string(data), "owner: "+strings.ToUpper(kind)+"-api-"+kind) {
			t.Fatal(string(data))
		}
		_, err := os.Stat(filepath.Join(path, "ai.md"))
		if (err == nil) != (kind == "lib") {
			t.Fatal("with-ai mismatch")
		}
	}
	index, _ := os.ReadFile(filepath.Join(root, "docs/INDEX.md"))
	if !strings.Contains(string(index), "| SVC-api-svc | svc | LANG-go |") {
		t.Fatal(string(index))
	}
	if code, _, _ := invoke(t, a, "component", "add", "../evil"); code != 1 {
		t.Fatal("accepted invalid name")
	}
	if _, err := git(context.Background(), root, "status", "--porcelain"); err != nil {
		t.Fatal(err)
	}
}

func TestInitErrors(t *testing.T) {
	s := server(t, nil)
	cases := []struct {
		name    string
		args    []string
		prepare func(*testing.T, *App)
	}{
		{"bad flag", []string{"--wat"}, nil},
		{"extra", []string{"extra"}, nil},
		{"no url", nil, func(t *testing.T, a *App) { a.WikiURL = "" }},
		{"bad profile", nil, func(t *testing.T, a *App) { a.WikiURL = s.URL }},
		{"bad project", []string{"--profile", "go-service", "--project", "BAD"}, nil},
		{"missing flake", []string{"--profile", "go-service"}, func(t *testing.T, a *App) { a.Source = "" }},
		{"nonempty", []string{"--profile", "go-service"}, func(t *testing.T, a *App) { put(t, a.Root, "README.md", "existing") }},
		{"invalid manifest", nil, func(t *testing.T, a *App) { put(t, a.Root, manifestFile, "broken") }},
		{"symlink manifest", nil, func(t *testing.T, a *App) {
			if err := os.Symlink("missing", filepath.Join(a.Root, manifestFile)); err != nil {
				t.Fatal(err)
			}
		}},
		{"conflicts", []string{"--profile", "go-service", "--existing"}, func(t *testing.T, a *App) { put(t, a.Root, "AGENTS.md", "keep") }},
		{"symlink docs", []string{"--profile", "go-service", "--existing", "--force"}, func(t *testing.T, a *App) {
			if err := os.Symlink(t.TempDir(), filepath.Join(a.Root, "docs")); err != nil {
				t.Fatal(err)
			}
		}},
		{"directory file", []string{"--profile", "go-service", "--existing", "--force"}, func(t *testing.T, a *App) {
			if err := os.Mkdir(filepath.Join(a.Root, "AGENTS.md"), 0755); err != nil {
				t.Fatal(err)
			}
		}},
		{"secret source", []string{"--profile", "go-service"}, func(t *testing.T, a *App) { a.Source = "path:" + a.Token }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := App{Root: t.TempDir(), WikiURL: s.URL, Source: "path:/wk", Token: "", Cache: t.TempDir()}
			if tc.name == "secret source" {
				a.Token = "credential"
				a.HTTP = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
					r.Header.Del("Authorization")
					return http.DefaultTransport.RoundTrip(r)
				})}
			}
			if tc.prepare != nil {
				tc.prepare(t, &a)
			}
			if code, _, err := invoke(t, a, append([]string{"init"}, tc.args...)...); code != 1 || err == "" {
				t.Fatalf("%d %s", code, err)
			}
		})
	}
}

func TestExistingAndOfflineInit(t *testing.T) {
	s := server(t, nil)
	a := App{Root: t.TempDir(), WikiURL: s.URL, Source: "path:/wk", Cache: filepath.Join(t.TempDir(), "cache")}
	put(t, a.Root, "main.go", "existing code")
	put(t, a.Root, ".gitignore", "mine\n")
	put(t, a.Root, "flake.nix", "mine\n")
	if code, _, err := invoke(t, a, "init", "--existing", "--profile", "go-service", "--project", "existing"); code != 0 {
		t.Fatal(err)
	}
	for _, name := range []string{".gitignore", "flake.nix"} {
		data, _ := os.ReadFile(filepath.Join(a.Root, name))
		if string(data) != "mine\n" {
			t.Fatal("changed existing file", name)
		}
	}
	data, _ := os.ReadFile(filepath.Join(a.Root, "main.go"))
	if string(data) != "existing code" {
		t.Fatal("changed code")
	}
	if code, _, err := invoke(t, a, "init", "--existing", "--force", "--profile", "go-service", "--project", "existing", "--offline"); code != 0 {
		t.Fatal(err)
	}
	if code, _, err := invoke(t, a, "init", "--existing", "--force", "--project", "existing", "--template", "go-service"); code != 0 {
		t.Fatal(err)
	}
	a.WikiURL = ""
	if code, _, err := invoke(t, a, "init", "--existing", "--force", "--project", "existing", "--template", "go-service", "--offline"); code != 0 {
		t.Fatal(err)
	}
	if !hasGit(a.Root) || !hasGit(filepath.Join(a.Root, "docs")) {
		t.Fatal("did not find git")
	}
}

func TestTemplateInit(t *testing.T) {
	remote := strings.Replace(templateYAML, "files:", "components: [{name: api, kind: svc, lang: LANG-go}]\nfiles:", 1)
	s := server(t, map[string]response{"/api/v1/schema/project-templates/go-service": {200, remote, ""}})
	a := App{Root: t.TempDir(), WikiURL: s.URL, Source: "path:/wk", Cache: filepath.Join(t.TempDir(), "cache")}
	if code, out, err := invoke(t, a, "init", "--dir", "service", "--template", "go-service"); code != 0 {
		t.Fatalf("%s %s", out, err)
	}
	root := filepath.Join(a.Root, "service")
	m, err := loadManifest(root)
	if err != nil || !strings.HasPrefix(m.Template.Digest, "sha256:") {
		t.Fatal(m, err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "main.go")); err != nil || string(data) != "package main" {
		t.Fatal(string(data), err)
	}
	if _, err := os.Stat(filepath.Join(root, "docs/components/api/component.md")); err != nil {
		t.Fatal(err)
	}
	a.WikiURL = "http://127.0.0.1:1"
	if code, _, err := invoke(t, a, "init", "--dir", "offline", "--template", "go-service", "--offline"); code != 0 {
		t.Fatal(err)
	}
}

func TestComponentFailures(t *testing.T) {
	root := t.TempDir()
	m := manifest("http://localhost")
	files, err := baseFiles()
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		put(t, root, name, string(data))
	}
	c := Component{Name: "api", Kind: "svc"}
	if _, err := componentFiles(fstest.MapFS{}, c, false); err == nil {
		t.Fatal("empty template")
	}
	if _, err := componentFiles(os.DirFS(filepath.Join(root, "missing")), c, false); err == nil {
		t.Fatal("missing template")
	}
	if _, err := componentFiles(fstest.MapFS{"component.md": {Data: []byte("hi")}}, Component{Name: "BAD", Kind: "svc"}, false); err == nil {
		t.Fatal("bad component")
	}
	if _, err := renderComponent(map[string][]byte{"component.md": []byte(strings.Repeat("x", maxFileBytes+1))}, c, false); err == nil {
		t.Fatal("oversized component")
	}
	for _, data := range []string{"none", "## Компоненты\n\nnone", "## Компоненты\n| header |\n", "## Компоненты\n| h |\n|---|\n| SVC-api |\n"} {
		if _, err := addIndexRow([]byte(data), c); err == nil {
			t.Fatal("accepted broken index")
		}
	}
	if _, err := addIndexRow([]byte("## Компоненты\n| h |\n|---|\n\n## Next"), c); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "docs/components/_template/link")); err != nil {
		t.Fatal(err)
	}
	if err := addComponent(root, m, c, false); err == nil {
		t.Fatal("accepted symlink template")
	}
	if err := os.Remove(filepath.Join(root, "docs/components/_template/link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "docs/INDEX.md")); err != nil {
		t.Fatal(err)
	}
	if err := addComponent(root, m, c, false); err == nil {
		t.Fatal("missing index")
	}
	put(t, root, "docs/INDEX.md", "broken")
	if err := addComponent(root, m, c, false); err == nil {
		t.Fatal("invalid index")
	}
	if _, err := fs.Stat(assets, "assets/docs/INDEX.md"); err != nil {
		t.Fatal(err)
	}
}

func TestInitCompileFailures(t *testing.T) {
	for _, r := range []response{{500, "oops", ""}, {200, `{"version":"bad","markdown":"text"}`, ""}, {200, `{"version":"12345678","markdown":"<!-- wikisvc:begin -->"}`, ""}} {
		s := server(t, map[string]response{"/api/v1/policies/compile": r})
		a := App{Root: t.TempDir(), WikiURL: s.URL, Source: "path:/wk"}
		if code, _, _ := invoke(t, a, "init", "--profile", "go-service", "--project", "orders"); code != 1 {
			t.Fatal("accepted invalid policies")
		}
		entries, _ := os.ReadDir(a.Root)
		if len(entries) != 0 {
			t.Fatal("wrote files before validation")
		}
	}
}

func TestProjectFlakeEscaping(t *testing.T) {
	flake := projectFlake("path:/a\"${boom}\\b\n\r")
	if !strings.Contains(flake, `path:/a\"\${boom}\\b\n\r`) {
		t.Fatal(flake)
	}
}

func TestTableDraftStatuses(t *testing.T) {
	input := "| id | status |\n|---|---|\n| CFG-a | <…> |\n| CFG-b | planned |\n|\n\n| unrelated |\n"
	want := strings.Replace(input, "<…>", "draft", 1)
	if got := draftTableStatuses(input); got != want {
		t.Fatalf("%q", got)
	}
	files, _ := baseFiles()
	rendered, err := renderComponent(map[string][]byte{"component.md": files["docs/components/_template/component.md"], "runtime.md": files["docs/components/_template/runtime.md"]}, Component{Name: "api", Kind: "svc"}, false)
	if err != nil || !strings.Contains(string(rendered["runtime.md"]), "| draft |") {
		t.Fatal(err, string(rendered["runtime.md"]))
	}
}

func TestBootstrapRun(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("WIKI_TOKEN", "secret")
	t.Setenv("WK_FLAKE", "")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	var out, err strings.Builder
	if code := Run(context.Background(), []string{"help"}, &out, &err); code != 0 {
		t.Fatal(code)
	}
	t.Setenv("WK_FLAKE", "path:/wk")
	if code := Run(context.Background(), []string{"--json", "help"}, &out, &err); code != 0 {
		t.Fatal(code)
	}
}
