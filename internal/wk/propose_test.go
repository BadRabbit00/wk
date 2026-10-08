package wk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

const page = "---\nid: term-example\ntitle: Example page\n---\n\n# Example\n"

func TestProposeFlow(t *testing.T) {
	for _, validationErrors := range []bool{false, true} {
		t.Run(fmt.Sprint(validationErrors), func(t *testing.T) {
			var calls []string
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, r.Method+" "+r.URL.Path)
				switch {
				case r.URL.Path == "/api/v1/proposals":
					var data map[string]string
					if err := json.NewDecoder(r.Body).Decode(&data); err != nil || data["title"] != "Example page" || data["kind"] != "manual" {
						t.Error(data, err)
					}
					fmt.Fprint(w, `{"pid":"p-123"}`)
				case strings.Contains(r.URL.Path, "/pages/"):
					var data string
					if err := json.NewDecoder(r.Body).Decode(&data); err != nil || data != page {
						t.Error(data, err)
					}
					fmt.Fprint(w, `{}`)
				case strings.HasSuffix(r.URL.Path, "/validate"):
					if validationErrors {
						fmt.Fprintf(w, `{"errors":[{"message":%q}],"warnings":[]}`, t.Name())
					} else {
						fmt.Fprint(w, `{"errors":[],"warnings":[]}`)
					}
				case strings.HasSuffix(r.URL.Path, "/submit"):
					fmt.Fprint(w, `{}`)
				default:
					t.Error("unexpected path", r.URL.Path)
				}
			}))
			defer s.Close()
			a := testApp(t, s)
			put(t, a.Root, "page.md", page)
			code, out, err := invoke(t, a, "propose", "--file", "page.md", "--json")
			want := 0
			if validationErrors {
				want = 1
			}
			if code != want || err != "" || !json.Valid([]byte(out)) {
				t.Fatalf("%d %s %s", code, out, err)
			}
			wantCalls := []string{"POST /api/v1/proposals", "PUT /api/v1/proposals/p-123/pages/term-example", "GET /api/v1/proposals/p-123/validate"}
			if !validationErrors {
				wantCalls = append(wantCalls, "POST /api/v1/proposals/p-123/submit")
			}
			if strings.Join(calls, "\n") != strings.Join(wantCalls, "\n") {
				t.Fatal(calls)
			}
		})
	}
}

func TestProposeFailures(t *testing.T) {
	cases := []map[string]response{
		{"/api/v1/proposals": {500, "broken", ""}},
		{"/api/v1/proposals": {200, `{"pid":"../evil"}`, ""}},
		{"/api/v1/proposals/p-123/pages/term-example": {500, "broken", ""}},
		{"/api/v1/proposals/p-123/validate": {500, "broken", ""}},
		{"/api/v1/proposals/p-123/validate": {200, `{}`, ""}},
		{"/api/v1/proposals/p-123/submit": {500, "broken", ""}},
	}
	for _, overrides := range cases {
		s := server(t, overrides)
		a := testApp(t, s)
		put(t, a.Root, "page.md", page)
		if code, _, err := invoke(t, a, "propose", "--file", filepath.Join(a.Root, "page.md"), "--title", "Explicit"); code != 1 || err == "" {
			t.Fatalf("%d %s", code, err)
		}
	}
	s := server(t, nil)
	a := testApp(t, s)
	put(t, a.Root, "page.md", strings.Replace(page, "title: Example page\n", "", 1))
	if code, _, err := invoke(t, a, "propose", "--file", "page.md"); code != 0 {
		t.Fatal(err)
	}
	put(t, a.Root, "page.md", "broken")
	if code, _, _ := invoke(t, a, "propose", "--file", "page.md"); code != 1 {
		t.Fatal("accepted bad page")
	}
	for _, text := range []string{"no frontmatter", "---\nunclosed", "---\n[]\n---\nbody", "---\nx: [\n---\nbody", "---\nid: term-example\nid: duplicate\n---\nbody", "---\nid: ../evil\n---\nbody", "---\nid: [list]\n---\nbody"} {
		if _, _, err := pageMetadata([]byte(text)); err == nil {
			t.Fatal(text)
		}
	}
	if _, _, err := pageMetadata([]byte(strings.ReplaceAll(page, "\n", "\r\n"))); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := git(ctx, a.Root, "status", "--porcelain"); err == nil {
		t.Fatal("ignored cancellation")
	}
}
