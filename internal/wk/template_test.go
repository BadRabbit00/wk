package wk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const templateYAML = "id: go-service\ntitle: Go service\nprofile: go-service\nfiles:\n  - path: main.go\n    content: package main\n"

func TestTemplateValidation(t *testing.T) {
	base, err := parseTemplate([]byte(templateYAML))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*ProjectTemplate){
		"id":         func(v *ProjectTemplate) { v.ID = "../evil" },
		"profile":    func(v *ProjectTemplate) { v.Profile = "" },
		"title":      func(v *ProjectTemplate) { v.Title = "" },
		"traversal":  func(v *ProjectTemplate) { v.Files[0].Path = "../evil" },
		"absolute":   func(v *ProjectTemplate) { v.Files[0].Path = "/evil" },
		"git":        func(v *ProjectTemplate) { v.Files[0].Path = ".git/hooks/post-checkout" },
		"manifest":   func(v *ProjectTemplate) { v.Files[0].Path = manifestFile },
		"duplicate":  func(v *ProjectTemplate) { v.Files = append(v.Files, v.Files[0]) },
		"ancestor":   func(v *ProjectTemplate) { v.Files = append(v.Files, TemplateFile{Path: "main.go/child"}) },
		"file count": func(v *ProjectTemplate) { v.Files = make([]TemplateFile, 300) },
		"file bytes": func(v *ProjectTemplate) { v.Files[0].Content = strings.Repeat("a", 1<<20) },
		"total bytes": func(v *ProjectTemplate) {
			v.Files = nil
			for i := 0; i < 9; i++ {
				v.Files = append(v.Files, TemplateFile{Path: string(rune('a' + i)), Content: strings.Repeat("a", maxFileBytes)})
			}
		},
		"component name":     func(v *ProjectTemplate) { v.Components = []Component{{Name: "../x", Kind: "svc"}} },
		"component kind":     func(v *ProjectTemplate) { v.Components = []Component{{Name: "api", Kind: "invalid"}} },
		"component language": func(v *ProjectTemplate) { v.Components = []Component{{Name: "api", Kind: "svc", Lang: "go"}} },
		"component duplicate": func(v *ProjectTemplate) {
			v.Components = []Component{{Name: "api", Kind: "svc"}, {Name: "api", Kind: "svc"}}
		},
		"policy path":    func(v *ProjectTemplate) { v.PolicyBlocks = []PolicyBlock{{File: "../file", Profile: "go-service"}} },
		"policy profile": func(v *ProjectTemplate) { v.PolicyBlocks = []PolicyBlock{{File: "AGENTS.md"}} },
		"policy duplicate": func(v *ProjectTemplate) {
			v.PolicyBlocks = []PolicyBlock{{File: "AGENTS.md", Profile: "go-service"}, {File: "AGENTS.md", Profile: "go-service"}}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			v := base
			v.Files = append([]TemplateFile{}, base.Files...)
			mutate(&v)
			data, _ := yaml.Marshal(v)
			if _, err := parseTemplate(data); err == nil {
				t.Fatal("accepted unsafe template")
			}
		})
	}
	for _, field := range []string{"post_init: echo pwn", "exec: ls", "script: run", "command: run", "files: [{path: safe, content: text, mode: symlink}]"} {
		if _, err := parseTemplate([]byte(templateYAML + field)); err == nil {
			t.Fatal(field)
		}
	}
	if _, err := parseTemplate([]byte(strings.Repeat("a", 4*maxTemplateBytes+1))); err == nil {
		t.Fatal("accepted oversized response")
	}
	a, da := base.canonical()
	other, err := parseTemplate([]byte("files: [{content: package main, path: main.go}]\nprofile: go-service\ntitle: Go service\nid: go-service\n"))
	if err != nil {
		t.Fatal(err)
	}
	b, db := other.canonical()
	if string(a) != string(b) || da != db {
		t.Fatal("digest depends on key order")
	}
}

func TestPreflightAndWrites(t *testing.T) {
	root := t.TempDir()
	if err := preflight(root, map[string][]byte{"docs": nil, "docs/INDEX.md": nil}, true); err == nil {
		t.Fatal("accepted conflicting plan")
	}
	files := map[string][]byte{"a/b": []byte("hello"), "c": []byte("world")}
	if err := preflight(root, files, false); err != nil {
		t.Fatal(err)
	}
	for path, data := range files {
		if err := atomicWrite(root, path, data); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(filepath.Join(root, "a/b"))
	if err != nil || string(data) != "hello" {
		t.Fatalf("%s %v", data, err)
	}
	if err := preflight(root, files, false); err == nil || !strings.Contains(err.Error(), "a/b, c") {
		t.Fatal(err)
	}
	if err := preflight(root, files, true); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(root, "a/b", []byte("updated")); err != nil {
		t.Fatal(err)
	}
	for _, plan := range []map[string][]byte{{"../evil": nil}, {"a": nil}, {"c/child": nil}} {
		if err := preflight(root, plan, true); err == nil {
			t.Fatal("accepted unsafe write")
		}
	}
	if err := atomicWrite(root, "../evil", nil); err == nil {
		t.Fatal("unsafe write")
	}
	if err := atomicWrite(root, "c/child", nil); err == nil {
		t.Fatal("write through file")
	}
	if err := atomicWrite(filepath.Join(root, "missing"), "x", nil); err == nil {
		t.Fatal("write to missing root")
	}
	if err := atomicWrite(root, "a", nil); err == nil {
		t.Fatal("replaced directory")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".wk-") {
			t.Fatal("temporary file leaked")
		}
	}
}
