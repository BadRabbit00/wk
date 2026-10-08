package wk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPolicyExportGolden(t *testing.T) {
	var compiled compiledPolicies
	data, err := os.ReadFile("../../testdata/policy/compiled.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &compiled); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"empty", "text", "crlf", "replace"} {
		t.Run(name, func(t *testing.T) {
			before, err := os.ReadFile("../../testdata/policy/" + name + ".before")
			if err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile("../../testdata/policy/" + name + ".after")
			if err != nil {
				t.Fatal(err)
			}
			got, err := exportPolicy(string(before), compiled)
			if err != nil || got != string(after) {
				t.Fatalf("%v\ngot %q\nwant %q", err, got, after)
			}
		})
	}
}

func TestPolicySyncCheckAndParameters(t *testing.T) {
	var requests []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.RawQuery)
		fmt.Fprint(w, `{"version":"b7d2e104","markdown":"- [MUST] Go. → rule-go\n","over_budget":true,"used_tokens":2500}`)
	}))
	defer s.Close()
	a := testApp(t, s)
	a.Token = ""
	m := manifest(s.URL)
	budget := 1234
	m.PolicyBlocks[0].Scopes = []string{"lang:go", "app:orders"}
	m.PolicyBlocks[0].BudgetTokens = &budget
	m.PolicyBlocks[0].Enforced = "all"
	saveManifest(t, a.Root, m)
	if code, out, err := invoke(t, a, "policy", "check"); code != 1 || !strings.Contains(out, "блока политик нет") {
		t.Fatal(code, out, err)
	}
	for i := 0; i < 2; i++ {
		code, out, err := invoke(t, a, "policy", "sync")
		if code != 0 || !strings.Contains(out, "2500") {
			t.Fatal(code, out, err)
		}
		if i == 1 && !strings.Contains(out, "без изменений") {
			t.Fatal(out)
		}
	}
	if code, out, err := invoke(t, a, "policy", "check"); code != 0 || !strings.Contains(out, "ok AGENTS.md") {
		t.Fatal(code, out, err)
	}
	for _, query := range requests {
		if query != "budget_tokens=1234&enforced=all&profile=go-service&scopes=lang%3Ago%2Capp%3Aorders" {
			t.Fatal(query)
		}
	}
	policies, err := localPolicies(a.Root, m)
	if err != nil || len(policies) != 1 || policies[0].ID != "rule-go" {
		t.Fatal(policies, err)
	}
	put(t, a.Root, "AGENTS.md", "<!-- wikisvc:begin version=00000000 -->\nOld\n<!-- wikisvc:end -->\n")
	if code, out, _ := invoke(t, a, "policy", "check"); code != 1 || !strings.Contains(out, "политики изменились") {
		t.Fatal(code, out)
	}
	if code, _, _ := invoke(t, a, "policy", "sync"); code != 0 {
		t.Fatal(code)
	}
	put(t, a.Root, "docs/INDEX.md", "rule-go\n")
	if code, _, err := invoke(t, a, "docs", "check"); code != 0 {
		t.Fatal(code, err)
	}
	s.Close()
	if code, _, err := invoke(t, a, "policy", "check"); code != 3 || !strings.Contains(err, "проверка не выполнена") {
		t.Fatal(code, err)
	}
	if code, _, _ := invoke(t, a, "check"); code != 3 {
		t.Fatal(code)
	}
	if code, _, _ := invoke(t, a, "policy", "sync"); code != 1 {
		t.Fatal(code)
	}
}

