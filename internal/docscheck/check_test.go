package docscheck

import (
	"reflect"
	"strings"
	"testing"
)

func TestLexicalCompatibility(t *testing.T) {
	if got := Lines("a\r\nb\rc\vd\fe\x1cf\x1dg\x1eh\u0085i\u2028j\u2029last"); !reflect.DeepEqual(got, []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "last"}) {
		t.Fatal(got)
	}
	if got := references("яENT-hidden _ENT-hidden -ENT-hidden ²ENT-hidden ENT-good#part ENT-group.*", refRE); len(got) != 2 || got[0][1] != "ENT-good" || got[1][2] != ".*" {
		t.Fatal(got)
	}
	for _, s := range []string{"nothing", "bad-key: x", "key: yes | missing"} {
		if len(parseMeta(s)) != 0 {
			t.Fatal(s)
		}
	}
	if !reflect.DeepEqual(parseMeta("`STATUS`: `draft` | OWNER: `SVC-api`"), map[string]string{"status": "draft", "owner": "SVC-api"}) {
		t.Fatal("meta")
	}
	if got := splitRow("| `id` | a\\|b | last\\|"); !reflect.DeepEqual(got, []string{"id", `a\|b`, `last\|`}) {
		t.Fatal(got)
	}
	for _, path := range []string{"_meta/x.md", "components/_template/a.md", "nested/TeMplate.md", "code.go"} {
		if IsDoc(path) {
			t.Fatal(path)
		}
	}
	if !IsDoc("_index.md") {
		t.Fatal("filename starting underscore must remain visible")
	}
}

func TestValidationBoundaries(t *testing.T) {
	docs := []File{
		{"system/domain.md", `# Domain
## ENT-main — Title | pipe
status: strange | replaced_by: ENT-absent
ENT-main#own
## ENT-missing — Missing metadata
not metadata
## ENT-draft — Draft
status: draft
## ENT-old — Old
status: deprecated
## TERM-elsewhere — Term
## API-no-owner — API
status: implemented
## API-invalid-owner — API
status: planned | owner: ENT-main
ENT-group.* ENT-none.* ENT-absent
### ENT-group.child — Child
status: planned
## ADR-wrong — ADR
status: proposed
## Q-wrong — Q
status: resolved
## RISK-wrong — Risk
status: closed
##### ENT-not-definition — Deep heading
~~~
`},
		{"components/api/component.md", "## SVC-api — API\nstatus: draft\n" + strings.Repeat("\n", 121)},
		{"components/lib/component.md", "## LIB-other — Library\nstatus: draft\n"},
		{"components/api/contracts.md", `| id | status | owner |
|---|---|---|
| API-mismatch | draft | LIB-other |
| API-unknown | draft | SVC-unknown |

## API-no-meta
## API-marker
status: planned | owner: SVC-api
`},
		{"decisions/one.md", "## ADR-good — Decision\nstatus: accepted\n"},
		{"table.md", "| id | ignored | название |\n|---|---|---|\n| TERM-one | fallback | preferred |\n| TERM-two | fallback |\n| TERM-only |\n"},
	}
	code := []File{{"src/a.go", "// doc: ENT-draft ENT-old API-marker ENT-unknown\n"}, {"src/invalid.go", string([]byte{0xff}) + "// doc: ENT-draft\n"}, {"big", strings.Repeat("x", 2000001)}, {"binary", "\x00// doc: ENT-binary"}, {"plain", "nothing"}}
	rep := Check(docs, code, nil)
	for _, expected := range []string{"status 'strange' не из", "нет status", "replaced_by → ENT-absent", "нет owner", "owner 'ENT-main'", "owner 'SVC-unknown'", "ENT-none.*: под шаблон", "маркер doc: ENT-unknown"} {
		if !strings.Contains(strings.Join(rep.Errors, "\n"), expected) {
			t.Errorf("missing %s: %v", expected, rep.Errors)
		}
	}
	for _, expected := range []string{"тип ADR определяется в decisions/*.md", "тип API определяется в components/<c>/contracts.md", "owner LIB-other, но файл лежит в папке SVC-api", "implemented, но в коде нет маркера", "deprecated, но ещё используется", "status draft, но в коде уже есть маркер", "при бюджете 120"} {
		if !strings.Contains(strings.Join(rep.Warnings, "\n"), expected) {
			t.Errorf("missing %s: %v", expected, rep.Warnings)
		}
	}
	for _, expected := range []string{`Title \| pipe`, "| TERM-only |  |", "| TERM-one | preferred |", "| TERM-two | fallback |", "| ENT-group.child"} {
		if !strings.Contains(rep.Registry, expected) {
			t.Error(expected, rep.Registry)
		}
	}
	if rep.Code(true) != 1 {
		t.Fatal("invalid accepted")
	}
}

func TestExternalPoliciesAndReferences(t *testing.T) {
	rep := Check([]File{{"system/domain.md", `# Document
rule-known rule-unknown
## rule-illegal — Not local
| id | title |
|---|---|
| rule-illegal-table | no |
`}}, nil, []Policy{{"rule-known", "must", "go"}, {"rule-known", "must", "other"}, {"rule-another", "should", "go"}})
	if !strings.Contains(rep.Registry, "## Политики компании") || !strings.Contains(rep.Registry, "| rule-known | must | go |") {
		t.Fatal(rep.Registry)
	}
	if !strings.Contains(strings.Join(rep.Errors, "\n"), "политики определяются в вике, здесь только ссылки") || !strings.Contains(rep.Text(), "политика не входит в профиль проекта") {
		t.Fatal(rep.Text())
	}
	defs := []Definition{{"ENT-a", "", "system/domain.md", 1, map[string]string{}}, {"ENT-b", "", "system/domain.md", 2, map[string]string{}}}
	refs := []Ref{{Target: "ENT-a", User: "ENT-a"}, {Target: "ENT-a", User: "other.md"}, {Target: "ENT-a", User: "other.md"}, {Target: "ENT-none.*", User: "reader"}}
	if got := registry(defs, refs, nil, nil); !strings.Contains(got, "—: 2") || strings.Count(got, "`other.md`") != 0 || strings.Count(got, "other.md") != 1 {
		t.Fatal(got)
	}
	if short([]string{"a/b", "x", "y"}, 1) != "`a/b` +2" {
		t.Fatal("short")
	}
	clean := Check(nil, nil, nil)
	if clean.Code(false) != 0 || clean.Code(true) != 0 {
		t.Fatal(clean)
	}
	if (Report{Warnings: []string{"warn"}}).Code(false) != 0 || (Report{Warnings: []string{"warn"}}).Code(true) != 1 {
		t.Fatal("strict warning")
	}
}
