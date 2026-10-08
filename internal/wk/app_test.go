package wk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type response struct {
	status      int
	body        string
	contentType string
}

func server(t *testing.T, overrides map[string]response) *httptest.Server {
	t.Helper()
	defaults := map[string]response{
		"/api/v1/health":           {200, `{"status":"ok"}`, ""},
		"/api/v1/whoami":           {200, `{"name":"anonymous","role":"writer","kind":"agent","clearance":"internal"}`, ""},
		"/api/v1/profiles":         {200, `[{"id":"go-service"}]`, ""},
		"/api/v1/scopes":           {200, `["lang:go"]`, ""},
		"/mcp":                     {200, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"tools\":[]}}\n\n", "text/event-stream"},
		"/agent-api/healthz":       {200, `{"status":"ok"}`, ""},
		"/api/v1/policies/compile": {200, `{"version":"a3f91c2e","markdown":"# Policies\n\nUse Go.\n"}`, ""},
		"/api/v1/schema/project-templates/go-service": {200, templateYAML, ""},
		"/api/v1/schema":                             {200, `{"project_templates":{"go-service":{"id":"go-service","title":"Go service","profile":"go-service"}}}`, ""},
		"/api/v1/rules/related":                      {200, `{"items":[{"id":"rule-go","title":"Go"}]}`, ""},
		"/api/v1/rules/rule-go":                      {200, `{"id":"rule-go","title":"Go"}`, ""},
		"/api/v1/proposals":                          {200, `{"pid":"p-123"}`, ""},
		"/api/v1/proposals/p-123/pages/term-example": {200, `{}`, ""},
		"/api/v1/proposals/p-123/validate":           {200, `{"errors":[],"warnings":[]}`, ""},
		"/api/v1/proposals/p-123/submit":             {200, `{}`, ""},
	}
	for path, r := range overrides {
		defaults[path] = r
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if want := t.Name(); r.Header.Get("Authorization") != "" && r.Header.Get("Authorization") != "Bearer "+want {
			t.Errorf("unexpected Authorization %q", r.Header.Get("Authorization"))
		}
		v, ok := defaults[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if v.contentType != "" {
			w.Header().Set("Content-Type", v.contentType)
		}
		w.WriteHeader(v.status)
		fmt.Fprint(w, v.body)
	}))
	t.Cleanup(s.Close)
	return s
}

func testApp(t *testing.T, s *httptest.Server) App {
	t.Helper()
	root := t.TempDir()
	saveManifest(t, root, manifest(s.URL))
	return App{Root: root, Token: t.Name(), Cache: filepath.Join(t.TempDir(), "templates"), Source: "path:/example/wk"}
}

func invoke(t *testing.T, a App, args ...string) (int, string, string) {
	t.Helper()
	var out, err bytes.Buffer
	code := a.Run(context.Background(), args, &out, &err)
	if a.Token != "" && (strings.Contains(out.String(), a.Token) || strings.Contains(err.String(), a.Token)) {
		t.Fatal("credential leaked")
	}
	return code, out.String(), err.String()
}

