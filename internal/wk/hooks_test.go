package wk

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func invokeHook(t *testing.T, a App, command, payload string) (int, string, string) {
	t.Helper()
	a.Stdin = strings.NewReader(payload)
	return invoke(t, a, "hook", command)
}

func toolPayload(path string, fields map[string]any) string {
	fields["file_path"] = path
	data, _ := json.Marshal(map[string]any{"tool_input": fields, "unknown": true})
	return string(data)
}

// currentBlock is exactly what the test server compiles; any other text under this version is tampering.
const currentBlock = "<!-- wikisvc:begin version=a3f91c2e -->\n# Policies\n\nUse Go.\n<!-- wikisvc:end -->"
const guardedText = "# Local\nBefore\n" + currentBlock + "\nAfter\n"

func TestGuardBlock(t *testing.T) {
	s := server(t, nil)
	a := testApp(t, s)
	put(t, a.Root, "AGENTS.md", guardedText)
	cases := []struct {
		name  string
		path  string
		input map[string]any
		code  int
	}{
		{"inside", "AGENTS.md", map[string]any{"old_string": "Use Go", "new_string": "Changed"}, 2},
		{"outside", "AGENTS.md", map[string]any{"old_string": "Before", "new_string": "Updated"}, 0},
		{"absolute", filepath.Join(a.Root, "AGENTS.md"), map[string]any{"old_string": "Use Go", "new_string": "Changed"}, 2},
		{"normalised", "docs/../AGENTS.md", map[string]any{"old_string": "Use Go", "new_string": "Changed"}, 2},
		{"unlisted", "OTHER.md", map[string]any{"old_string": "Use Go", "new_string": "Changed"}, 0},
		{"escape", "../AGENTS.md", map[string]any{"old_string": "Use Go", "new_string": "Changed"}, 0},
		{"missing path", "", map[string]any{"old_string": "Use Go", "new_string": "Changed"}, 0},
		{"begin", "AGENTS.md", map[string]any{"old_string": "<!-- wikisvc:begin", "new_string": "<!-- local"}, 2},
		{"end", "AGENTS.md", map[string]any{"old_string": "<!-- wikisvc:end -->", "new_string": ""}, 2},
		{"cross boundary", "AGENTS.md", map[string]any{"old_string": "Before\n<!--", "new_string": "Before\n"}, 2},
		{"no match", "AGENTS.md", map[string]any{"old_string": "not in file", "new_string": "Changed"}, 0},
		{"replace all", "AGENTS.md", map[string]any{"old_string": "e", "new_string": "E", "replace_all": true}, 2},
		{"replace first outside", "AGENTS.md", map[string]any{"old_string": "e", "new_string": "E"}, 0},
		{"replace all outside", "AGENTS.md", map[string]any{"old_string": "Before", "new_string": "Updated", "replace_all": true}, 0},
		{"empty old", "AGENTS.md", map[string]any{"old_string": "", "new_string": "Changed"}, 0},
		{"unknown edit shape", "AGENTS.md", map[string]any{"unknown": true}, 0},
		{"multi blocked", "AGENTS.md", map[string]any{"edits": []map[string]any{{"old_string": "Before", "new_string": "Much longer before"}, {"old_string": "Policies", "new_string": "Rules"}}}, 2},
		{"multi outside", "AGENTS.md", map[string]any{"edits": []map[string]any{{"old_string": "Before", "new_string": "Changed"}, {"old_string": "Changed", "new_string": "Again"}}}, 0},
		{"write preserve", "AGENTS.md", map[string]any{"content": strings.ReplaceAll(strings.ReplaceAll(guardedText, "Before", "Updated"), "After", "Updated too")}, 0},
		{"write change", "AGENTS.md", map[string]any{"content": strings.ReplaceAll(guardedText, "Use Go", "Changed")}, 2},
		{"write delete", "AGENTS.md", map[string]any{"content": "# Local\n"}, 2},
		{"inject", "AGENTS.md", map[string]any{"old_string": "After", "new_string": "<!-- wikisvc:begin"}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, out, stderr := invokeHook(t, a, "guard-block", toolPayload(tc.path, tc.input))
			if code != tc.code || out != "" || (tc.code == 0 && stderr != "") || (tc.code == 2 && stderr != blockGuardMessage+"\n") {
				t.Fatalf("%d %q %q", code, out, stderr)
			}
		})
	}
	data, _ := os.ReadFile(filepath.Join(a.Root, "AGENTS.md"))
	if string(data) != guardedText {
		t.Fatal("hook mutated policy file")
	}
}

