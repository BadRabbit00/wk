// Package docscheck is the built-in port of the docs-kit checker. The frozen
// Python oracle lives under testdata; project files cannot replace these rules.
package docscheck

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

type File struct{ Path, Text string }
type Policy struct{ ID, Level, Profile string }
type Definition struct {
	ID, Title, File string
	Line            int
	Meta            map[string]string
}
type Ref struct {
	Target, File, User string
	Line               int
	Wildcard           bool
}
type Report struct {
	Errors     []string `json:"errors"`
	Warnings   []string `json:"warnings"`
	IDs        int      `json:"ids"`
	References int      `json:"references"`
	Markers    int      `json:"markers"`
	Registry   string   `json:"-"`
}
type rule struct{ prefix, location string }

var locations = []rule{
	{"ACT", "system/overview.md"}, {"EXT", "system/overview.md"}, {"TERM", "system/glossary.md"},
	{"ENT", "system/domain.md"}, {"STATE", "system/domain.md"}, {"INV", "system/domain.md"}, {"FLOW", "system/flows.md"},
	{"ROLE", "system/security.md"}, {"PERM", "system/security.md"}, {"SEC", "system/security.md"}, {"ERR", "system/errors.md"}, {"NFR", "system/nfr.md"},
	{"SVC", "@component.md"}, {"LIB", "@component.md"}, {"MOD", "@code.md"}, {"CLS", "@code.md"},
	{"API", "@contracts.md"}, {"EVT", "@contracts.md"}, {"CLI", "@contracts.md"}, {"DTO", "@contracts.md"}, {"FMT", "@contracts.md"}, {"PRM", "@ai.md"},
	{"TBL", "@data.md"}, {"KV", "@data.md"}, {"BKT", "@data.md"}, {"JOB", "@runtime.md"}, {"CFG", "@runtime.md"},
	{"LANG", "stack/languages.md"}, {"TOOL", "stack/languages.md"}, {"DEP", "stack/dependencies.md"}, {"MDL", "stack/models.md"},
	{"RES", "infra/resources.md"}, {"ENV", "infra/resources.md"}, {"PIPE", "infra/delivery.md"}, {"OBS", "infra/observability.md"},
	{"ADR", "decisions/"}, {"Q", "open-questions.md"}, {"RISK", "risks.md"},
}
var groups = []struct {
	name     string
	prefixes []string
}{
	{"Система и домен", strings.Fields("ACT EXT TERM ENT STATE INV FLOW NFR")},
	{"Безопасность и ошибки", strings.Fields("ROLE PERM SEC ERR")},
	{"Компоненты и код", strings.Fields("SVC LIB MOD CLS")},
	{"Контракты", strings.Fields("API EVT CLI DTO FMT PRM")},
	{"Данные", strings.Fields("TBL KV BKT")}, {"Рантайм", strings.Fields("JOB CFG")},
	{"Стек", strings.Fields("LANG TOOL DEP MDL")}, {"Инфраструктура", strings.Fields("RES ENV PIPE OBS")},
	{"Решения и незакрытое", strings.Fields("ADR Q RISK")},
}
var owned = strings.Fields("API EVT CLI DTO FMT PRM TBL KV BKT MOD CLS JOB CFG")
var titles = strings.Split("название|термин|кто|роль|право|класс|когда возникает|требование|вопрос|что|пакет|язык и версия|инструмент|назначение|имя / путь|на что влияет", "|")
var SkipCodeDirs = strings.Fields(".git .hg node_modules target .venv venv __pycache__ dist build vendor .direnv result .mypy_cache .pytest_cache .next .turbo")

const namePattern = `[A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?`
const spacePattern = `[\s\x{001c}-\x{001f}\x{0085}\p{Z}]`

var idPattern = func() string {
	prefixes := make([]string, len(locations))
	for i, r := range locations {
		prefixes[i] = r.prefix
	}
	sort.SliceStable(prefixes, func(i, j int) bool { return len(prefixes[i]) > len(prefixes[j]) })
	return `(?:` + strings.Join(prefixes, "|") + `)-` + namePattern
}()
var refRE = regexp.MustCompile(`(` + idPattern + `)(\.\*)?(#[A-Za-z0-9_.-]+)?`)
var fullRE = regexp.MustCompile(`^` + idPattern + `$`)
var headingRE = regexp.MustCompile(`^(#{1,6})` + spacePattern + `+(.*?)` + spacePattern + `*$`)
var headDefRE = regexp.MustCompile(`^(` + idPattern + `)(?:` + spacePattern + `+[—–-]` + spacePattern + `+(.*))?$`)
var markerRE = regexp.MustCompile(`(?://|#|--|/\*|<!--|;)` + spacePattern + `*doc:` + spacePattern + `*(.+)`)
var metaKeyRE = regexp.MustCompile(`^[a-z_]+$`)
var dividerRE = regexp.MustCompile(`^:?-{2,}:?$`)
var externalRE = regexp.MustCompile(`rule-[a-z0-9]+(?:-[a-z0-9]+)*`)
var externalDefRE = regexp.MustCompile(`^(rule-[a-z0-9]+(?:-[a-z0-9]+)*)(?:` + spacePattern + `+[—–-]` + spacePattern + `+.*)?$`)

