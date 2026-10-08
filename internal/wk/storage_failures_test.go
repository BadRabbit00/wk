package wk

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

type faultFile struct {
	bytes.Buffer
	writeErr, errorOnClose error
	onWrite                func()
	closed                 bool
}

func (f *faultFile) Write(data []byte) (int, error) {
	if f.onWrite != nil {
		f.onWrite()
	}
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return f.Buffer.Write(data)
}
func (f *faultFile) Close() error { f.closed = true; return f.errorOnClose }

type faultRoot struct {
	file              *faultFile
	openErr, mkdirErr error
	onMkdir           func()
	removed, renamed  bool
}

func (r *faultRoot) MkdirAll(string, os.FileMode) error {
	if r.onMkdir != nil {
		r.onMkdir()
	}
	return r.mkdirErr
}
func (r *faultRoot) OpenFile(string, int, os.FileMode) (io.WriteCloser, error) {
	return r.file, r.openErr
}
func (r *faultRoot) Remove(string) error         { r.removed = true; return nil }
func (r *faultRoot) Rename(string, string) error { r.renamed = true; return nil }

func TestAtomicStorageFailures(t *testing.T) {
	for _, step := range []string{"mkdir", "open", "write", "close", "changed parent", "changed target", "success"} {
		t.Run(step, func(t *testing.T) {
			root := t.TempDir()
			r := &faultRoot{file: &faultFile{}}
			failure := errors.New("storage failure")
			replace := func() {
				if err := os.Symlink(t.TempDir(), filepath.Join(root, "file")); err != nil {
					t.Fatal(err)
				}
			}
			switch step {
			case "mkdir":
				r.mkdirErr = failure
			case "open":
				r.openErr = failure
			case "write":
				r.file.writeErr = failure
			case "close":
				r.file.errorOnClose = failure
			case "changed parent":
				r.onMkdir = replace
			case "changed target":
				r.file.onWrite = replace
			}
			err := writeInRoot(root, r, "file", []byte("new content"))
			if step == "success" {
				if err != nil || !r.renamed || r.file.String() != "new content" {
					t.Fatal(err, r)
				}
				return
			}
			if err == nil || r.renamed {
				t.Fatal("failed write was committed")
			}
			if step == "write" || step == "close" || step == "changed target" {
				if !r.removed || !r.file.closed {
					t.Fatal("temporary file leaked")
				}
			}
		})
	}
}

func TestMissingBundledDocs(t *testing.T) {
	for _, bundle := range []fs.FS{fstest.MapFS{}, fstest.MapFS{"assets/docs/INDEX.md": {Data: []byte("index")}}, unreadableFS{fstest.MapFS{"assets/docs/INDEX.md": {Data: []byte("index")}}}} {
		if _, err := readBaseFiles(bundle); err == nil {
			t.Fatal("accepted broken assets")
		}
	}
	a := App{Root: t.TempDir(), WikiURL: "http://localhost", Source: "path:/wk", docs: fstest.MapFS{}}
	if code, _, _ := invoke(t, a, "init", "--project", "orders", "--profile", "go-service"); code != 1 {
		t.Fatal("ignored missing bundle")
	}
	s := server(t, map[string]response{"/api/v1/schema/project-templates/go-service": {200, "id: go-service\ntitle: Go\nprofile: go-service\ncomponents: [{name: api, kind: svc}]\n", ""}})
	a.WikiURL = s.URL
	a.Cache = t.TempDir()
	a.docs = fstest.MapFS{"assets/docs/INDEX.md": {Data: []byte("index")}, "assets/AGENTS-snippet.md": {Data: []byte("instructions")}}
	if code, _, _ := invoke(t, a, "init", "--project", "orders", "--template", "go-service"); code != 1 {
		t.Fatal("accepted missing component template")
	}
}