func TestGuardPaths(t *testing.T) {
	s := server(t, nil)
	a := testApp(t, s)
	for _, path := range protectedPaths {
		for _, spelling := range []string{path, "./" + path, filepath.Join(a.Root, path)} {
			if code, out, stderr := invokeHook(t, a, "guard-paths", toolPayload(spelling, nilMap())); code != 2 || out != "" || !strings.Contains(stderr, path) {
				t.Fatalf("%s: %d %q %q", spelling, code, out, stderr)
			}
		}
	}
	notebook, _ := json.Marshal(map[string]any{"tool_input": map[string]any{"notebook_path": ".wiki-project.yaml"}})
	if code, _, _ := invokeHook(t, a, "guard-paths", string(notebook)); code != 2 {
		t.Fatal("notebook path not guarded", code)
	}
	for _, path := range []string{"main.go", ".claude/settings.local.json", ".codex/hooks.json.bak", "", ".", "../.codex/hooks.json", "src/flake.nix"} {
		if code, out, stderr := invokeHook(t, a, "guard-paths", toolPayload(path, nilMap())); code != 0 || out != "" || stderr != "" {
			t.Fatalf("%s: %d %q %q", path, code, out, stderr)
		}
	}
	put(t, a.Root, "src/main.go", "package main")
	payload, _ := json.Marshal(map[string]any{"cwd": filepath.Join(a.Root, "src"), "tool_input": map[string]any{"file_path": "../.codex/hooks.json"}})
	if code, _, _ := invokeHook(t, a, "guard-paths", string(payload)); code != 2 {
		t.Fatal(code)
	}
	if err := os.Symlink(filepath.Join(a.Root, ".codex/hooks.json"), filepath.Join(a.Root, "alias.json")); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := invokeHook(t, a, "guard-paths", toolPayload("alias.json", nilMap())); code != 2 {
		t.Fatal(code)
	}
	outside := filepath.Join(t.TempDir(), "custom.json")
	put(t, filepath.Dir(outside), "custom.json", `{}`)
	os.Remove(filepath.Join(a.Root, ".codex/hooks.json"))
	if err := os.Symlink(outside, filepath.Join(a.Root, ".codex/hooks.json")); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := invokeHook(t, a, "guard-paths", toolPayload(".codex/hooks.json", nilMap())); code != 2 {
		t.Fatal(code)
	}
}

func nilMap() map[string]any { return map[string]any{} }

func TestHooksIgnoreMalformedOrAbsentProject(t *testing.T) {
	s := server(t, nil)
	a := testApp(t, s)
	for _, cmd := range []string{"guard-paths", "guard-block", "guard-shell", "check", "policy-stale"} {
		for _, payload := range []string{"", "{", "null", "[]", `{"tool_input":`, `{"tool_input":1}`, `{"stop_hook_active":"yes"}`, "{} {}", strings.Repeat(" ", (16<<20)+1)} {
			if code, out, stderr := invokeHook(t, a, cmd, payload); code != 0 || out != "" || stderr != "" {
				t.Fatalf("%s malformed: %d %q %q", cmd, code, out, stderr)
			}
		}
		for _, payload := range []string{`{}`, toolPayload(manifestFile, nilMap())} {
			if code, out, stderr := invokeHook(t, App{Root: t.TempDir()}, cmd, payload); code != 0 || out != "" || stderr != "" {
				t.Fatalf("%s absent: %d %q %q", cmd, code, out, stderr)
			}
		}
		if code, out, stderr := invoke(t, a, "hook", cmd); code != 0 || out != "" || stderr != "" {
			t.Fatal(code, out, stderr)
		}
	}
	put(t, a.Root, manifestFile, "broken")
	if code, out, stderr := invokeHook(t, a, "check", `{}`); code != 0 || out != "" || stderr != "" {
		t.Fatal(code, out, stderr)
	}
	a.Stdin = brokenReader{}
	if code, out, stderr := invoke(t, a, "hook", "check"); code != 0 || out != "" || stderr != "" {
		t.Fatal(code, out, stderr)
	}
	for _, args := range [][]string{{"hook"}, {"hook", "unknown"}, {"hook", "guard-paths", "--file", "x"}, {"badcmd"}} {
		if code, _, _ := invoke(t, a, args...); code != 2 {
			t.Fatal(args, code)
		}
	}
}

