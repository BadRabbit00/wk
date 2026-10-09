package wk

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const blockGuardMessage = "Блок политик генерируется. Измени правило в вике: `wk propose`, затем `wk policy sync`"
const staleContext = `{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"Политики компании изменились, выполни ` + "`wk policy sync`" + `."}}`

type hookEdit struct {
	Old *string `json:"old_string"`
	New *string `json:"new_string"`
	All bool    `json:"replace_all"`
}

type hookEvent struct {
	Cwd            string `json:"cwd"`
	StopHookActive bool   `json:"stop_hook_active"`
	ToolInput      struct {
		File    string     `json:"file_path"`
		Content *string    `json:"content"`
		Edits   []hookEdit `json:"edits"`
		hookEdit
	} `json:"tool_input"`
}

func (a App) hook(ctx context.Context, args []string) (result, error) {
	if len(args) != 1 || !slices.Contains([]string{"check", "guard-block", "guard-paths", "policy-stale"}, args[0]) {
		return result{}, exitError{2, errors.New("используй hook check|guard-block|guard-paths|policy-stale; JSON события передай на stdin")}
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
	if strings.HasPrefix(args[0], "guard-") {
		path := hookPath(a.Root, cwd, event.ToolInput.File)
		if args[0] == "guard-paths" {
			protected := []string{"docs/_meta/check.py", ".claude/settings.json", ".codex/hooks.json", manifestFile}
			// Protect the named config itself even when it points outside the project.
			named := event.ToolInput.File
			if !filepath.IsAbs(named) {
				named = filepath.Join(cwd, named)
			}
			rel, _ := filepath.Rel(a.Root, named)
			if slices.Contains(protected, filepath.ToSlash(rel)) {
				path = filepath.ToSlash(rel)
			}
			if slices.Contains(protected, path) {
				return result{Code: 2, Stderr: "Файл " + path + " защищён: изменение проверок и их конфигурации выполняет человек."}, nil
			}
			return result{}, nil
		}
		if path == "" {
			return result{}, nil
		}
		for _, block := range m.PolicyBlocks {
			if block.File == path {
				current, err := readPolicy(a.Root, block)
				if err == nil && changesPolicyBlock(current, *event) {
					return result{Code: 2, Stderr: blockGuardMessage}, nil
				}
			}
		}
		return result{}, nil
	}
	// Leave time for reporting before the harness's ten-second deadline.
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	c := Client{Origin: m.Wiki.URL, Token: a.Token, HTTP: a.HTTP}
	pol, err := a.policies(ctx, c, m, false)
	if args[0] == "policy-stale" {
		if err == nil && pol.Code != 0 {
			return result{Text: staleContext}, nil
		}
		return result{}, nil
	}
	if err != nil {
		var unavailable exitError
		if errors.As(err, &unavailable) && unavailable.code == 3 {
			return result{Stderr: "wk hook: сеть недоступна; проверка политик пропущена, повтори `wk check --strict` при доступной вике."}, nil
		}
		return result{Code: 2, Stderr: "Проверка политик не пройдена: " + oneLine(err.Error()) + "; выполни `wk policy check`."}, nil
	}
	// Hooks only inspect files; they must not regenerate the documentation registry.
	rep, err := a.docsReport(m, m.Docs.Root, ".", false)
	if err != nil {
		return result{Code: 2, Stderr: "Проверка документации не пройдена: " + oneLine(err.Error()) + "; выполни `wk docs check --strict`."}, nil
	}
	if rep.Code(true) != 0 || pol.Code != 0 {
		return result{Code: 2, Stderr: oneLine(rep.Text()+"\n"+pol.Text) + "; выполни `wk check --strict`."}, nil
	}
	return result{}, nil
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

func changesPolicyBlock(current string, event hookEvent) bool {
	input := event.ToolInput
	if input.Content != nil {
		// A whole-file Write may change both sides of an intact generated block.
		before, after := protectedPolicyRanges(current), protectedPolicyRanges(*input.Content)
		if len(before) != len(after) {
			return true
		}
		for i, span := range before {
			if current[span[0]:span[1]] != (*input.Content)[after[i][0]:after[i][1]] {
				return true
			}
		}
		return false
	}
	edits := input.Edits
	if len(edits) == 0 {
		edits = []hookEdit{input.hookEdit}
	}
	for _, edit := range edits {
		if edit.Old == nil || edit.New == nil || *edit.Old == "" {
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
