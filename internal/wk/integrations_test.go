package wk

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestSkillValidation(t *testing.T) {
	valid := "---\nname: example\ndescription: Короткое описание.\n---\n\n1. Выполни `wk check --strict`.\n"
	for _, data := range []string{valid, strings.ReplaceAll(valid, "\n", "\r\n"), strings.ReplaceAll(valid, "Короткое описание.", strings.Repeat("я", 1536))} {
		if err := validateSkill("example", []byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	cases := map[string]string{
		"first line":        "\n" + valid,
		"bad encoding":      valid + string([]byte{0xff}),
		"extra key":         strings.Replace(valid, "name: example", "name: example\nallowed-tools: Bash", 1),
		"missing key":       strings.Replace(valid, "description: Короткое описание.\n", "", 1),
		"unknown key":       strings.Replace(valid, "description:", "other:", 1),
		"name mismatch":     strings.Replace(valid, "name: example", "name: different", 1),
		"empty description": strings.Replace(valid, "Короткое описание.", "", 1),
		"colon":             strings.Replace(valid, "Короткое описание.", "Смотри doc: ID", 1),
		"quoted colon":      strings.Replace(valid, "Короткое описание.", `"Смотри doc: ID"`, 1),
		"escaped newline":   strings.Replace(valid, "Короткое описание.", `"one\ntwo"`, 1),
		"folded":            strings.Replace(valid, "Короткое описание.", ">\n  fold", 1),
		"long":              strings.Replace(valid, "Короткое описание.", strings.Repeat("я", 1537), 1),
		"missing check":     strings.Replace(valid, "wk check --strict", "wk doctor", 1),
		"later step":        valid + "2. Продолжай работу.\n",
		"step continuation": valid + "   Затем пропусти результат проверки.\n",
		"no steps":          strings.Replace(valid, "1. ", "", 1),
	}
	for _, forbidden := range []string{"curl", "WIKI_TOKEN", "https://example.test", "www.example.test", "Желательно", "обычно", "при необходимости"} {
		cases[forbidden] = strings.Replace(valid, "1. ", forbidden+"\n1. ", 1)
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if err := validateSkill("example", []byte(data)); err == nil {
				t.Fatal("accepted invalid skill")
			}
		})
	}
}

func TestEmbeddedIntegrationsMatchSources(t *testing.T) {
	files, err := integrationFiles(assets)
	if err != nil || len(files) != 14 {
		t.Fatal(len(files), err)
	}
	for _, name := range skillNames {
		data, err := os.ReadFile("../../../skills/" + name + "/SKILL.md")
		if errors.Is(err, os.ErrNotExist) {
			continue
		} // Standalone wk Nix source has no sibling repositories.
		if err != nil {
			t.Fatal(err)
		}
		for _, dir := range skillDirs {
			if !bytes.Equal(files[dir+"/"+name+"/SKILL.md"], data) {
				t.Fatal("stale embedded skill", name)
			}
		}
	}
	for target, source := range map[string]string{".claude/settings.json": "claude-settings.json", ".codex/hooks.json": "codex-hooks.json"} {
		data := files[target]
		var config struct {
			Hooks map[string][]struct {
				Matcher string
				Hooks   []struct {
					Type, Command, OnFailure string
					Timeout                  int
				}
			}
		}
		if err := json.Unmarshal(data, &config); err != nil {
			t.Fatal(err)
		}
		count := 0
		for event, groups := range config.Hooks {
			for _, group := range groups {
				if event == "PreToolUse" && group.Matcher != "Edit|Write|MultiEdit|NotebookEdit" && group.Matcher != "Bash" {
					t.Fatal(group.Matcher)
				}
				if event == "SessionStart" && group.Matcher != "startup|resume" {
					t.Fatal(group.Matcher)
				}
				for _, hook := range group.Hooks {
					count++
					if hook.Type != "command" || !strings.HasPrefix(hook.Command, "wk hook ") {
						t.Fatal(hook)
					}
					// Claude Code fails closed when wk is missing; Codex has no such field.
					wantFailure := ""
					if source == "claude-settings.json" && event != "SessionStart" {
						wantFailure = "block"
					}
					if hook.OnFailure != wantFailure {
						t.Fatal(source, hook)
					}
					if (group.Matcher == "Bash") != (hook.Command == "wk hook guard-shell") {
						t.Fatal(group.Matcher, hook)
					}
					wantTimeout := 10
					if event == "PreToolUse" {
						wantTimeout = 5
					}
					if hook.Timeout != wantTimeout {
						t.Fatal(hook)
					}
				}
			}
		}
		if count != 5 {
			t.Fatal(count)
		}
		original, err := os.ReadFile("../../../hooks/install/" + source)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !bytes.Equal(original, data) {
			t.Fatal("stale embedded hooks", source, err)
		}
	}
}