func TestHookCheckAndStaleness(t *testing.T) {
	for _, state := range []string{"current", "stale", "offline", "offline docs error", "tampered", "missing block", "docs error", "docs warning", "no docs", "broken block", "server error"} {
		t.Run(state, func(t *testing.T) {
			s := server(t, nil)
			a := testApp(t, s)
			put(t, a.Root, "AGENTS.md", guardedText)
			put(t, a.Root, "docs/INDEX.md", "# Docs\n")
			switch state {
			case "stale":
				put(t, a.Root, "AGENTS.md", strings.ReplaceAll(guardedText, "a3f91c2e", "00000000"))
			case "offline":
				s.Close()
			case "offline docs error":
				// Regression: an unreachable wiki used to skip the documentation check entirely.
				s.Close()
				put(t, a.Root, "docs/broken.md", "ENT-missing\n")
			case "tampered":
				put(t, a.Root, "AGENTS.md", strings.ReplaceAll(guardedText, "Use Go.", "Use anything."))
			case "missing block":
				put(t, a.Root, "AGENTS.md", "# Local\n")
			case "docs error":
				put(t, a.Root, "docs/broken.md", "ENT-missing\n")
			case "docs warning":
				put(t, a.Root, "docs/system/domain.md", "## ENT-order — Order\nstatus: draft\n"+strings.Repeat("\n", 400))
			case "no docs":
				os.RemoveAll(filepath.Join(a.Root, "docs"))
			case "broken block":
				put(t, a.Root, "AGENTS.md", "<!-- wikisvc:begin")
			case "server error":
				a.HTTP = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("unavailable"))}, nil
				})}
			}
			want := 2
			if state == "current" || state == "offline" {
				want = 0
			}
			code, out, stderr := invokeHook(t, a, "check", `{"extra":{"ignored":true}}`)
			if code != want || out != "" {
				t.Fatalf("%d %q %q", code, out, stderr)
			}
			if state == "current" && stderr != "" {
				t.Fatal(stderr)
			}
			if state != "current" && strings.Count(stderr, "\n") != 1 {
				t.Fatal(stderr)
			}
			if state == "offline" && !strings.Contains(stderr, "сеть недоступна") {
				t.Fatal(stderr)
			}
			if state == "offline docs error" && !strings.Contains(stderr, "ENT-missing") {
				t.Fatal(stderr)
			}
			if state == "tampered" && !strings.Contains(stderr, "изменён вручную") {
				t.Fatal(stderr)
			}
			if _, err := os.Stat(filepath.Join(a.Root, "docs/_meta/registry.md")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("hook wrote registry", err)
			}
			code, out, stderr = invokeHook(t, a, "policy-stale", `{}`)
			if code != 0 || stderr != "" {
				t.Fatal(code, out, stderr)
			}
			if state == "stale" || state == "missing block" || state == "tampered" {
				if out != staleContext+"\n" || !json.Valid([]byte(out)) {
					t.Fatal(out)
				}
			} else if out != "" {
				t.Fatal(out)
			}
		})
	}
}

func TestStopHookActiveSkipsEverything(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); t.Error("unexpected network access") }))
	defer s.Close()
	a := testApp(t, s)
	if code, out, stderr := invokeHook(t, a, "check", `{"stop_hook_active":true}`); code != 0 || out != "" || stderr != "" {
		t.Fatal(code, out, stderr)
	}
	if calls.Load() != 0 {
		t.Fatal("stop continuation performed checks")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a.Stdin = strings.NewReader(`{}`)
	res, err := a.hook(ctx, []string{"policy-stale"})
	if err != nil || res.Code != 0 || res.Text != "" {
		t.Fatal(res, err)
	}
}

func TestProtectedPolicyRanges(t *testing.T) {
	for _, text := range []string{"<!-- wikisvc:begin", "prefix <!-- wikisvc:begin -->\npolicy\n<!-- wikisvc:end -->", guardedText + guardedText} {
		change := hookChange{}
		empty := ""
		change.Content = &empty
		if !changesPolicyBlock(text, change) {
			t.Fatal("unprotected block", text)
		}
	}
	if got := hookPath("/not-a-real-project", "/", "a"); got != "" {
		t.Fatal(got)
	}
	if got := resolveHookTarget("/"); got != "/" {
		t.Fatal(got)
	}
}

func commandPayload(command any) string {
	data, _ := json.Marshal(map[string]any{"tool_name": "apply_patch", "tool_input": map[string]any{"command": command}})
	return string(data)
}