func TestCommandDispatch(t *testing.T) {
	s := server(t, nil)
	a := testApp(t, s)
	for _, args := range [][]string{nil, {"help"}, {"--help"}, {"-h"}, {"--json", "help"}, {"policy", "search", "go & sql"}, {"policy", "show", "rule-go", "--json"}, {"template", "list"}, {"template", "show", "go-service", "--json"}} {
		if code, out, err := invoke(t, a, args...); code != 0 || out == "" || err != "" {
			t.Fatalf("%v: %d %s %s", args, code, out, err)
		}
	}
	bad := [][]string{{"login"}, {"accept"}, {"promote"}, {"deprecate"}, {"revert"}, {"doctor", "extra"}, {"component"}, {"component", "remove"}, {"component", "add"}, {"component", "add", "a", "b"}, {"component", "add", "api", "--unknown"}, {"component", "add", "api", "--lang"}, {"component", "add", "api", "--with-ai=bad"}, {"check", "arg"}, {"check", "--wat"}, {"policy"}, {"policy", "accept", "rule-go"}, {"policy", "show", "../x"}, {"template"}, {"template", "show", "github:owner/repo#id"}, {"propose"}, {"propose", "--file", "x", "--kind", "heal"}, {"propose", "--file", "x", "extra"}, {"propose", "--unknown"}, {"propose", "--file", "missing.md"}}
	for _, args := range bad {
		want := 1
		if args[0] == "check" {
			want = 2
		}
		if code, _, _ := invoke(t, a, args...); code != want {
			t.Fatalf("accepted %v", args)
		}
	}
	put(t, a.Root, "docs/INDEX.md", "# Docs\n")
	for _, args := range [][]string{{"policy", "sync"}, {"policy", "check"}, {"check"}, {"check", "--strict", "--json"}} {
		code, out, _ := invoke(t, a, args...)
		if code != 0 || out == "" {
			t.Fatalf("%v: %d %s", args, code, out)
		}
	}
	if err := os.Remove(filepath.Join(a.Root, manifestFile)); err != nil {
		t.Fatal(err)
	}
	if code, _, err := invoke(t, a, "template", "list", "--json"); code != 1 || err == "" {
		t.Fatal("accepted missing manifest")
	}
}

func TestDoctor(t *testing.T) {
	cases := []struct {
		name      string
		overrides map[string]response
		code      int
		contains  string
	}{
		{"healthy", nil, 0, "whoami: name=anonymous role=writer kind=agent clearance=internal"},
		{"reader", map[string]response{"/api/v1/whoami": {200, `{"name":"reader","role":"reader","kind":"agent","clearance":"internal"}`, ""}}, 0, "предложения создавать нельзя"},
		{"anonymous disabled", map[string]response{"/api/v1/whoami": {401, "denied", ""}}, 1, "анонимный доступ выключен на сервере"},
		{"bad identity", map[string]response{"/api/v1/whoami": {200, `{}`, ""}}, 1, "невалидный ответ whoami"},
		{"bad health", map[string]response{"/api/v1/health": {200, `{"status":"broken"}`, ""}}, 1, "status не равен ok"},
		{"unavailable", map[string]response{"/api/v1/health": {503, "unavailable", ""}}, 1, "HTTP 503"},
		{"profile", map[string]response{"/api/v1/profiles": {200, `[]`, ""}}, 1, "профиль отсутствует"},
		{"scopes", map[string]response{"/api/v1/scopes": {200, `[]`, ""}}, 1, "отсутствуют scopes: lang:go"},
		{"mcp absent", map[string]response{"/mcp": {404, "missing", ""}}, 0, "MCP не включён"},
		{"mcp error", map[string]response{"/mcp": {200, `{"error":{"code":-1}}`, ""}}, 1, "невалидный ответ MCP"},
		{"mcp bad", map[string]response{"/mcp": {403, "denied", ""}}, 1, "mcp: вика вернула HTTP 403"},
		{"agent absent", map[string]response{"/agent-api/healthz": {503, "missing", ""}}, 0, "warn agent"},
		{"agent not ready", map[string]response{"/agent-api/healthz": {200, `{"status":"broken"}`, ""}}, 0, "агент не готов"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := server(t, tc.overrides)
			a := testApp(t, s)
			put(t, a.Root, "docs/INDEX.md", "# Docs\n")
			put(t, a.Root, "AGENTS.md", "<!-- wikisvc:begin version=a3f91c2e -->\nPolicies\n<!-- wikisvc:end -->\n")
			if code, out, err := invoke(t, a, "doctor"); code != tc.code || !strings.Contains(out, tc.contains) {
				t.Fatalf("%d %s %s", code, out, err)
			}
			code, out, _ := invoke(t, a, "doctor", "--json")
			if code != tc.code || !json.Valid([]byte(out)) {
				t.Fatal(out)
			}
		})
	}
	s := server(t, nil)
	a := testApp(t, s)
	if _, err := git(context.Background(), a.Root, "init", "--template="); err != nil {
		t.Fatal(err)
	}
	_, out, _ := invoke(t, a, "doctor")
	if !strings.Contains(out, "есть незакоммиченные") {
		t.Fatal(out)
	}
	put(t, a.Root, ".git/info/exclude", manifestFile+"\n")
	_, out, _ = invoke(t, a, "doctor")
	if !strings.Contains(out, "рабочее дерево чистое") {
		t.Fatal(out)
	}
	put(t, a.Root, manifestFile, "bad")
	code, out, _ := invoke(t, a, "doctor")
	if code != 1 || !strings.Contains(out, "fail manifest") {
		t.Fatal(out)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) { return 0, errors.New("broken") }
