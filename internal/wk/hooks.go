package wk

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

const blockGuardMessage = "Блок политик генерируется. Измени правило в вике: `wk propose`, затем `wk policy sync`"
const staleContext = `{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"Политики компании изменились, выполни ` + "`wk policy sync`" + `."}}`

// Checks, their configuration and the toolchain that provides wk are changed by a human.
var protectedPaths = []string{"docs/_meta/check.py", ".claude/settings.json", ".codex/hooks.json", manifestFile, "flake.nix", "flake.lock", ".envrc"}

// Directories whose contents configure the checks; shell writes into them are refused.
var protectedDirs = []string{".claude", ".codex", "_meta"}

// Shell cannot be parsed reliably, so a command is treated as writing when it
// contains any of these. False positives cost one Read call; misses cost the gate.
var shellWrite = regexp.MustCompile(`(?:^|[^0-9&>])>|\b(?:tee|rm|mv|cp|ln|dd|truncate|install|chmod|chown|unlink|rsync|touch)\b|\b(?:sed|perl)\b[^|;&\n]*\s-[a-zA-Z]*i|\bgit\s+(?:checkout|restore|reset|stash|apply|am|mv|rm|clean)\b|\b(?:python3?|node|ruby|awk|gawk|bash|sh|zsh|eval|xargs)\b`)
var shellReconfigure = regexp.MustCompile(`\bnix\s+flake\s+(?:update|lock)\b|\bwk\s+init\b`)

type hookEdit struct {
	Old *string `json:"old_string"`
	New *string `json:"new_string"`
	All bool    `json:"replace_all"`
}

type hookEvent struct {
	Cwd            string `json:"cwd"`
	StopHookActive bool   `json:"stop_hook_active"`
	ToolInput      struct {
		File     string          `json:"file_path"`
		Notebook string          `json:"notebook_path"`
		Content  *string         `json:"content"`
		Command  json.RawMessage `json:"command"`
		Edits    []hookEdit      `json:"edits"`
		hookEdit
	} `json:"tool_input"`
}

// hookChange is one file touched by a tool call. Content replaces the whole
// file (an empty string for deletion); otherwise Edits apply in order.
type hookChange struct {
	File    string
	Content *string
	Edits   []hookEdit
}

// Bash and Codex apply_patch carry a string in tool_input.command; some shells pass argv.
func (e hookEvent) command() string {
	var text string
	if json.Unmarshal(e.ToolInput.Command, &text) == nil {
		return text
	}
	var argv []string
	json.Unmarshal(e.ToolInput.Command, &argv)
	return strings.Join(argv, " ")
}

func (e hookEvent) changes() []hookChange {
	if text := e.command(); strings.Contains(text, "*** Begin Patch") {
		return patchChanges(text)
	}
	in := e.ToolInput
	file := in.File
	if file == "" {
		file = in.Notebook
	}
	edits := in.Edits
	if len(edits) == 0 {
		edits = []hookEdit{in.hookEdit}
	}
	return []hookChange{{File: file, Content: in.Content, Edits: edits}}
}

// patchChanges reads the Codex apply_patch envelope. Hunks become replace-all
// edits: matching every occurrence is stricter than Codex's anchored search.
func patchChanges(text string) []hookChange {
	var changes []hookChange
	var removed, added []string
	current, adding := -1, false
	empty := ""
	flush := func() {
		if current >= 0 && adding {
			content := strings.Join(added, "\n")
			changes[current].Content = &content
		} else if current >= 0 && len(removed)+len(added) > 0 {
			old, next := strings.Join(removed, "\n"), strings.Join(added, "\n")
			changes[current].Edits = append(changes[current].Edits, hookEdit{Old: &old, New: &next, All: true})
		}
		removed, added = nil, nil
	}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		directive, file, _ := strings.Cut(line, ": ")
		switch {
		case slices.Contains([]string{"*** Add File", "*** Update File", "*** Delete File", "*** Move to"}, directive):
			flush()
			if directive == "*** Move to" && current >= 0 {
				// The source disappears and the destination is replaced wholesale.
				changes[current].Content = &empty
			}
			changes = append(changes, hookChange{File: strings.TrimSpace(file)})
			current, adding = len(changes)-1, directive == "*** Add File"
			if directive == "*** Delete File" || directive == "*** Move to" {
				changes[current].Content = &empty
				current = -1
			}
		case strings.HasPrefix(line, "*** ") || strings.HasPrefix(line, "@@"):
			flush()
			if line == "*** End Patch" {
				current = -1
			}
		case current < 0 || line == "":
		case line[0] == ' ':
			removed, added = append(removed, line[1:]), append(added, line[1:])
		case line[0] == '-':
			removed = append(removed, line[1:])
		case line[0] == '+':
			added = append(added, line[1:])
		}
	}
	flush()
	return changes
}

