package wk

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestDocsPythonGolden(t *testing.T) {
	cases, err := os.ReadDir("../../testdata/golden")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range cases {
		t.Run(entry.Name(), func(t *testing.T) {
			root := t.TempDir()
			if err := os.CopyFS(root, os.DirFS("../../testdata/example")); err != nil {
				t.Fatal(err)
			}
			base := filepath.Join("../../testdata/golden", entry.Name())
			var mutations map[string]string
			data, err := os.ReadFile(filepath.Join(base, "mutation.json"))
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &mutations); err != nil {
				t.Fatal(err)
			}
			for name, appendix := range mutations {
				old, _ := os.ReadFile(filepath.Join(root, name))
				put(t, root, name, string(old)+appendix)
			}
			m := manifest("http://wiki.invalid")
			m.PolicyBlocks = nil
			saveManifest(t, root, m)
			a := App{Root: root}
			args := []string{"docs", "check"}
			if entry.Name() == "strict" {
				args = append(args, "--strict")
			}
			code, out, stderr := invoke(t, a, args...)
			expected, _ := os.ReadFile(filepath.Join(base, "stdout.txt"))
			exit, _ := os.ReadFile(filepath.Join(base, "exit.txt"))
			wantCode, _ := strconv.Atoi(string(exit))
			if stderr != "" || code != wantCode || out != string(expected) {
				t.Fatalf("exit=%d want=%d stderr=%s\nstdout:\n%s\nexpected:\n%s", code, wantCode, stderr, out, expected)
			}
			actual, err := os.ReadFile(filepath.Join(root, "docs/_meta/registry.md"))
			if err != nil {
				t.Fatal(err)
			}
			registry, _ := os.ReadFile(filepath.Join(base, "registry.md"))
			if string(actual) != string(registry) {
				t.Fatalf("registry differs from Python oracle\n%s", actual)
			}
		})
	}
}