func TestInitInstallsIntegrationsOfflineAndPreservesExisting(t *testing.T) {
	a := App{Root: t.TempDir(), WikiURL: "http://127.0.0.1:1", Source: "path:/wk"}
	put(t, a.Root, ".claude/settings.json", `{"permissions":{"allow":["Read"]}}`)
	put(t, a.Root, ".codex/hooks.json", `{"hooks":{}}`)
	args := []string{"init", "--existing", "--offline", "--profile", "go-service", "--project", "example"}
	for _, force := range []bool{false, true} {
		if force {
			args = append(args, "--force")
		}
		code, _, stderr := invoke(t, a, args...)
		if code != 0 || !strings.Contains(stderr, "примени изменения вручную") || !strings.Contains(stderr, "--- a/.codex/hooks.json") || !strings.Contains(stderr, "+            \"command\": \"wk hook check\",") {
			t.Fatal(code, stderr)
		}
		for path, want := range map[string]string{".claude/settings.json": `{"permissions":{"allow":["Read"]}}`, ".codex/hooks.json": `{"hooks":{}}`} {
			data, err := os.ReadFile(filepath.Join(a.Root, path))
			if err != nil || string(data) != want {
				t.Fatal(path, string(data), err)
			}
		}
		if err := checkSkills(a.Root); err != nil {
			t.Fatal(err)
		}
		for _, dir := range skillDirs {
			for _, name := range skillNames {
				info, err := os.Lstat(filepath.Join(a.Root, dir, name, "SKILL.md"))
				if err != nil || !info.Mode().IsRegular() {
					t.Fatal(info, err)
				}
			}
		}
	}
	// An already-initialized project receives missing integrations without changing its manifest.
	manifestBefore, _ := os.ReadFile(filepath.Join(a.Root, manifestFile))
	os.RemoveAll(filepath.Join(a.Root, ".agents"))
	os.Remove(filepath.Join(a.Root, ".codex/hooks.json"))
	if code, out, _ := invoke(t, a, "init", "--offline", "--json"); code != 0 || !strings.Contains(out, `"changed":true`) {
		t.Fatal(code, out)
	}
	manifestAfter, _ := os.ReadFile(filepath.Join(a.Root, manifestFile))
	if !bytes.Equal(manifestBefore, manifestAfter) {
		t.Fatal("upgrade changed manifest")
	}
	if err := checkSkills(a.Root); err != nil {
		t.Fatal(err)
	}
	if code, out, _ := invoke(t, a, "init", "--json"); code != 0 || !strings.Contains(out, `"changed":false`) {
		t.Fatal(code, out)
	}
}

func TestIntegrationFailures(t *testing.T) {
	for _, bundle := range []fs.FS{fstest.MapFS{}, fstest.MapFS{"assets/skills/policy-sync/SKILL.md": {Data: []byte("broken")}}} {
		a := App{Root: t.TempDir(), WikiURL: "http://localhost", Source: "path:/wk", integrations: bundle}
		if code, _, _ := invoke(t, a, "init", "--offline", "--project", "example", "--profile", "go-service"); code != 1 {
			t.Fatal(code)
		}
	}
	bundle := fstest.MapFS{}
	for _, name := range skillNames {
		path := "assets/skills/" + name + "/SKILL.md"
		data, _ := assets.ReadFile(path)
		bundle[path] = &fstest.MapFile{Data: data}
	}
	if _, err := integrationFiles(bundle); err == nil {
		t.Fatal("missing hooks accepted")
	}
	s := server(t, nil)
	for _, scenario := range []string{"invalid skill", "directory", "symlink", "write"} {
		t.Run(scenario, func(t *testing.T) {
			a := testApp(t, s)
			switch scenario {
			case "invalid skill":
				put(t, a.Root, ".agents/skills/doc-card/SKILL.md", "broken")
			case "directory":
				os.Remove(filepath.Join(a.Root, ".codex/hooks.json"))
				os.Mkdir(filepath.Join(a.Root, ".codex/hooks.json"), 0755)
			case "symlink":
				os.RemoveAll(filepath.Join(a.Root, ".agents"))
				os.Symlink(t.TempDir(), filepath.Join(a.Root, ".agents"))
			case "write":
				os.Remove(filepath.Join(a.Root, ".codex/hooks.json"))
				a.writeFile = func(string, string, []byte) error { return fs.ErrPermission }
			}
			if code, _, _ := invoke(t, a, "init"); code != 1 {
				t.Fatal(code)
			}
		})
	}
	if got := integrationDiff("empty", "", ""); !strings.Contains(got, "@@ -1,0 +1,0 @@") {
		t.Fatal(got)
	}
}

func TestDoctorValidatesSkills(t *testing.T) {
	s := server(t, nil)
	a := testApp(t, s)
	put(t, a.Root, "AGENTS.md", guardedText)
	put(t, a.Root, "docs/INDEX.md", "# Docs\n")
	if code, out, _ := invoke(t, a, "doctor"); code != 0 || !strings.Contains(out, "ok skills") {
		t.Fatal(code, out)
	}
	put(t, a.Root, ".claude/skills/doc-card/SKILL.md", "broken")
	if code, out, _ := invoke(t, a, "doctor"); code != 1 || !strings.Contains(out, "fail skills") {
		t.Fatal(code, out)
	}
	os.RemoveAll(filepath.Join(a.Root, ".agents"))
	os.Symlink(t.TempDir(), filepath.Join(a.Root, ".agents"))
	if err := checkSkills(a.Root); err == nil {
		t.Fatal("symlink skills accepted")
	}
}

func TestIntegrationUpgradeDoesNotWriteCredential(t *testing.T) {
	a := App{Root: t.TempDir(), Token: "policy-sync"}
	saveManifest(t, a.Root, manifest("http://localhost"))
	if code, _, _ := invoke(t, a, "init"); code != 1 {
		t.Fatal("credential written", code)
	}
	if _, err := os.Stat(filepath.Join(a.Root, ".agents")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}