func (a App) hook(ctx context.Context, args []string) (result, error) {
	if len(args) != 1 || !slices.Contains([]string{"check", "guard-block", "guard-paths", "guard-shell", "policy-stale"}, args[0]) {
		return result{}, exitError{2, errors.New("используй hook check|guard-block|guard-paths|guard-shell|policy-stale; JSON события передай на stdin")}
	}
	if a.Stdin == nil {
		return result{}, nil
	}
	const limit = 16 << 20
	data, err := io.ReadAll(io.LimitReader(a.Stdin, limit+1))
	var event *hookEvent
	if err != nil || len(data) > limit || json.Unmarshal(data, &event) != nil || event == nil {
		return result{}, nil
	}
	if args[0] == "check" && event.StopHookActive {
		return result{}, nil
	}
	cwd := a.Root
	if filepath.IsAbs(event.Cwd) {
		cwd = event.Cwd
	}
	// A harness may start inside a component directory. Find the nearest manifest.
	a.Root = filepath.Clean(cwd)
	var m Manifest
	for {
		m, err = loadManifest(a.Root)
		if err == nil {
			break
		}
		parent := filepath.Dir(a.Root)
		if !errors.Is(err, os.ErrNotExist) || parent == a.Root {
			return result{}, nil
		}
		a.Root = parent
	}
	switch args[0] {
	case "guard-paths":
		return guardPaths(a.Root, cwd, event.changes()), nil
	case "guard-block":
		return a.guardBlock(m, cwd, event.changes()), nil
	case "guard-shell":
		return a.guardShell(m, cwd, *event), nil
	}
	// Leave time for reporting before the harness's ten-second deadline.
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	c := Client{Origin: m.Wiki.URL, Token: a.Token, HTTP: a.HTTP}
	if args[0] == "policy-stale" {
		if pol, err := a.policies(ctx, c, m, false); err == nil && pol.Code != 0 {
			return result{Text: staleContext}, nil
		}
		return result{}, nil
	}
	// Documentation is checked locally first: a missing wiki must not open the gate for docs.
	// Hooks only inspect files; they must not regenerate the documentation registry.
	rep, err := a.docsReport(m, m.Docs.Root, ".", false)
	if err != nil {
		return result{Code: 2, Stderr: "Проверка документации не пройдена: " + oneLine(err.Error()) + "; выполни `wk docs check --strict`."}, nil
	}
	pol, err := a.policies(ctx, c, m, false)
	if err != nil {
		var unavailable exitError
		if !errors.As(err, &unavailable) || unavailable.code != 3 {
			return result{Code: 2, Stderr: "Проверка политик не пройдена: " + oneLine(err.Error()) + "; выполни `wk policy check`."}, nil
		}
		if rep.Code(true) != 0 {
			return result{Code: 2, Stderr: oneLine(rep.Text()) + "; выполни `wk docs check --strict` (вика недоступна, политики не проверены)."}, nil
		}
		return result{Stderr: "wk hook: сеть недоступна; документация в порядке, проверка политик пропущена, повтори `wk check --strict` при доступной вике."}, nil
	}
	if rep.Code(true) != 0 || pol.Code != 0 {
		return result{Code: 2, Stderr: oneLine(rep.Text()+"\n"+pol.Text) + "; выполни `wk check --strict`."}, nil
	}
	return result{}, nil
}

func guardPaths(root, cwd string, changes []hookChange) result {
	for _, change := range changes {
		if change.File == "" {
			continue
		}
		path := hookPath(root, cwd, change.File)
		// Protect the named config itself even when it points outside the project.
		named := change.File
		if !filepath.IsAbs(named) {
			named = filepath.Join(cwd, named)
		}
		rel, _ := filepath.Rel(root, named)
		if slices.Contains(protectedPaths, filepath.ToSlash(rel)) {
			path = filepath.ToSlash(rel)
		}
		if slices.Contains(protectedPaths, path) {
			return result{Code: 2, Stderr: "Файл " + path + " защищён: изменение проверок и их конфигурации выполняет человек."}
		}
	}
	return result{}
}

func (a App) guardBlock(m Manifest, cwd string, changes []hookChange) result {
	for _, change := range changes {
		path := hookPath(a.Root, cwd, change.File)
		if path == "" {
			continue
		}
		for _, block := range m.PolicyBlocks {
			if block.File == path {
				current, err := readPolicy(a.Root, block)
				if err == nil && changesPolicyBlock(current, change) {
					return result{Code: 2, Stderr: blockGuardMessage}
				}
			}
		}
	}
	return result{}
}