func (brokenReader) Close() error             { return nil }

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("broken") }

func TestClient(t *testing.T) {
	for _, token := range []string{"", t.Name()} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			want := ""
			if token != "" {
				want = "Bearer " + token
			}
			if r.Header.Get("Authorization") != want {
				t.Error("auth mismatch")
			}
			fmt.Fprint(w, `{"ok":true}`)
		}))
		c := Client{Origin: s.URL, Token: token}
		var out map[string]bool
		if err := c.request(context.Background(), "GET", "/", nil, &out); err != nil || !out["ok"] {
			t.Fatal(err)
		}
		s.Close()
	}
	c := Client{Origin: "https://wiki.example", Token: "secret"}
	if err := c.request(context.Background(), "POST", "/", make(chan int), nil); err == nil {
		t.Fatal("encoded unsupported payload")
	}
	c.Origin = ":"
	if err := c.request(context.Background(), "GET", "/", nil, nil); err == nil {
		t.Fatal("bad origin")
	}
	c.Origin = "https://wiki.example"
	cases := []struct {
		name     string
		response *http.Response
		err      error
	}{
		{"network", nil, errors.New("secret")},
		{"read", &http.Response{StatusCode: 200, Body: brokenReader{}}, nil},
		{"large", &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", 4*maxTemplateBytes+1)))}, nil},
		{"json", &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("invalid"))}, nil},
		{"status", &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("secret"))}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c.HTTP = &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) { return tc.response, tc.err })}
			var out any
			err := c.request(context.Background(), "GET", "/", nil, &out)
			if err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatal(err)
			}
		})
	}
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("followed redirect") }))
	defer destination.Close()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 302) }))
	defer s.Close()
	c = Client{Origin: s.URL, Token: "secret"}
	if err := c.request(context.Background(), "GET", "/", nil, nil); err == nil {
		t.Fatal("redirect accepted")
	}
}

func TestRedactionAndOutput(t *testing.T) {
	token := t.Name()
	s := server(t, map[string]response{"/api/v1/rules/related": {200, `{"text":"` + token + `"}`, ""}})
	a := testApp(t, s)
	for _, jsonFlag := range []string{"", "--json"} {
		args := []string{"policy", "search", "query"}
		if jsonFlag != "" {
			args = append(args, jsonFlag)
		}
		_, out, _ := invoke(t, a, args...)
		if !strings.Contains(out, "REDACTED") {
			t.Fatal(out)
		}
	}
	var err bytes.Buffer
	if code := a.Run(context.Background(), []string{"help"}, brokenWriter{}, &err); code != 1 || err.Len() == 0 {
		t.Fatal("ignored stdout error")
	}
	if redact(`secret\nvalue`, "secret\nvalue") != "[REDACTED]" {
		t.Fatal("escaped token leaked")
	}
	if redact("unchanged", "") != "unchanged" {
		t.Fatal("empty token changed output")
	}
}