func TestInitWriteFailures(t *testing.T) {
	s := server(t, nil)
	for _, failOn := range []string{"AGENTS.md", manifestFile} {
		a := App{Root: t.TempDir(), WikiURL: s.URL, Source: "path:/wk"}
		var writes []string
		a.writeFile = func(root, name string, data []byte) error {
			writes = append(writes, name)
			if name == failOn {
				return fs.ErrPermission
			}
			return atomicWrite(root, name, data)
		}
		if code, _, _ := invoke(t, a, "init", "--project", "orders", "--profile", "go-service"); code != 1 {
			t.Fatal("ignored write failure")
		}
		if writes[len(writes)-1] != failOn {
			t.Fatal(writes)
		}
		if _, err := os.Stat(filepath.Join(a.Root, manifestFile)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("completed manifest persisted on failure")
		}
	}
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a := App{Root: root, WikiURL: s.URL, Source: "path:/wk"}
	if _, err := a.init(ctx, []string{"--project", "orders", "--profile", "go-service", "--offline"}); err == nil {
		t.Fatal("ignored git failure")
	}
	if os.Geteuid() != 0 {
		if err := os.Chmod(root, 0500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(root, 0755) })
		a.Root = filepath.Join(root, "missing")
		if code, _, _ := invoke(t, a, "init", "--project", "orders", "--profile", "go-service", "--offline"); code != 1 {
			t.Fatal("ignored mkdir failure")
		}
	}
}

func TestComponentWriteFailures(t *testing.T) {
	root := t.TempDir()
	files, _ := baseFiles()
	for name, data := range files {
		put(t, root, name, string(data))
	}
	m := manifest("http://localhost")
	c := Component{Name: "api", Kind: "svc"}
	put(t, root, "docs/components/_template/invalid:name.md", "data")
	if err := addComponent(root, m, c, false); err == nil {
		t.Fatal("ignored preflight failure")
	}
	if err := os.Remove(filepath.Join(root, "docs/components/_template/invalid:name.md")); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() != 0 {
		path := filepath.Join(root, "docs/components")
		if err := os.Chmod(path, 0500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0755) })
		if err := addComponent(root, m, c, false); err == nil {
			t.Fatal("ignored write failure")
		}
	}
}

type vanishedEntry struct{ os.DirEntry }

func (vanishedEntry) Name() string               { return "go-service-vanished.yaml" }
func (vanishedEntry) Info() (fs.FileInfo, error) { return nil, fs.ErrNotExist }

func TestCacheDirectoryRace(t *testing.T) {
	a := App{Cache: t.TempDir(), readCache: func(string) ([]os.DirEntry, error) { return []os.DirEntry{vanishedEntry{}}, nil }}
	if _, err := a.cachedTemplate("go-service", ""); err == nil {
		t.Fatal("ignored vanished cache entry")
	}
}

func TestInvalidCacheEnvironment(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "relative")
	t.Chdir(t.TempDir())
	var out, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"help"}, &out, &stderr); code != 0 {
		t.Fatal(code)
	}
}

func TestInitDirectoryUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission checks require an unprivileged user")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0300); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0755) })
	a := App{Root: root, WikiURL: "http://localhost", Source: "path:/wk"}
	if code, _, _ := invoke(t, a, "init", "--project", "orders", "--profile", "go-service"); code != 1 {
		t.Fatal("read inaccessible directory")
	}
}

func TestEncodedCredentialTemplate(t *testing.T) {
	token := t.Name()
	escaped := "\\x" + "54" + token[1:]
	s := server(t, map[string]response{"/api/v1/schema/project-templates/go-service": {200, templateYAML + "scopes: [\"" + escaped + "\"]\n", ""}})
	a := testApp(t, s)
	if code, _, _ := invoke(t, a, "template", "show", "go-service"); code != 1 {
		t.Fatal("cached an encoded credential")
	}
	entries, _ := os.ReadDir(a.Cache)
	if len(entries) != 0 {
		t.Fatal("secret cache file exists")
	}
	if !strings.HasPrefix(token, "T") {
		t.Fatal("test token assumption changed")
	}
}