func TestPolicyCorruptionAndWriteFailures(t *testing.T) {
	s := server(t, nil)
	a := testApp(t, s)
	m := manifest(s.URL)
	good := "<!-- wikisvc:begin version=a3f91c2e -->\nText\n<!-- wikisvc:end -->"
	for _, text := range []string{good + good, "<!-- wikisvc:begin version=a3f91c2e -->", "<!-- wikisvc:begin version=INVALID -->\n<!-- wikisvc:end -->"} {
		put(t, a.Root, "AGENTS.md", text)
		for _, cmd := range []string{"sync", "check"} {
			if code, _, err := invoke(t, a, "policy", cmd); code != 1 || !strings.Contains(err, "повреждён или неоднозначен") {
				t.Fatal(code, err)
			}
		}
		actual, _ := os.ReadFile(filepath.Join(a.Root, "AGENTS.md"))
		if string(actual) != text {
			t.Fatal("damaged file modified")
		}
		if _, err := exportPolicy(text, compiledPolicies{}); err == nil {
			t.Fatal("damaged accepted")
		}
		if _, err := localPolicies(a.Root, m); err == nil {
			t.Fatal("damaged accepted")
		}
		if _, err := a.docsReport(m, "docs", ".", false); err == nil {
			t.Fatal("missing docs accepted")
		}
	}
	put(t, a.Root, "AGENTS.md", "")
	a.writeFile = func(string, string, []byte) error { return errors.New("read only") }
	if code, _, err := invoke(t, a, "policy", "sync"); code != 1 || !strings.Contains(err, "read only") {
		t.Fatal(code, err)
	}
	a.writeFile = nil
	a.Token = "Use Go."
	a.HTTP = &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") })}
	if code, _, _ := invoke(t, a, "policy", "sync"); code != 1 {
		t.Fatal(code)
	}
	a.Token = ""
	if _, err := readPolicy(a.Root, PolicyBlock{File: "../escape"}); err == nil {
		t.Fatal("escape")
	}
	bad := m
	bad.PolicyBlocks = []PolicyBlock{{File: "../escape"}}
	if _, err := localPolicies(a.Root, bad); err == nil {
		t.Fatal("escape")
	}
	if _, err := a.policies(context.Background(), Client{}, bad, true); err == nil {
		t.Fatal("escape")
	}
	os.Remove(filepath.Join(a.Root, "AGENTS.md"))
	os.Mkdir(filepath.Join(a.Root, "AGENTS.md"), 0755)
	if _, err := readPolicy(a.Root, m.PolicyBlocks[0]); err == nil {
		t.Fatal("read directory")
	}
	secretServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"version":"12345678","markdown":"credential-value"}`)
	}))
	defer secretServer.Close()
	b := testApp(t, secretServer)
	b.Token = "credential-value"
	if code, _, _ := invoke(t, b, "policy", "sync"); code != 1 {
		t.Fatal("credential written")
	}
	for _, body := range []string{`{"version":"bad"}`, `{"version":"12345678","markdown":"<!-- wikisvc:end -->"}`, `not-json`} {
		badServer := server(t, map[string]response{"/api/v1/policies/compile": {200, body, ""}})
		if _, err := compilePolicies(context.Background(), Client{Origin: badServer.URL}, PolicyBlock{}); err == nil {
			t.Fatal(body)
		}
	}
}

func TestPolicyManifestFields(t *testing.T) {
	for _, budget := range []int{0, 100001} {
		m := manifest("https://wiki.example")
		m.PolicyBlocks[0].BudgetTokens = &budget
		if err := m.validate(t.TempDir()); err == nil {
			t.Fatal(budget)
		}
	}
	for _, enforced := range []string{"bad", "keep_must", "none", "all"} {
		m := manifest("https://wiki.example")
		m.PolicyBlocks[0].Enforced = enforced
		if err := m.validate(t.TempDir()); (err != nil) != (enforced == "bad") {
			t.Fatal(enforced, err)
		}
	}
	for _, scope := range []string{"", "lang:go,lang:python"} {
		m := manifest("https://wiki.example")
		m.PolicyBlocks[0].Scopes = []string{scope}
		if err := m.validate(t.TempDir()); err == nil {
			t.Fatal(scope)
		}
	}
}