// Codex sends apply_patch to the Edit|Write matcher with the patch in tool_input.command.
func TestGuardsReadCodexPatches(t *testing.T) {
	s := server(t, nil)
	a := testApp(t, s)
	put(t, a.Root, "AGENTS.md", guardedText)
	patch := func(body string) string { return "*** Begin Patch\n" + body + "\n*** End Patch" }
	cases := []struct {
		name, hook, body string
		code             int
	}{
		{"update settings", "guard-paths", "*** Update File: .claude/settings.json\n@@\n-{}\n+{\"hooks\":{}}", 2},
		{"add flake", "guard-paths", "*** Add File: flake.nix\n+{}", 2},
		{"delete manifest", "guard-paths", "*** Delete File: .wiki-project.yaml", 2},
		{"move onto envrc", "guard-paths", "*** Update File: notes.txt\n*** Move to: .envrc\n@@\n-a\n+b", 2},
		{"ordinary file", "guard-paths", "*** Update File: main.go\n@@\n-a\n+b", 0},
		{"edit inside block", "guard-block", "*** Update File: AGENTS.md\n@@\n # Policies\n \n-Use Go.\n+Use anything.", 2},
		{"edit outside block", "guard-block", "*** Update File: AGENTS.md\n@@\n # Local\n-Before\n+Updated\n*** End of File", 0},
		{"two hunks", "guard-block", "*** Update File: AGENTS.md\n@@\n-Before\n+Updated\n@@\n-After\n+Later", 0},
		{"pure insertion", "guard-block", "*** Update File: AGENTS.md\n@@\n+appended", 0},
		{"forged block", "guard-block", "*** Update File: AGENTS.md\n@@\n+<!-- wikisvc:begin version=00000000 -->", 2},
		{"delete policy file", "guard-block", "*** Delete File: AGENTS.md", 2},
		{"move policy file away", "guard-block", "*** Update File: AGENTS.md\n*** Move to: OLD.md", 2},
		{"rewrite policy file", "guard-block", "*** Add File: AGENTS.md\n+# Local", 2},
		{"unrelated add", "guard-block", "*** Add File: notes.md\n+hi", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if code, out, stderr := invokeHook(t, a, tc.hook, commandPayload(patch(tc.body))); code != tc.code || out != "" || (code == 0) != (stderr == "") {
				t.Fatalf("%d %q %q", code, out, stderr)
			}
		})
	}
	if code, _, _ := invokeHook(t, a, "guard-paths", commandPayload([]string{"apply_patch", patch("*** Delete File: flake.lock")})); code != 2 {
		t.Fatal("argv command not parsed", code)
	}
	if got := patchChanges("stray line\n-before any file"); len(got) != 0 {
		t.Fatal(got)
	}
}

// guard-shell is a tripwire for Bash: it is checked case by case, not proven complete.
func TestGuardShell(t *testing.T) {
	s := server(t, nil)
	a := testApp(t, s)
	put(t, a.Root, "AGENTS.md", guardedText)
	cases := []struct {
		command string
		code    int
	}{
		{"rm .claude/settings.json", 2},
		{"cd .claude && rm -f settings.json", 2},
		{"cat .wiki-project.yaml", 2},
		{"sed -i 's/a/b/' flake.nix", 2},
		{"echo use nix > .envrc", 2},
		{"git checkout HEAD~3 -- flake.lock", 2},
		{"nix flake update", 2},
		{"nix  flake lock --update-input wk", 2},
		{"wk init --existing --force --profile x", 2},
		{"rm -rf .codex", 2},
		{"rm -rf build", 0},
		{"cp /tmp/x docs/_meta/ids.md", 2},
		{"python3 -c 'open(\"AGENTS.md\",\"w\")'", 2},
		{"printf x >> AGENTS.md", 2},
		{"apply_patch <<'EOF'\n*** Begin Patch\n*** Update File: AGENTS.md\n@@\n-Use Go.\n+Use anything.\n*** End Patch\nEOF", 2},
		{"apply_patch <<'EOF'\n*** Begin Patch\n*** Add File: .codex/hooks.json\n+{}\n*** End Patch\nEOF", 2},
		{"apply_patch <<'EOF'\n*** Begin Patch\n*** Update File: main.go\n@@\n-a\n+b\n*** End Patch\nEOF", 0},
		{"go test ./... 2>&1 | tail -5", 0},
		{"grep -n rule AGENTS.md", 0},
		{"nix develop -c wk check --strict", 0},
		{"wk policy sync", 0},
		{"ls docs/_meta", 0},
		{"cat .claude/settings.local.json", 0},
		{"", 0},
	}
	for _, tc := range cases {
		t.Run(tc.command, func(t *testing.T) {
			payload, _ := json.Marshal(map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": tc.command, "description": "x"}})
			if code, out, stderr := invokeHook(t, a, "guard-shell", string(payload)); code != tc.code || out != "" || (code == 0) != (stderr == "") {
				t.Fatalf("%d %q %q", code, out, stderr)
			}
		})
	}
}