func prefix(id string) string { return strings.SplitN(id, "-", 2)[0] }
func pySpace(r rune) bool     { return unicode.IsSpace(r) || r >= 0x1c && r <= 0x1f }
func trim(s string) string    { return strings.TrimFunc(s, pySpace) }

// Lines mirrors str.splitlines, including CRLF, Unicode separators and no final empty line.
func Lines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var out []string
	start := 0
	for i, r := range text {
		if strings.ContainsRune("\n\r\v\f\x1c\x1d\x1e\u0085\u2028\u2029", r) {
			out = append(out, text[start:i])
			start = i + utf8.RuneLen(r)
		}
	}
	if start < len(text) {
		out = append(out, text[start:])
	}
	return out
}
func references(text string, re *regexp.Regexp) [][]string {
	var out [][]string
	for _, m := range re.FindAllStringSubmatchIndex(text, -1) {
		if m[0] > 0 {
			r, _ := utf8.DecodeLastRuneInString(text[:m[0]])
			if r == '_' || r == '-' || unicode.IsLetter(r) || unicode.IsNumber(r) {
				continue
			}
		}
		values := make([]string, len(m)/2)
		for i := range values {
			if m[2*i] >= 0 {
				values[i] = text[m[2*i]:m[2*i+1]]
			}
		}
		out = append(out, values)
	}
	return out
}
func parseMeta(line string) map[string]string {
	meta := map[string]string{}
	for _, chunk := range strings.Split(line, " | ") {
		key, value, ok := strings.Cut(chunk, ":")
		key = strings.ToLower(strings.Trim(trim(key), "`"))
		if !ok || !metaKeyRE.MatchString(key) {
			return map[string]string{}
		}
		meta[key] = strings.Trim(trim(value), "`")
	}
	return meta
}
func splitRow(line string) []string {
	s := strings.TrimPrefix(trim(line), "|")
	if strings.HasSuffix(s, "|") && !strings.HasSuffix(s, `\|`) {
		s = s[:len(s)-1]
	}
	var cells []string
	start := 0
	for i, r := range s {
		if r == '|' && (i == 0 || s[i-1] != '\\') {
			cells = append(cells, s[start:i])
			start = i + 1
		}
	}
	cells = append(cells, s[start:])
	for i, c := range cells {
		cells[i] = trim(strings.Trim(trim(c), "`"))
	}
	return cells
}
func (r *Report) err(where, msg string)  { r.Errors = append(r.Errors, where+": "+msg) }
func (r *Report) warn(where, msg string) { r.Warnings = append(r.Warnings, where+": "+msg) }
func where(file string, line int) string { return fmt.Sprintf("%s:%d", file, line) }
func IsDoc(path string) bool {
	parts := strings.Split(path, "/")
	for _, part := range parts[:len(parts)-1] {
		if strings.HasPrefix(part, "_") {
			return false
		}
	}
	return strings.HasSuffix(path, ".md") && !strings.Contains(strings.ToLower(parts[len(parts)-1]), "template")
}
func addDef(defs *[]Definition, index map[string]Definition, d Definition, rep *Report) {
	if old, ok := index[d.ID]; ok {
		rep.err(where(d.File, d.Line), fmt.Sprintf("%s уже определён в %s:%d", d.ID, old.File, old.Line))
		return
	}
	index[d.ID] = d
	*defs = append(*defs, d)
}
func parse(files []File, rep *Report, external map[string]Policy) ([]Definition, []Ref, map[string]Definition) {
	var defs []Definition
	var refs []Ref
	index := map[string]Definition{}
	for _, file := range files {
		lines := Lines(file.Text)
		card := ""
		cardLevel := 0
		var headers []string
		fence := false
		for i, raw := range lines {
			stripped := trim(raw)
			skip := ""
			user := ""
			if strings.HasPrefix(stripped, "```") {
				fence = !fence
			}
			var heading []string
			if !fence {
				heading = headingRE.FindStringSubmatch(raw)
			}
			if heading != nil {
				level := len(heading[1])
				if card != "" && level <= cardLevel {
					card = ""
				}
				if externalDefRE.MatchString(heading[2]) && level <= 4 {
					rep.err(where(file.Path, i+1), "политики определяются в вике, здесь только ссылки")
				}
				if hd := headDefRE.FindStringSubmatch(heading[2]); hd != nil && level <= 4 {
					meta := map[string]string{}
					for _, next := range lines[i+1:] {
						if trim(next) != "" {
							meta = parseMeta(trim(next))
							break
						}
					}
					addDef(&defs, index, Definition{hd[1], hd[2], file.Path, i + 1, meta}, rep)
					card = hd[1]
					cardLevel = level
					skip = hd[1]
				}
				headers = nil
			} else if strings.HasPrefix(stripped, "|") && !fence {
				cells := splitRow(stripped)
				if externalDefRE.MatchString(cells[0]) {
					rep.err(where(file.Path, i+1), "политики определяются в вике, здесь только ссылки")
				}
				if headers == nil {
					headers = make([]string, len(cells))
					for j, c := range cells {
						headers[j] = strings.ToLower(c)
					}
					if fullRE.MatchString(cells[0]) {
						rep.err(where(file.Path, i+1), cells[0]+": строка похожа на определение, но у таблицы нет заголовка с колонкой id (лишняя пустая строка?)")
						skip = cells[0]
					}
				} else {
					divider := true
					for _, c := range cells {
						if c != "" && !dividerRE.MatchString(c) {
							divider = false
						}
					}
					if !divider && headers[0] == "id" && fullRE.MatchString(cells[0]) {
						meta := map[string]string{}
						for j, h := range headers {
							if j < len(cells) && slices.Contains([]string{"status", "owner", "replaced_by"}, h) {
								meta[h] = cells[j]
							}
						}
						title := ""
						if len(cells) > 1 {
							title = cells[1]
						}
						for _, h := range titles {
							if j := slices.Index(headers, h); j >= 0 && j < len(cells) {
								title = cells[j]
								break
							}
						}
						addDef(&defs, index, Definition{cells[0], title, file.Path, i + 1, meta}, rep)
						skip = cells[0]
						user = cells[0]
					}
				}
			} else {
				headers = nil
			}
			if user == "" {
				user = card
			}
			if user == "" {
				user = file.Path
			}
			for _, m := range references(raw, refRE) {
				base, star := m[1], m[2] != ""
				if base == skip && !star {
					skip = ""
					continue
				}
				if base == user && !star {
					continue
				}
				target := base
				if star {
					target += ".*"
				}
				refs = append(refs, Ref{target, file.Path, user, i + 1, star})
			}
			for _, m := range references(raw, externalRE) {
				if _, ok := external[m[0]]; !ok {
					rep.err(where(file.Path, i+1), m[0]+": политика не входит в профиль проекта, проверь `wk policy sync` или профиль в манифесте")
				}
			}
		}
	}
	return defs, refs, index
}
func expected(p, path string) string {
	i := slices.IndexFunc(locations, func(r rule) bool { return r.prefix == p })
	target := locations[i].location
	if strings.HasPrefix(target, "@") {
		target = target[1:]
		parts := strings.Split(path, "/")
		if len(parts) == 3 && parts[0] == "components" && parts[2] == target {
			return ""
		}
		return "components/<c>/" + target
	}
	if strings.HasSuffix(target, "/") {
		if strings.HasPrefix(path, target) {
			return ""
		}
		return target + "*.md"
	}
	if path == target {
		return ""
	}
	return target
}
func checkDefs(defs []Definition, index map[string]Definition, rep *Report) {
	folders := map[string]string{}
	for _, d := range defs {
		if slices.Contains([]string{"SVC", "LIB"}, prefix(d.ID)) && strings.HasPrefix(d.File, "components/") {
			folders[strings.Split(d.File, "/")[1]] = d.ID
		}
	}
	for _, d := range defs {
		p := prefix(d.ID)
		loc := where(d.File, d.Line)
		if exp := expected(p, d.File); exp != "" {
			rep.warn(loc, fmt.Sprintf("%s: тип %s определяется в %s", d.ID, p, exp))
		}
		allowed := []string{"deprecated", "draft", "implemented", "planned"}
		switch p {
		case "ADR":
			allowed = []string{"accepted", "proposed", "superseded"}
		case "Q":
			allowed = []string{"open", "resolved"}
		case "RISK":
			allowed = []string{"accepted", "closed", "open"}
		case "TERM":
			allowed = nil
		}
		st := d.Meta["status"]
		if allowed != nil {
			if st == "" {
				rep.err(loc, d.ID+": нет status (мета-строка сразу под заголовком или колонка status)")
			} else if !slices.Contains(allowed, st) {
				rep.err(loc, fmt.Sprintf("%s: status '%s' не из ['%s']", d.ID, st, strings.Join(allowed, "', '")))
			}
		}
		if rb := d.Meta["replaced_by"]; rb != "" {
			if _, ok := index[rb]; !ok {
				rep.err(loc, d.ID+": replaced_by → "+rb+" не определён")
			}
		}
		if slices.Contains(owned, p) {
			owner := d.Meta["owner"]
			_, ok := index[owner]
			if owner == "" {
				rep.err(loc, d.ID+": нет owner (SVC-… или LIB-…)")
			} else if !ok || !slices.Contains([]string{"SVC", "LIB"}, prefix(owner)) {
				rep.err(loc, d.ID+": owner '"+owner+"' — не определённый SVC-/LIB-")
			} else if strings.HasPrefix(d.File, "components/") {
				if folder := folders[strings.Split(d.File, "/")[1]]; folder != "" && folder != owner {
					rep.warn(loc, d.ID+": owner "+owner+", но файл лежит в папке "+folder)
				}
			}
		}
	}
}
func checkRefs(defs []Definition, index map[string]Definition, refs []Ref, rep *Report) {
	for _, r := range refs {
		if r.Wildcard {
			stem := strings.TrimSuffix(r.Target, ".*")
			if !slices.ContainsFunc(defs, func(d Definition) bool { return strings.HasPrefix(d.ID, stem+".") || strings.HasPrefix(d.ID, stem+"-") }) {
				rep.err(where(r.File, r.Line), r.Target+": под шаблон не подходит ни один ID")
			}
		} else if _, ok := index[r.Target]; !ok {
			rep.err(where(r.File, r.Line), "ссылка на неопределённый "+r.Target)
		}
	}
}
func scanCode(code []File, defs []Definition, index map[string]Definition, rep *Report) map[string][]string {
	markers := map[string][]string{}
	for _, file := range code {
		if len(file.Text) > 2000000 || strings.Contains(file.Text[:min(len(file.Text), 4096)], "\x00") || !strings.Contains(file.Text, "doc:") {
			continue
		}
		for i, line := range Lines(strings.ToValidUTF8(file.Text, "")) {
			if m := markerRE.FindStringSubmatch(line); m != nil {
				for _, r := range references(m[1], refRE) {
					id := r[1]
					loc := where(file.Path, i+1)
					if _, ok := index[id]; !ok {
						rep.err(loc, "маркер doc: "+id+" — такого ID нет в docs/")
						continue
					}
					markers[id] = append(markers[id], loc)
				}
			}
		}
	}
	for _, d := range defs {
		loc := where(d.File, d.Line)
		st := d.Meta["status"]
		has := len(markers[d.ID]) > 0
		if slices.Contains(owned, prefix(d.ID)) && st == "implemented" && !has {
			rep.warn(loc, d.ID+": implemented, но в коде нет маркера `doc: "+d.ID+"`")
		}
		if has && (st == "draft" || st == "planned") {
			rep.warn(loc, d.ID+": status "+st+", но в коде уже есть маркер ("+markers[d.ID][0]+") — обнови статус")
		}
		if has && st == "deprecated" {
			rep.warn(loc, d.ID+": deprecated, но ещё используется в коде ("+markers[d.ID][0]+")")
		}
	}
	return markers
}
func short(items []string, n int) string {
	shown := make([]string, min(len(items), n))
	for i := range shown {
		shown[i] = items[i]
		if strings.Contains(items[i], "/") {
			shown[i] = "`" + items[i] + "`"
		}
	}
	text := strings.Join(shown, ", ")
	if len(items) > n {
		text += fmt.Sprintf(" +%d", len(items)-n)
	}
	return text
}
func registry(defs []Definition, refs []Ref, markers map[string][]string, policies []Policy) string {
	users := map[string][]string{}
	for _, r := range refs {
		targets := []string{r.Target}
		if r.Wildcard {
			targets = nil
			for _, d := range defs {
				if strings.HasPrefix(d.ID, strings.TrimSuffix(r.Target, ".*")+".") {
					targets = append(targets, d.ID)
				}
			}
		}
		for _, t := range targets {
			if r.User != t && !slices.Contains(users[t], r.User) {
				users[t] = append(users[t], r.User)
			}
		}
	}
	counts := map[string]int{}
	for _, d := range defs {
		st := d.Meta["status"]
		if st == "" {
			st = "—"
		}
		counts[st]++
	}
	var statuses []string
	for st := range counts {
		statuses = append(statuses, st)
	}
	sort.Strings(statuses)
	for i, st := range statuses {
		statuses[i] = fmt.Sprintf("%s: %d", st, counts[st])
	}
	out := []string{"# Реестр ID", "", "> Сгенерировано `docs/_meta/check.py`. Руками не править — перезапусти скрипт.", "> Колонки: где определён, где в коде (маркер `doc:`), кто ссылается.", "", fmt.Sprintf("Всего ID: **%d** · %s", len(defs), strings.Join(statuses, " · ")), ""}
	for _, g := range groups {
		var rows []Definition
		for _, d := range defs {
			if slices.Contains(g.prefixes, prefix(d.ID)) {
				rows = append(rows, d)
			}
		}
		if len(rows) == 0 {
			continue
		}
		sort.Slice(rows, func(i, j int) bool {
			a, b := slices.Index(g.prefixes, prefix(rows[i].ID)), slices.Index(g.prefixes, prefix(rows[j].ID))
			if a != b {
				return a < b
			}
			return rows[i].ID < rows[j].ID
		})
		out = append(out, "## "+g.name, "", "| id | название | status | owner | определён | в коде | используют |", "|---|---|---|---|---|---|---|")
		for _, d := range rows {
			out = append(out, fmt.Sprintf("| %s | %s | %s | %s | `%s:%d` | %s | %s |", d.ID, strings.ReplaceAll(d.Title, "|", `\|`), d.Meta["status"], d.Meta["owner"], d.File, d.Line, short(markers[d.ID], 3), short(users[d.ID], 6)))
		}
		out = append(out, "")
	}
	if len(policies) > 0 {
		out = append(out, "## Политики компании", "", "| id | уровень | профиль |", "|---|---|---|")
		sorted := slices.Clone(policies)
		sort.Slice(sorted, func(i, j int) bool {
			if sorted[i].ID != sorted[j].ID {
				return sorted[i].ID < sorted[j].ID
			}
			return sorted[i].Profile < sorted[j].Profile
		})
		for _, p := range sorted {
			out = append(out, fmt.Sprintf("| %s | %s | %s |", p.ID, p.Level, p.Profile))
		}
		out = append(out, "")
	}
	return strings.Join(out, "\n")
}
func Check(files, code []File, policies []Policy) Report {
	rep := Report{Errors: []string{}, Warnings: []string{}}
	docs := slices.DeleteFunc(slices.Clone(files), func(f File) bool { return !IsDoc(f.Path) })
	sort.Slice(docs, func(i, j int) bool { return docs[i].Path < docs[j].Path })
	external := map[string]Policy{}
	for _, p := range policies {
		external[p.ID] = p
	}
	defs, refs, index := parse(docs, &rep, external)
	checkDefs(defs, index, &rep)
	checkRefs(defs, index, refs, &rep)
	for _, f := range docs {
		limit := 400
		if f.Path == "INDEX.md" {
			limit = 150
		} else if strings.HasPrefix(f.Path, "components/") && strings.HasSuffix(f.Path, "/component.md") {
			limit = 120
		}
		if n := len(Lines(f.Text)); n > limit {
			rep.warn(f.Path, fmt.Sprintf("%d строк при бюджете %d — раздели файл", n, limit))
		}
	}
	markers := scanCode(code, defs, index, &rep)
	rep.IDs = len(defs)
	rep.References = len(refs)
	for _, locs := range markers {
		rep.Markers += len(locs)
	}
	rep.Registry = registry(defs, refs, markers, policies)
	return rep
}
func (r Report) Text() string {
	var b strings.Builder
	for _, e := range r.Errors {
		fmt.Fprintln(&b, "ОШИБКА ", e)
	}
	for _, w := range r.Warnings {
		fmt.Fprintln(&b, "ВНИМАНИЕ", w)
	}
	fmt.Fprintf(&b, "\nID: %d, ссылок: %d, маркеров в коде: %d, ошибок: %d, предупреждений: %d\n", r.IDs, r.References, r.Markers, len(r.Errors), len(r.Warnings))
	return b.String()
}
func (r Report) Code(strict bool) int {
	if len(r.Errors) > 0 || strict && len(r.Warnings) > 0 {
		return 1
	}
	return 0
}