func TestDocsFilesystemFailures(t *testing.T) {
	root := t.TempDir()
	m := manifest("http://wiki.invalid")
	m.PolicyBlocks = nil
	a := App{Root: root}
	if _, err := readDocs(filepath.Join(root, "missing")); err == nil {
		t.Fatal("missing tree")
	}
	put(t, root, "docs/good.md", "# Good\n")
	if err := os.Symlink("missing", filepath.Join(root, "docs/broken.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := readDocs(filepath.Join(root, "docs")); err == nil {
		t.Fatal("broken doc link")
	}
	os.Remove(filepath.Join(root, "docs/broken.md"))
	if _, err := a.docsReport(m, strings.Repeat("x", 5000), ".", false); err == nil {
		t.Fatal("invalid path")
	}
	original := resolveDocsPath
	resolveDocsPath = func(string) (string, error) { return "", errors.New("directory vanished during resolution") }
	if _, err := a.docsReport(m, "docs", ".", false); err == nil {
		t.Fatal("resolution ignored")
	}
	resolveDocsPath = original
	m.PolicyBlocks = []PolicyBlock{{File: "AGENTS.md", Profile: "go-service"}}
	put(t, root, "AGENTS.md", "<!-- wikisvc:begin")
	if _, err := a.docsReport(m, "docs", ".", false); err == nil {
		t.Fatal("invalid policy ignored")
	}
	m.PolicyBlocks = nil
	a.writeFile = func(string, string, []byte) error { return errors.New("disk full") }
	if _, err := a.docsReport(m, "docs", ".", true); err == nil {
		t.Fatal("failed registry ignored")
	}
	bad := m
	bad.Docs.Root = "../escape"
	if _, err := a.docsCommand(bad, []string{"upgrade"}); err == nil {
		t.Fatal("upgrade escaped")
	}
	put(t, root, "docs/_meta/check.py/file", "not an empty directory")
	if _, err := a.docsCommand(m, []string{"upgrade"}); err == nil {
		t.Fatal("removed nonempty directory")
	}
	readCode(filepath.Join(root, "missing"), "")
	readCode(filepath.Join(root, "docs/good.md"), "") // not a directory
	put(t, root, "unreadable/code.go", "// doc: ENT-secret")
	os.Chmod(filepath.Join(root, "unreadable"), 0000)
	t.Cleanup(func() { os.Chmod(filepath.Join(root, "unreadable"), 0755) })
	readCode(filepath.Join(root, "unreadable"), "")
	put(t, root, "hidden.go", "// doc: ENT-secret")
	os.Chmod(filepath.Join(root, "hidden.go"), 0000)
	os.Symlink("missing", filepath.Join(root, "broken.go"))
	files := readCode(root, filepath.Join(root, "docs"))
	for _, file := range files {
		if file.Path == "hidden.go" || file.Path == "broken.go" || strings.HasPrefix(file.Path, "docs/") {
			t.Fatal(file)
		}
	}
}

func TestDocsCommands(t *testing.T) {
	s := server(t, nil)
	a := testApp(t, s)
	for _, args := range [][]string{{"docs"}, {"docs", "bad"}, {"docs", "check", "--bad"}, {"docs", "check", "arg"}, {"docs", "upgrade", "arg"}, {"check", "--bad"}, {"check", "arg"}} {
		if code, _, _ := invoke(t, a, args...); code != 2 {
			t.Fatalf("%v: %d", args, code)
		}
	}
	if code, _, _ := invoke(t, a, "docs", "check"); code != 3 {
		t.Fatal(code)
	}
	put(t, a.Root, "docs/system/domain.md", "## ENT-order — Order\nstatus: draft\n")
	if code, out, _ := invoke(t, a, "docs", "check", "--json", "--no-registry"); code != 0 || !json.Valid([]byte(out)) {
		t.Fatal(code, out)
	}
	if _, err := os.Stat(filepath.Join(a.Root, "docs/_meta/registry.md")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, present := range []bool{true, false} {
		if present {
			put(t, a.Root, "docs/_meta/check.py", "obsolete")
		}
		if code, out, _ := invoke(t, a, "docs", "upgrade"); code != 0 || out != "проверка теперь встроена в wk\n" {
			t.Fatal(code, out)
		}
	}
	if code, _, _ := invoke(t, a, "policy", "sync"); code != 0 {
		t.Fatal(code)
	}
	if code, _, err := invoke(t, a, "check", "--strict"); code != 0 {
		t.Fatal(code, err)
	}
	put(t, a.Root, "docs/broken.md", "ENT-missing\n")
	if code, out, _ := invoke(t, a, "check"); code != 1 || !strings.Contains(out, "ENT-missing") {
		t.Fatal(code, out)
	}
	if code, out, _ := invoke(t, a, "doctor"); code != 1 || !strings.Contains(out, "fail docs") {
		t.Fatal(code, out)
	}
	os.Remove(filepath.Join(a.Root, "docs/broken.md"))
	put(t, a.Root, "docs/system/domain.md", "## ENT-order — Order\nstatus: draft\n"+strings.Repeat("\n", 400))
	if code, out, _ := invoke(t, a, "doctor"); code != 0 || !strings.Contains(out, "warn docs") {
		t.Fatal(code, out)
	}
	for _, args := range [][]string{{"docs", "check"}, {"check"}} {
		if code, _, _ := invoke(t, App{Root: t.TempDir()}, args...); code != 3 {
			t.Fatal(code)
		}
	}
	put(t, a.Root, "docs/bad.md", string([]byte{0xff}))
	if _, err := a.docsReport(manifest(s.URL), "docs", ".", true); err == nil {
		t.Fatal("invalid UTF8 accepted")
	}
	if _, err := a.check(context.Background(), Client{Origin: s.URL}, manifest(s.URL), nil); err == nil {
		t.Fatal("bad docs accepted")
	}
}
