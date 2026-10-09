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

const guardedText = "# Local\nBefore\n<!-- wikisvc:begin version=a3f91c2e -->\nGenerated policy\n<!-- wikisvc:end -->\nAfter\n"

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
		{"inside", "AGENTS.md", map[string]any{"old_string": "Generated", "new_string": "Changed"}, 2},
		{"outside", "AGENTS.md", map[string]any{"old_string": "Before", "new_string": "Updated"}, 0},
		{"absolute", filepath.Join(a.Root, "AGENTS.md"), map[string]any{"old_string": "Generated", "new_string": "Changed"}, 2},
		{"normalised", "docs/../AGENTS.md", map[string]any{"old_string": "Generated", "new_string": "Changed"}, 2},
		{"unlisted", "OTHER.md", map[string]any{"old_string": "Generated", "new_string": "Changed"}, 0},
		{"escape", "../AGENTS.md", map[string]any{"old_string": "Generated", "new_string": "Changed"}, 0},
		{"missing path", "", map[string]any{"old_string": "Generated", "new_string": "Changed"}, 0},
		{"begin", "AGENTS.md", map[string]any{"old_string": "<!-- wikisvc:begin", "new_string": "<!-- local"}, 2},
		{"end", "AGENTS.md", map[string]any{"old_string": "<!-- wikisvc:end -->", "new_string": ""}, 2},
		{"cross boundary", "AGENTS.md", map[string]any{"old_string": "Before\n<!--", "new_string": "Before\n"}, 2},
		{"no match", "AGENTS.md", map[string]any{"old_string": "not in file", "new_string": "Changed"}, 0},
		{"replace all", "AGENTS.md", map[string]any{"old_string": "e", "new_string": "E", "replace_all": true}, 2},
		{"replace first outside", "AGENTS.md", map[string]any{"old_string": "e", "new_string": "E"}, 0},
		{"replace all outside", "AGENTS.md", map[string]any{"old_string": "Before", "new_string": "Updated", "replace_all": true}, 0},
		{"empty old", "AGENTS.md", map[string]any{"old_string": "", "new_string": "Changed"}, 0},
		{"unknown edit shape", "AGENTS.md", map[string]any{"unknown": true}, 0},
		{"multi blocked", "AGENTS.md", map[string]any{"edits": []map[string]any{{"old_string": "Before", "new_string": "Much longer before"}, {"old_string": "policy", "new_string": "rule"}}}, 2},
		{"multi outside", "AGENTS.md", map[string]any{"edits": []map[string]any{{"old_string": "Before", "new_string": "Changed"}, {"old_string": "Changed", "new_string": "Again"}}}, 0},
		{"write preserve", "AGENTS.md", map[string]any{"content": strings.ReplaceAll(strings.ReplaceAll(guardedText, "Before", "Updated"), "After", "Updated too")}, 0},
		{"write change", "AGENTS.md", map[string]any{"content": strings.ReplaceAll(guardedText, "Generated", "Changed")}, 2},
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
	for _, path := range []string{"docs/_meta/check.py", ".claude/settings.json", ".codex/hooks.json", manifestFile} {
		for _, spelling := range []string{path, "./" + path, filepath.Join(a.Root, path)} {
			if code, out, stderr := invokeHook(t, a, "guard-paths", toolPayload(spelling, nilMap())); code != 2 || out != "" || !strings.Contains(stderr, path) {
				t.Fatalf("%s: %d %q %q", spelling, code, out, stderr)
			}
		}
	}
	for _, path := range []string{"main.go", ".claude/settings.local.json", ".codex/hooks.json.bak", "", ".", "../.codex/hooks.json"} {
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
	for _, cmd := range []string{"guard-paths", "guard-block", "check", "policy-stale"} {
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
	for _, state := range []string{"current", "stale", "offline", "missing block", "docs error", "docs warning", "no docs", "broken block", "server error"} {
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
			if _, err := os.Stat(filepath.Join(a.Root, "docs/_meta/registry.md")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("hook wrote registry", err)
			}
			code, out, stderr = invokeHook(t, a, "policy-stale", `{}`)
			if code != 0 || stderr != "" {
				t.Fatal(code, out, stderr)
			}
			if state == "stale" || state == "missing block" {
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
		event := hookEvent{}
		empty := ""
		event.ToolInput.Content = &empty
		if !changesPolicyBlock(text, event) {
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