// guardShell is a tripwire, not a parser: it refuses commands that name the
// protected files, write into protected places or re-pin the toolchain.
// CI with `wk check --strict` remains the guarantee.
func (a App) guardShell(m Manifest, cwd string, event hookEvent) result {
	text := event.command()
	if text == "" {
		return result{}
	}
	if strings.Contains(text, "*** Begin Patch") {
		if res := guardPaths(a.Root, cwd, event.changes()); res.Code != 0 {
			return res
		}
		if res := a.guardBlock(m, cwd, event.changes()); res.Code != 0 {
			return res
		}
	}
	lower := strings.ToLower(text)
	refuse := func(what string) result {
		return result{Code: 2, Stderr: "Команда затрагивает " + what + ": изменение проверок и их конфигурации выполняет человек; файлы читай инструментом Read."}
	}
	for _, protected := range protectedPaths {
		dir, base := path.Split(protected)
		if strings.Contains(lower, strings.ToLower(base)) && (dir == "" || strings.Contains(lower, path.Base(dir))) {
			return refuse(protected)
		}
	}
	if match := shellReconfigure.FindString(text); match != "" {
		return refuse("`" + match + "`")
	}
	if !shellWrite.MatchString(text) {
		return result{}
	}
	targets := slices.Clone(protectedDirs)
	for _, block := range m.PolicyBlocks {
		targets = append(targets, block.File)
	}
	for _, target := range targets {
		if strings.Contains(lower, strings.ToLower(target)) {
			return refuse(target)
		}
	}
	return result{}
}

func oneLine(text string) string { return strings.Join(strings.Fields(text), " ") }

func hookPath(root, cwd, path string) string {
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	// Resolve existing ancestors of the target too: cwd and file_path can use
	// different spellings of a symlinked ancestor, and new files do not exist yet.
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return ""
	}
	path = resolveHookTarget(filepath.Clean(path))
	rel, err := filepath.Rel(resolvedRoot, path)
	if err != nil || relativePath(filepath.ToSlash(rel)) != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}

func resolveHookTarget(path string) string {
	parent := filepath.Dir(path)
	if parent == path {
		return path
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return filepath.Join(resolveHookTarget(parent), filepath.Base(path))
}

// Keep marker lines protected too, including an unfinished generated block.
func protectedPolicyRanges(text string) [][2]int {
	var ranges [][2]int
	for offset := 0; offset < len(text); {
		start := strings.Index(text[offset:], "<!-- wikisvc:begin")
		if start < 0 {
			break
		}
		start += offset
		end := strings.Index(text[start:], "<!-- wikisvc:end -->")
		if end < 0 {
			end = len(text)
		} else {
			end += start + len("<!-- wikisvc:end -->")
			if newline := strings.IndexByte(text[end:], '\n'); newline >= 0 {
				end += newline + 1
			} else {
				end = len(text)
			}
		}
		start = strings.LastIndexByte(text[:start], '\n') + 1
		ranges = append(ranges, [2]int{start, end})
		offset = end
	}
	return ranges
}

func changesPolicyBlock(current string, change hookChange) bool {
	if change.Content != nil {
		// A whole-file Write may change both sides of an intact generated block.
		before, after := protectedPolicyRanges(current), protectedPolicyRanges(*change.Content)
		if len(before) != len(after) {
			return true
		}
		for i, span := range before {
			if current[span[0]:span[1]] != (*change.Content)[after[i][0]:after[i][1]] {
				return true
			}
		}
		return false
	}
	for _, edit := range change.Edits {
		if edit.Old == nil || edit.New == nil {
			continue
		}
		if *edit.Old == "" {
			// Pure insertions cannot touch the block but may forge a new one.
			if strings.Contains(*edit.New, "<!-- wikisvc:") {
				return true
			}
			continue
		}
		ranges := protectedPolicyRanges(current)
		for offset := 0; offset <= len(current); {
			index := strings.Index(current[offset:], *edit.Old)
			if index < 0 {
				break
			}
			start := offset + index
			end := start + len(*edit.Old)
			for _, span := range ranges {
				if start < span[1] && end > span[0] {
					return true
				}
			}
			offset = end
			if !edit.All {
				break
			}
		}
		count := 1
		if edit.All {
			count = -1
		}
		updated := strings.Replace(current, *edit.Old, *edit.New, count)
		// An edit outside a block must not inject new policy markers either.
		if len(protectedPolicyRanges(updated)) != len(ranges) {
			return true
		}
		current = updated
	}
	return false
}
