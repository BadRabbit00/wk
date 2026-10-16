package wk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCommandsThroughSymlinkedAncestor(t *testing.T) {
	s := server(t, nil)
	parent := t.TempDir()
	alias := filepath.Join(t.TempDir(), "work")
	if err := os.Symlink(parent, alias); err != nil {
		t.Fatal(err)
	}
	a := App{Root: alias, WikiURL: s.URL, Source: "path:/wk", Cache: t.TempDir(), lookPath: func(string) (string, error) { return "/usr/bin/wk", nil }, writable: func(string) bool { return false }}
	// EvalSymlinks fails for the not-yet-existing init root under the alias.
	if code, out, stderr := invoke(t, a, "init", "--dir", "project", "--profile", "go-service", "--offline"); code != 0 {
		t.Fatal(code, out, stderr)
	}
	a.Root = filepath.Join(alias, "project")
	if code, _, stderr := invoke(t, a, "init"); code != 0 {
		t.Fatal(code, stderr)
	}
	files, _ := baseFiles()
	os.RemoveAll(filepath.Join(a.Root, "docs"))
	put(t, a.Root, "docs/INDEX.md", "# Docs\n")
	put(t, a.Root, "page.md", page)
	for _, args := range [][]string{{"policy", "sync"}, {"policy", "check"}, {"policy", "search", "Go"}, {"policy", "show", "rule-go"}, {"docs", "check"}, {"docs", "upgrade"}, {"check", "--strict"}, {"doctor"}, {"template", "list"}, {"template", "show", "go-service"}, {"propose", "--file", "page.md"}} {
		if code, out, stderr := invoke(t, a, args...); code != 0 {
			t.Fatal(args, code, out, stderr)
		}
	}
	for name, data := range files {
		if strings.HasPrefix(name, "docs/components/_template/") || name == "docs/INDEX.md" {
			put(t, a.Root, name, string(data))
		}
	}
	if code, out, stderr := invoke(t, a, "component", "add", "api"); code != 0 {
		t.Fatal(code, out, stderr)
	}
	if code, _, _ := invokeHook(t, a, "guard-paths", toolPayload(filepath.Join(parent, "project", manifestFile), nilMap())); code != 2 {
		t.Fatal(code)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(a.Root, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := safePath(a.Root, "escape/file"); err == nil {
		t.Fatal("accepted symlink inside aliased project")
	}
	// A direct symlink to the project root is also a user-owned path spelling.
	rootAlias := filepath.Join(t.TempDir(), "project")
	if err := os.Symlink(a.Root, rootAlias); err != nil {
		t.Fatal(err)
	}
	if _, err := loadManifest(rootAlias); err != nil {
		t.Fatal(err)
	}
}

func manifest(url string) Manifest {
	return Manifest{Version: 1, Project: "orders", Wiki: Wiki{URL: url, Profile: "go-service", Scopes: []string{"lang:go"}}, PolicyBlocks: []PolicyBlock{{File: "AGENTS.md", Profile: "go-service"}}, Docs: Docs{Root: "docs"}}
}

func put(t *testing.T, root, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, path), []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
}

func saveManifest(t *testing.T, root string, m Manifest) {
	t.Helper()
	data, err := yaml.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	put(t, root, manifestFile, string(data))
}

func TestManifest(t *testing.T) {
	root := t.TempDir()
	m := manifest("https://wiki.example")
	saveManifest(t, root, m)
	got, err := loadManifest(root)
	if err != nil || got.Project != "orders" {
		t.Fatalf("%+v %v", got, err)
	}
	cases := map[string]func(*Manifest){
		"reserved policy file": func(m *Manifest) { m.PolicyBlocks[0].File = manifestFile },
		"version":              func(m *Manifest) { m.Version = 2 },
		"project":              func(m *Manifest) { m.Project = "Bad_Name" },
		"origin":               func(m *Manifest) { m.Wiki.URL = "https://wiki.example/path" },
		"profile":              func(m *Manifest) { m.Wiki.Profile = "" },
		"scope":                func(m *Manifest) { m.Wiki.Scopes = []string{" "} },
		"block path":           func(m *Manifest) { m.PolicyBlocks[0].File = "../AGENTS.md" },
		"block profile":        func(m *Manifest) { m.PolicyBlocks[0].Profile = "" },
		"block duplicate":      func(m *Manifest) { m.PolicyBlocks = append(m.PolicyBlocks, m.PolicyBlocks[0]) },
		"docs path":            func(m *Manifest) { m.Docs.Root = "/docs" },
		"template id":          func(m *Manifest) { m.Template.ID = "../evil" },
		"digest":               func(m *Manifest) { m.Template.Digest = "sha256:1234" },
		"digest without id":    func(m *Manifest) { m.Template.Digest = "sha256:" + strings.Repeat("a", 64) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			m := manifest("http://localhost:8787")
			change(&m)
			saveManifest(t, root, m)
			if _, err := loadManifest(root); err == nil {
				t.Fatal("accepted invalid manifest")
			}
		})
	}
	m.Template = TemplateRef{ID: "go-service", Digest: "sha256:" + strings.Repeat("a", 64)}
	if err := m.validate(root); err != nil {
		t.Fatal(err)
	}
	for _, extra := range []string{"unknown: true\n", "api_token: value\n", "SECRET: value\n", "password: value\n", "version: 1\n"} {
		t.Run(extra, func(t *testing.T) {
			data, _ := yaml.Marshal(m)
			put(t, root, manifestFile, string(data)+extra)
			if _, err := loadManifest(root); err == nil {
				t.Fatal("accepted forbidden key")
			}
		})
	}
	for _, bad := range []string{"[", "[]", "---\nversion: 1\n---\nversion: 1", "a: &x {b: 1}\nc: *x", "{1: a}", "version: [one]", "wiki: {nested_password: value}"} {
		put(t, root, manifestFile, bad)
		if _, err := loadManifest(root); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	put(t, root, manifestFile, strings.Repeat("x", maxFileBytes+1))
	if _, err := loadManifest(root); err == nil {
		t.Fatal("accepted oversized manifest")
	}
	if _, err := loadManifest(t.TempDir()); err == nil {
		t.Fatal("accepted missing manifest")
	}
}

func TestOrigins(t *testing.T) {
	for _, raw := range []string{"https://wiki.example", "http://localhost:8787", "http://[::1]:8787"} {
		if !validOrigin(raw) {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{"https://wiki.example/", "https://wiki.example?", "https://wiki.example?q=1", "https://wiki.example#fragment", "https://wiki.example#", "https://user:pass@wiki.example", "ftp://wiki.example", "https://", "https://wiki.example:bad", "%", "https://wiki.example\n"} {
		if validOrigin(raw) {
			t.Fatal(raw)
		}
	}
}

func TestSafePaths(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"../evil", "a/../evil", "/evil", "", ".", "a//b", ".git/config", "a/.GiT/config", "C:/evil", "a\\b", "a\x00b", "a\nb"} {
		if _, err := safePath(root, path); err == nil {
			t.Fatalf("accepted %q", path)
		}
	}
	put(t, root, "a/b", "ok")
	if _, err := safePath(root, "a/b"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := safePath(root, "link/file"); err == nil {
		t.Fatal("accepted symlink parent")
	}
	if err := os.Symlink(filepath.Join(root, "a/b"), filepath.Join(root, manifestFile)); err != nil {
		t.Fatal(err)
	}
	if _, err := loadManifest(root); err == nil {
		t.Fatal("accepted symlink manifest")
	}
	if _, err := safePath(root, "a/b/file"); err == nil {
		t.Fatal("accepted non-directory parent")
	}
	if _, err := readLimited(filepath.Join(root, "a"), 5); err == nil {
		t.Fatal("read directory")
	}
}
