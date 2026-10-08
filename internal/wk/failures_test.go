package wk

import (
	"context"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// Filesystem failures must leave existing documents intact and avoid creating
// a completed-project manifest when initialization has not completed.
func TestReadOnlyFilesystem(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission checks require an unprivileged user")
	}
	root := t.TempDir()
	put(t, root, "locked/keep", "original")
	locked := filepath.Join(root, "locked")
	if err := os.Chmod(locked, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0755) })
	for _, name := range []string{"locked/new/file", "locked/keep"} {
		if err := atomicWrite(root, name, []byte("replace")); err == nil {
			t.Fatal("write succeeded on readonly directory")
		}
	}
	data, _ := os.ReadFile(filepath.Join(locked, "keep"))
	if string(data) != "original" {
		t.Fatal("changed original")
	}
	s := server(t, nil)
	a := App{Root: locked, WikiURL: s.URL, Source: "path:/wk", Cache: locked}
	if code, _, _ := invoke(t, a, "init", "--existing", "--project", "orders", "--profile", "go-service"); code != 1 {
		t.Fatal("readonly init succeeded")
	}
	if _, err := os.Stat(filepath.Join(locked, manifestFile)); !os.IsNotExist(err) {
		t.Fatal("manifest exists after failure")
	}
	if _, err := a.fetchTemplate(context.Background(), Client{Origin: s.URL}, "go-service", false, ""); err == nil {
		t.Fatal("readonly cache succeeded")
	}
	if err := os.Chmod(locked, 0000); err != nil {
		t.Fatal(err)
	}
	if _, err := componentFiles(os.DirFS(root), Component{Name: "api", Kind: "svc"}, false); err == nil {
		t.Fatal("read locked tree")
	}
}

type unreadableFS struct{ fs.FS }

func (unreadableFS) ReadFile(string) ([]byte, error) { return nil, fs.ErrPermission }

func TestComponentInputFailures(t *testing.T) {
	c := Component{Name: "api", Kind: "svc"}
	if _, err := componentFiles(unreadableFS{fstest.MapFS{"component.md": {Data: []byte("hello")}}}, c, false); err == nil {
		t.Fatal("read failure ignored")
	}
	if _, err := renderComponent(nil, Component{Name: "BAD", Kind: "svc"}, false); err == nil {
		t.Fatal("bad component accepted")
	}
	for _, path := range []string{"docs/components/_template", "docs/components/api", "docs/INDEX.md"} {
		root := t.TempDir()
		files, _ := baseFiles()
		for name, data := range files {
			put(t, root, name, string(data))
		}
		full := filepath.Join(root, path)
		if err := os.RemoveAll(full); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(t.TempDir(), full); err != nil {
			t.Fatal(err)
		}
		if err := addComponent(root, manifest("http://localhost"), c, false); err == nil {
			t.Fatal("accepted symlink", path)
		}
	}
}

func TestRemovedWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	var out, stderr strings.Builder
	if Run(context.Background(), []string{"help"}, &out, &stderr) != 1 {
		t.Fatal("ignored missing cwd")
	}
	if _, err := safePath(".", "file"); err == nil {
		t.Fatal("resolved removed cwd")
	}
	a := App{Root: "."}
	if _, err := a.init(context.Background(), nil); err == nil {
		t.Fatal("init ignored removed cwd")
	}
}

func TestGitWorktreeDiscovery(t *testing.T) {
	root := t.TempDir()
	put(t, root, "metadata/HEAD", "ref: refs/heads/main\n")
	put(t, root, "project/.git", "gitdir: ../metadata\n")
	if !hasGit(filepath.Join(root, "project")) {
		t.Fatal("relative worktree not found")
	}
	put(t, root, "project/.git", "gitdir: "+filepath.Join(root, "metadata")+"\n")
	if !hasGit(filepath.Join(root, "project")) {
		t.Fatal("absolute worktree not found")
	}
}

func TestCacheSelection(t *testing.T) {
	a := App{Cache: t.TempDir()}
	first, _ := parseTemplate([]byte(templateYAML))
	firstData, firstDigest := first.canonical()
	second := first
	second.Title = "Updated"
	secondData, secondDigest := second.canonical()
	files := []string{"go-service-" + strings.TrimPrefix(firstDigest, "sha256:") + ".yaml", "go-service-" + strings.TrimPrefix(secondDigest, "sha256:") + ".yaml"}
	put(t, a.Cache, files[0], string(firstData))
	put(t, a.Cache, files[1], string(secondData))
	for i, name := range files {
		when := time.Unix(int64(i+1), 0)
		if err := os.Chtimes(filepath.Join(a.Cache, name), when, when); err != nil {
			t.Fatal(err)
		}
	}
	tpl, err := a.cachedTemplate("go-service", "")
	if err != nil || tpl.Title != "Updated" {
		t.Fatal(tpl, err)
	}
	when := time.Unix(1, 0)
	if err := os.Chtimes(filepath.Join(a.Cache, files[1]), when, when); err != nil {
		t.Fatal(err)
	}
	if _, err := a.cachedTemplate("go-service", ""); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(a.Cache, files[0])); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(a.Cache, files[1])); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(a.Cache, files[0]), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := a.cachedTemplate("go-service", ""); err == nil {
		t.Fatal("read directory cache")
	}
}

func TestInitRejectsTemplateConflicts(t *testing.T) {
	cases := []string{
		"components: [{name: api, kind: svc}]\nfiles: [{path: docs/components/api/component.md, content: duplicate}]\n",
		"components: [{name: api, kind: svc}]\nfiles: [{path: docs/INDEX.md, content: broken}]\n",
		"policy_blocks: [{file: other.md, profile: go-service}]\n",
		"files: [{path: AGENTS.md, content: '<!-- wikisvc:begin -->'}]\n",
	}
	for _, fragment := range cases {
		s := server(t, map[string]response{"/api/v1/schema/project-templates/go-service": {200, "id: go-service\ntitle: Go\nprofile: go-service\n" + fragment, ""}})
		a := App{Root: t.TempDir(), WikiURL: s.URL, Cache: t.TempDir(), Source: "path:/wk"}
		if code, _, _ := invoke(t, a, "init", "--existing", "--project", "orders", "--template", "go-service"); code != 1 {
			t.Fatal("accepted conflicting template")
		}
	}
	s := server(t, map[string]response{"/api/v1/schema/project-templates/go-service": {404, "missing", ""}})
	a := App{Root: t.TempDir(), WikiURL: s.URL, Cache: t.TempDir(), Source: "path:/wk"}
	if code, _, _ := invoke(t, a, "init", "--project", "orders", "--template", "go-service"); code != 1 {
		t.Fatal("accepted missing template")
	}
}

func TestLiteralArguments(t *testing.T) {
	f := flags("test")
	f.String("file", "", "file")
	if err := parseFlags(f, []string{"--file=x", "--", "--json"}); err != nil || f.Arg(0) != "--json" {
		t.Fatal(err, f.Args())
	}
	s := server(t, nil)
	a := testApp(t, s)
	if code, _, _ := invoke(t, a, "propose", "--", "--json"); code != 1 {
		t.Fatal("accepted positional argument")
	}
	a.HTTP = &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) { return nil, fs.ErrPermission })}
	if code, _, _ := invoke(t, a, "doctor"); code != 1 {
		t.Fatal("ignored network failures")
	}
}
