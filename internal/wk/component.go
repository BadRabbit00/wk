package wk

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var langID = regexp.MustCompile(`^LANG-[a-z0-9][a-z0-9-]*$`)
var statusPlaceholder = regexp.MustCompile(`status: <[^>]*>`)

func componentFiles(source fs.FS, c Component, withAI bool) (map[string][]byte, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	err := fs.WalkDir(source, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("симлинки запрещены в шаблоне компонента")
		}
		if entry.IsDir() {
			return nil
		}
		if path == "ai.md" && !withAI {
			return nil
		}
		data, err := fs.ReadFile(source, path)
		if err != nil {
			return err
		}
		files[path] = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	return renderComponent(files, c, withAI)
}

func renderComponent(files map[string][]byte, c Component, withAI bool) (map[string][]byte, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}
	if _, ok := files["component.md"]; !ok {
		return nil, errors.New("в шаблоне отсутствует component.md")
	}
	output := map[string][]byte{}
	for path, data := range files {
		if path == "ai.md" && !withAI {
			continue
		}
		if len(data) > maxFileBytes {
			return nil, errors.New("слишком большой файл компонента")
		}
		id := strings.ToUpper(c.Kind) + "-" + c.Name
		text := strings.NewReplacer("<Имя компонента>", c.Name, "<компонент>", c.Name, "SVC-<имя>", id, "LIB-<имя>", id, "SVC-<…>", id, "LIB-<…>", id).Replace(string(data))
		text = statusPlaceholder.ReplaceAllString(text, "status: draft")
		text = draftTableStatuses(text)
		if c.Lang != "" {
			text = strings.ReplaceAll(text, "LANG-<…>", c.Lang)
		}
		text = strings.ReplaceAll(text, "kind: <http-service | worker | frontend | bot | cli | lib>", "kind: "+c.Kind)
		output[path] = []byte(text)
	}
	return output, nil
}

func draftTableStatuses(text string) string {
	lines := strings.Split(text, "\n")
	column := -1
	for i, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			column = -1
			continue
		}
		cells := strings.Split(line, "|")
		if column == -1 {
			for j, cell := range cells {
				if strings.TrimSpace(cell) == "status" {
					column = j
					break
				}
			}
			continue
		}
		if column >= len(cells) {
			continue
		}
		value := strings.TrimSpace(cells[column])
		if strings.HasPrefix(value, "<") && strings.HasSuffix(value, ">") {
			cells[column] = " draft "
			lines[i] = strings.Join(cells, "|")
		}
	}
	return strings.Join(lines, "\n")
}

func addIndexRow(data []byte, c Component) ([]byte, error) {
	lines := strings.Split(string(data), "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "## Компоненты" {
			start = i + 1
			break
		}
	}
	if start == -1 {
		return nil, errors.New("в INDEX.md отсутствует раздел Компоненты")
	}
	end := start
	for end < len(lines) && !strings.HasPrefix(lines[end], "## ") {
		end++
	}
	tableStart, tableEnd := -1, -1
	for i := start; i < end; i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "|") {
			if tableStart == -1 {
				tableStart = i
			}
			tableEnd = i + 1
			cell := strings.TrimSpace(strings.Split(line, "|")[1])
			if cell == "SVC-"+c.Name || cell == "LIB-"+c.Name {
				return nil, errors.New("компонент уже есть в INDEX.md")
			}
		} else if tableStart != -1 {
			break
		}
	}
	if tableEnd-tableStart < 2 || !strings.Contains(lines[tableStart+1], "---") {
		return nil, errors.New("в INDEX.md отсутствует таблица компонентов")
	}
	lang := c.Lang
	if lang == "" {
		lang = "—"
	}
	row := fmt.Sprintf("| %s-%s | %s | %s | [документация](components/%s/component.md) | %s |", strings.ToUpper(c.Kind), c.Name, c.Kind, lang, c.Name, c.Name)
	updated := append([]string{}, lines[:tableEnd]...)
	updated = append(updated, row)
	updated = append(updated, lines[tableEnd:]...)
	return []byte(strings.Join(updated, "\n")), nil
}

func addComponent(root string, m Manifest, c Component, withAI bool) error {
	if err := c.validate(); err != nil {
		return err
	}
	source, err := safePath(root, m.Docs.Root+"/components/_template")
	if err != nil {
		return err
	}
	dest := m.Docs.Root + "/components/" + c.Name
	path, err := safePath(root, dest)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		return errors.New("каталог компонента уже существует или недоступен")
	}
	files, err := componentFiles(os.DirFS(source), c, withAI)
	if err != nil {
		return err
	}
	indexName := m.Docs.Root + "/INDEX.md"
	indexPath, err := safePath(root, indexName)
	if err != nil {
		return err
	}
	index, err := readLimited(indexPath, maxTemplateBytes)
	if err != nil {
		return err
	}
	index, err = addIndexRow(index, c)
	if err != nil {
		return err
	}
	plan := map[string][]byte{}
	for name, data := range files {
		plan[filepath.ToSlash(filepath.Join(dest, name))] = data
	}
	if err := preflight(root, plan, false); err != nil {
		return err
	}
	for _, name := range sortedKeys(plan) {
		if err := atomicWrite(root, name, plan[name]); err != nil {
			return err
		}
	}
	return atomicWrite(root, indexName, index)
}
