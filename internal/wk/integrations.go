package wk

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"
)

var skillNames = []string{"policy-sync", "doc-card", "implement-planned", "propose-rule"}
var skillDirs = []string{".claude/skills", ".agents/skills"}
var skillStep = regexp.MustCompile(`(?m)^\d+[.)] .+$`)
var bareURL = regexp.MustCompile(`(?i)(?:[a-z][a-z0-9+.-]*://|www\.)\S+`)

func (a App) integrationBundle() fs.FS {
	if a.integrations != nil {
		return a.integrations
	}
	return assets
}

func (a App) installIntegrations(root, project string) (result, error) {
	plan, notices, err := planIntegrations(root, a.integrationBundle())
	if err != nil {
		return result{}, err
	}
	if a.Token != "" {
		for name, data := range plan {
			if strings.Contains(name, a.Token) || strings.Contains(string(data), a.Token) {
				return result{}, errors.New("credential не может быть записан на диск")
			}
		}
	}
	for _, name := range sortedKeys(plan) {
		if err := a.write(root, name, plan[name]); err != nil {
			return result{}, err
		}
	}
	message := "Проект уже инициализирован; без изменений"
	if len(plan) > 0 {
		message = "Установлены хуки и навыки; изменения не закоммичены"
	}
	return result{Data: map[string]any{"project": project, "changed": len(plan) > 0, "files": sortedKeys(plan), "integration_diff": notices}, Text: message, Stderr: notices}, nil
}

func validateSkill(name string, data []byte) error {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if !utf8.Valid(data) || len(lines) < 5 || lines[0] != "---" {
		return errors.New("SKILL.md должен начинаться с --- на первой строке (UTF-8)")
	}
	// Exactly two single-line fields; folded/block YAML scalars are not portable.
	if lines[3] != "---" {
		return errors.New("frontmatter должен содержать ровно name и description, по одной строке")
	}
	var meta struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	if err := decodeYAML([]byte(strings.Join(lines[1:3], "\n")), &meta, false); err != nil {
		return fmt.Errorf("frontmatter: %w", err)
	}
	if meta.Name != name {
		return errors.New("name должен совпадать с именем каталога")
	}
	if strings.TrimSpace(meta.Description) == "" || strings.ContainsAny(meta.Description, "\r\n") || strings.Contains(meta.Description, ": ") || utf8.RuneCountInString(meta.Description) > 1536 {
		return errors.New("description: нужна одна непустая строка без ': ', не длиннее 1536 символов")
	}
	body := strings.Join(lines[4:], "\n")
	steps := skillStep.FindAllStringIndex(body, -1)
	if len(steps) == 0 || !strings.HasSuffix(strings.TrimRight(body[steps[len(steps)-1][0]:], "` .\t\r\n"), "wk check --strict") {
		return errors.New("последний шаг должен заканчиваться на wk check --strict")
	}
	lower := strings.ToLower(text)
	for _, forbidden := range []string{"curl", "wiki_token", "желательно", "обычно", "при необходимости"} {
		if strings.Contains(lower, forbidden) {
			return fmt.Errorf("запрещённый текст в навыке: %s", forbidden)
		}
	}
	if bareURL.MatchString(text) {
		return errors.New("голые URL в навыке запрещены")
	}
	return nil
}

func integrationFiles(bundle fs.FS) (map[string][]byte, error) {
	files := map[string][]byte{}
	for _, name := range skillNames {
		data, err := fs.ReadFile(bundle, "assets/skills/"+name+"/SKILL.md")
		if err != nil {
			return nil, err
		}
		if err := validateSkill(name, data); err != nil {
			return nil, fmt.Errorf("навык %s: %w", name, err)
		}
		for _, dir := range skillDirs {
			files[dir+"/"+name+"/SKILL.md"] = data
		}
	}
	for target, source := range map[string]string{".claude/settings.json": "claude-settings.json", ".codex/hooks.json": "codex-hooks.json"} {
		data, err := fs.ReadFile(bundle, "assets/hooks/"+source)
		if err != nil {
			return nil, err
		}
		files[target] = data
	}
	return files, nil
}

// Existing harness configuration and skills belong to the project. Even --force
// cannot overwrite them; return reviewable diffs and install only missing files.
func planIntegrations(root string, bundle fs.FS) (map[string][]byte, string, error) {
	files, err := integrationFiles(bundle)
	if err != nil {
		return nil, "", err
	}
	var notices []string
	for _, name := range sortedKeys(files) {
		path, err := safePath(root, name)
		if err != nil {
			return nil, "", err
		}
		old, err := readLimited(path, 8<<20)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, "", err
		}
		if strings.HasSuffix(name, "/SKILL.md") {
			parts := strings.Split(name, "/")
			if err := validateSkill(parts[len(parts)-2], old); err != nil {
				return nil, "", fmt.Errorf("%s: %w", name, err)
			}
		}
		if !bytes.Equal(old, files[name]) {
			notices = append(notices, "Существующий "+name+" сохранён; примени изменения вручную:\n"+integrationDiff(name, string(old), string(files[name])))
		}
		delete(files, name)
	}
	return files, strings.Join(notices, "\n"), nil
}

func integrationDiff(name, old, next string) string {
	var diff strings.Builder
	before := strings.SplitAfter(old, "\n")
	after := strings.SplitAfter(next, "\n")
	if before[len(before)-1] == "" {
		before = before[:len(before)-1]
	}
	if after[len(after)-1] == "" {
		after = after[:len(after)-1]
	}
	fmt.Fprintf(&diff, "--- a/%s\n+++ b/%s\n@@ -1,%d +1,%d @@\n", name, name, len(before), len(after))
	for i, lines := range [][]string{before, after} {
		for _, line := range lines {
			diff.WriteByte("-+"[i])
			diff.WriteString(line)
			if !strings.HasSuffix(line, "\n") {
				diff.WriteString("\n\\ No newline at end of file\n")
			}
		}
	}
	return diff.String()
}

func checkSkills(root string) error {
	var failures []string
	for _, dir := range skillDirs {
		for _, name := range skillNames {
			file := dir + "/" + name + "/SKILL.md"
			path, err := safePath(root, file)
			if err == nil {
				var data []byte
				data, err = readLimited(path, 64<<10)
				if err == nil {
					err = validateSkill(name, data)
				}
			}
			if err != nil {
				failures = append(failures, file+": "+err.Error())
			}
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	return nil
}
