package wk

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTemplateCache(t *testing.T) {
	s := server(t, nil)
	a := testApp(t, s)
	c := Client{Origin: s.URL, Token: a.Token}
	ctx := context.Background()
	tpl, err := a.fetchTemplate(ctx, c, "go-service", false, "")
	if err != nil {
		t.Fatal(err)
	}
	_, digest := tpl.canonical()
	for _, pin := range []string{"", digest} {
		cached, err := a.cachedTemplate("go-service", pin)
		if err != nil || cached.ID != tpl.ID {
			t.Fatal(cached, err)
		}
	}
	if _, err := a.cachedTemplate("go-service", "sha256:"+strings.Repeat("a", 64)); err == nil {
		t.Fatal("accepted missing pin")
	}
	if _, err := a.fetchTemplate(ctx, c, "go-service", false, "wrong"); err == nil {
		t.Fatal("accepted wrong pin")
	}
	path := filepath.Join(a.Cache, "go-service-"+strings.TrimPrefix(digest, "sha256:")+".yaml")
	put(t, a.Cache, "unrelated.txt", "x")
	if err := os.WriteFile(path, []byte(strings.Replace(templateYAML, "Go service", "Changed", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := a.cachedTemplate("go-service", ""); err == nil {
		t.Fatal("accepted corrupted digest")
	}
	put(t, a.Cache, filepath.Base(path), "bad")
	if _, err := a.cachedTemplate("go-service", ""); err == nil {
		t.Fatal("accepted invalid cache")
	}
	put(t, a.Cache, filepath.Base(path), a.Token)
	if _, err := a.cachedTemplate("go-service", ""); err == nil {
		t.Fatal("accepted credential in cache")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), path); err != nil {
		t.Fatal(err)
	}
	if _, err := a.cachedTemplate("go-service", ""); err == nil {
		t.Fatal("accepted symlink cache")
	}
	a.Cache = filepath.Join(t.TempDir(), "missing")
	if _, err := a.cachedTemplate("go-service", ""); err == nil {
		t.Fatal("accepted missing cache")
	}
	for _, source := range []string{"../evil", "github:malformed"} {
		if _, err := a.fetchTemplate(ctx, c, source, false, ""); err == nil {
			t.Fatal(source)
		}
	}
}

func TestTemplateRemoteFailures(t *testing.T) {
	for _, r := range []response{{500, "oops", ""}, {200, "broken", ""}, {200, strings.Replace(templateYAML, "id: go-service", "id: another", 1), ""}, {200, templateYAML + "# " + t.Name(), ""}} {
		s := server(t, map[string]response{"/api/v1/schema/project-templates/go-service": r})
		a := testApp(t, s)
		if code, _, err := invoke(t, a, "template", "show", "go-service"); code != 1 || err == "" {
			t.Fatalf("%d %s", code, err)
		}
	}
	s := server(t, map[string]response{"/api/v1/schema": {200, `{"project_templates":{}}`, ""}})
	a := testApp(t, s)
	if code, out, err := invoke(t, a, "template", "list"); code != 0 || !strings.Contains(out, "Шаблонов нет") {
		t.Fatalf("%d %s %s", code, out, err)
	}
	s2 := server(t, map[string]response{"/api/v1/schema": {500, "oops", ""}, "/api/v1/rules/related": {500, "oops", ""}})
	a = testApp(t, s2)
	for _, args := range [][]string{{"template", "list"}, {"policy", "search", "query"}} {
		if code, _, _ := invoke(t, a, args...); code != 1 {
			t.Fatal(args)
		}
	}
	a.Cache = filepath.Join(a.Root, manifestFile, "child")
	if _, err := a.fetchTemplate(context.Background(), Client{Origin: s.URL}, "go-service", false, ""); err == nil {
		t.Fatal("accepted cache under regular file")
	}
}

func TestRejectedTemplateNeverFallsBack(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
	}{{422, "E_TEMPLATE_INVALID"}, {404, "E_TEMPLATE_UNKNOWN"}} {
		t.Run(tc.code, func(t *testing.T) {
			healthy := server(t, nil)
			a := testApp(t, healthy)
			if _, err := a.fetchTemplate(context.Background(), Client{Origin: healthy.URL}, "go-service", false, ""); err != nil {
				t.Fatal(err)
			}
			failed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/schema/project-templates/go-service" {
					t.Errorf("unexpected fallback request: %s", r.URL.Path)
				}
				w.WriteHeader(tc.status)
				fmt.Fprintf(w, `{"error":{"code":%q,"message":%q,"details":["private-details"]}}`, tc.code, a.Token)
			}))
			defer failed.Close()
			saveManifest(t, a.Root, manifest(failed.URL))
			put(t, a.Root, "existing/keep.txt", "keep existing contents")
			for _, args := range [][]string{
				{"template", "show", "go-service"},
				{"template", "show", "go-service", "--json"},
				{"init", "--dir", "new-project", "--url", failed.URL, "--template", "go-service", "--profile", "go-service"},
				{"init", "--dir", "existing", "--existing", "--force", "--url", failed.URL, "--template", "go-service", "--profile", "go-service"},
			} {
				code, out, stderr := invoke(t, a, args...)
				if code != 1 || !strings.Contains(out+stderr, fmt.Sprintf("HTTP %d (%s)", tc.status, tc.code)) || strings.Contains(out+stderr, "private-details") {
					t.Fatalf("%v: %d %s %s", args, code, out, stderr)
				}
			}
			if _, err := os.Stat(filepath.Join(a.Root, "new-project")); !os.IsNotExist(err) {
				t.Fatalf("failed init created project: %v", err)
			}
			entries, err := os.ReadDir(filepath.Join(a.Root, "existing"))
			if err != nil || len(entries) != 1 || entries[0].Name() != "keep.txt" {
				t.Fatalf("failed init changed existing project: %v %v", entries, err)
			}
			data, err := os.ReadFile(filepath.Join(a.Root, "existing/keep.txt"))
			if err != nil || string(data) != "keep existing contents" {
				t.Fatalf("failed init overwrote existing file: %q %v", data, err)
			}
			if _, err := a.cachedTemplate("go-service", ""); err != nil {
				t.Fatalf("cached template must still be valid: %v", err)
			}
		})
	}
}

func TestGitHubExplicitSource(t *testing.T) {
	a := App{Cache: filepath.Join(t.TempDir(), "cache"), Token: "wiki-secret"}
	a.HTTP = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://raw.githubusercontent.com/owner/repo/HEAD/schema/project-templates/go-service.yaml" || r.Header.Get("Authorization") != "" {
			t.Fatal("wrong GitHub request", r.URL, r.Header)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(templateYAML))}, nil
	})}
	if _, err := a.fetchTemplate(context.Background(), Client{}, "github:owner/repo#go-service", false, ""); err != nil {
		t.Fatal(err)
	}
}

func TestTemplateLimitBoundaries(t *testing.T) {
	var files strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&files, "  - {path: file-%d, content: x}\n", i)
	}
	if _, err := parseTemplate([]byte("id: go-service\ntitle: Go\nprofile: go-service\nfiles:\n" + files.String())); err != nil {
		t.Fatal(err)
	}
}
