package wk

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const maxFileBytes = 256 << 10
const maxTemplateBytes = 2 << 20

type TemplateFile struct {
	Path    string `yaml:"path" json:"path"`
	Content string `yaml:"content" json:"content"`
}

type Component struct {
	Name string `yaml:"name" json:"name"`
	Kind string `yaml:"kind" json:"kind"`
	Lang string `yaml:"lang" json:"lang"`
}

type ProjectTemplate struct {
	ID           string         `yaml:"id" json:"id"`
	Title        string         `yaml:"title" json:"title"`
	Profile      string         `yaml:"profile" json:"profile"`
	Scopes       []string       `yaml:"scopes" json:"scopes"`
	Components   []Component    `yaml:"components" json:"components"`
	Files        []TemplateFile `yaml:"files" json:"files"`
	PolicyBlocks []PolicyBlock  `yaml:"policy_blocks" json:"policy_blocks"`
}

func parseTemplate(data []byte) (ProjectTemplate, error) {
	var t ProjectTemplate
	if len(data) > 4*maxTemplateBytes {
		return t, errors.New("превышен размер ответа шаблона")
	}
	if err := decodeYAML(data, &t, false); err != nil {
		return t, err
	}
	if !projectID.MatchString(t.ID) || !projectID.MatchString(t.Profile) || t.Title == "" {
		return t, errors.New("невалидные id, title или profile шаблона")
	}
	if len(t.Files) > 200 {
		return t, errors.New("шаблон содержит больше 200 файлов")
	}
	total := 0
	seen := map[string]bool{}
	for _, f := range t.Files {
		if err := relativePath(f.Path); err != nil {
			return t, err
		}
		if f.Path == manifestFile || seen[f.Path] {
			return t, errors.New("шаблон содержит зарезервированный или повторяющийся путь")
		}
		seen[f.Path] = true
		total += len(f.Content)
		if len(f.Content) > maxFileBytes || total > maxTemplateBytes {
			return t, errors.New("превышены лимиты файлов шаблона")
		}
	}
	for path := range seen {
		for parent := filepath.Dir(path); parent != "."; parent = filepath.Dir(parent) {
			if seen[parent] {
				return t, errors.New("конфликт файла и каталога шаблона")
			}
		}
	}
	seen = map[string]bool{}
	for _, c := range t.Components {
		if err := c.validate(); err != nil {
			return t, err
		}
		if seen[c.Name] {
			return t, errors.New("повторяющийся компонент шаблона")
		}
		seen[c.Name] = true
	}
	seen = map[string]bool{}
	for _, b := range t.PolicyBlocks {
		if err := relativePath(b.File); err != nil {
			return t, err
		}
		if !projectID.MatchString(b.Profile) || seen[b.File] {
			return t, errors.New("невалидные блоки политик шаблона")
		}
		seen[b.File] = true
	}
	return t, nil
}

func (c Component) validate() error {
	if !componentID.MatchString(c.Name) {
		return errors.New("невалидное имя компонента")
	}
	if c.Kind != "svc" && c.Kind != "lib" {
		return errors.New("kind должен быть svc или lib")
	}
	if c.Lang != "" && !langID.MatchString(c.Lang) {
		return errors.New("язык должен быть LANG-id")
	}
	return nil
}

// Typed YAML encoding gives a stable field order, regardless of input key order.
func (t ProjectTemplate) canonical() ([]byte, string) {
	data, _ := yaml.Marshal(t) // only strings, slices and structs: encoding cannot fail
	hash := sha256.Sum256(data)
	return data, "sha256:" + hex.EncodeToString(hash[:])
}

func preflight(root string, files map[string][]byte, force bool) error {
	var conflicts []string
	for _, name := range sortedKeys(files) {
		for parent := filepath.Dir(name); parent != "."; parent = filepath.Dir(parent) {
			if _, ok := files[parent]; ok {
				return errors.New("конфликт файла и каталога в плане записи")
			}
		}
		_, info, err := inspectPath(root, name)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if info != nil {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("%s: целевой путь не обычный файл", name)
			}
			if !force {
				conflicts = append(conflicts, name)
			}
		}
	}
	if len(conflicts) > 0 {
		return fmt.Errorf("конфликты файлов (нужен --force): %s", strings.Join(conflicts, ", "))
	}
	return nil
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func atomicWrite(root, name string, data []byte) error {
	_, err := safePath(root, name)
	if err != nil {
		return err
	}
	// OpenRoot confines file operations even if a directory is concurrently moved.
	r, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer r.Close()
	return writeInRoot(root, rootDirectory{r}, name, data)
}

// This small interface lets storage-failure tests exercise cleanup and commit
// ordering without relying on a full disk or a filesystem race.
type writableRoot interface {
	MkdirAll(string, os.FileMode) error
	OpenFile(string, int, os.FileMode) (io.WriteCloser, error)
	Remove(string) error
	Rename(string, string) error
}

type rootDirectory struct{ *os.Root }

func (r rootDirectory) OpenFile(name string, flags int, mode os.FileMode) (io.WriteCloser, error) {
	return r.Root.OpenFile(name, flags, mode)
}

func writeInRoot(root string, r writableRoot, name string, data []byte) error {
	if err := r.MkdirAll(filepath.Dir(name), 0755); err != nil {
		return err
	}
	if _, err := safePath(root, name); err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(name), ".wk-"+rand.Text())
	f, err := r.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	defer r.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if _, err := safePath(root, name); err != nil {
		return err
	}
	return r.Rename(tmp, name)
}
