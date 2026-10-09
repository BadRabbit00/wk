package wk

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const manifestFile = ".wiki-project.yaml"

var projectID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
var componentID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

type Wiki struct {
	URL     string   `yaml:"url" json:"url"`
	Profile string   `yaml:"profile" json:"profile"`
	Scopes  []string `yaml:"scopes" json:"scopes"`
}

type PolicyBlock struct {
	File         string   `yaml:"file" json:"file"`
	Profile      string   `yaml:"profile" json:"profile"`
	Scopes       []string `yaml:"scopes,omitempty" json:"scopes,omitempty"`
	BudgetTokens *int     `yaml:"budget_tokens,omitempty" json:"budget_tokens,omitempty"`
	Enforced     string   `yaml:"enforced,omitempty" json:"enforced,omitempty"`
}

type TemplateRef struct {
	ID     string `yaml:"id" json:"id"`
	Digest string `yaml:"digest" json:"digest"`
}

type Docs struct {
	Root string `yaml:"root" json:"root"`
}

type Manifest struct {
	Version      int           `yaml:"version" json:"version"`
	Project      string        `yaml:"project" json:"project"`
	Wiki         Wiki          `yaml:"wiki" json:"wiki"`
	PolicyBlocks []PolicyBlock `yaml:"policy_blocks" json:"policy_blocks"`
	Template     TemplateRef   `yaml:"template" json:"template"`
	Docs         Docs          `yaml:"docs" json:"docs"`
}

// decodeYAML rejects ambiguous documents before typed decoding. Aliases and merge
// keys are deliberately unsupported in configuration received from a server.
func decodeYAML(data []byte, out any, forbidSecrets bool) error {
	var node yaml.Node
	d := yaml.NewDecoder(bytes.NewReader(data))
	if err := d.Decode(&node); err != nil {
		return errors.New("невалидный YAML")
	}
	var extra yaml.Node
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("ожидается один YAML-документ")
	}
	if len(node.Content) != 1 || node.Content[0].Kind != yaml.MappingNode {
		return errors.New("ожидается YAML-объект")
	}
	if err := validateNode(&node, forbidSecrets); err != nil {
		return err
	}
	d = yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if err := d.Decode(out); err != nil {
		return errors.New("неизвестное поле или неверный тип YAML")
	}
	return nil
}

func validateNode(n *yaml.Node, forbidSecrets bool) error {
	if n.Kind == yaml.AliasNode {
		return errors.New("YAML aliases запрещены")
	}
	if n.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for i := 0; i < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Kind != yaml.ScalarNode || k.Tag != "!!str" || seen[k.Value] {
				return errors.New("невалидный или повторяющийся ключ YAML")
			}
			seen[k.Value] = true
			lower := strings.ToLower(k.Value)
			if forbidSecrets && lower != "budget_tokens" && (strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "password")) {
				return errors.New("секреты запрещены в манифесте")
			}
		}
	}
	for _, child := range n.Content {
		if err := validateNode(child, forbidSecrets); err != nil {
			return err
		}
	}
	return nil
}

func validOrigin(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil && u.Path == "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && !strings.ContainsAny(raw, "\r\n\t #") && u.Opaque == ""
}

func relativePath(path string) error {
	if path == "" || filepath.IsAbs(path) || strings.ContainsAny(path, "\\:\x00\r\n") {
		return errors.New("ожидается относительный путь внутри проекта")
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." || strings.EqualFold(part, ".git") {
			return errors.New("небезопасный путь внутри проекта")
		}
	}
	return nil
}

// safePath resolves the project root and checks only components inside it.
// Writes repeat this check immediately before accessing the destination.
func safePath(root, path string) (string, error) {
	full, _, err := inspectPath(root, path)
	return full, err
}

func inspectPath(root, path string) (string, fs.FileInfo, error) {
	if err := relativePath(path); err != nil {
		return "", nil, err
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", nil, err
	}
	// Ancestors belong to the user, not the project (e.g. /var on macOS).
	// A not-yet-created init root cannot be resolved; inspect its children below.
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	full := filepath.Join(abs, filepath.FromSlash(path))
	current := abs
	var last fs.FileInfo
	for _, part := range strings.Split(path, "/") {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", nil, errors.New("симлинки запрещены")
		}
		if current == full {
			last = info
		}
	}
	return full, last, nil
}

func (m Manifest) validate(root string) error {
	if m.Version != 1 {
		return errors.New("неподдерживаемая версия манифеста: обнови wk")
	}
	if !projectID.MatchString(m.Project) {
		return errors.New("невалидное имя проекта")
	}
	if !validOrigin(m.Wiki.URL) {
		return errors.New("wiki.url должен быть http/https origin без пути, query и учётных данных")
	}
	if !projectID.MatchString(m.Wiki.Profile) {
		return errors.New("wiki.profile обязателен и должен быть ID профиля")
	}
	for _, scope := range m.Wiki.Scopes {
		if strings.TrimSpace(scope) == "" {
			return errors.New("пустой scope")
		}
	}
	seen := map[string]bool{}
	for _, block := range m.PolicyBlocks {
		if block.File == manifestFile {
			return errors.New("блок политик не может заменять манифест")
		}
		if _, err := safePath(root, block.File); err != nil {
			return err
		}
		if seen[block.File] || !projectID.MatchString(block.Profile) {
			return errors.New("невалидный или повторяющийся policy_blocks")
		}
		seen[block.File] = true
		if block.BudgetTokens != nil && (*block.BudgetTokens < 1 || *block.BudgetTokens > 100000) {
			return errors.New("невалидный budget_tokens")
		}
		if block.Enforced != "" && block.Enforced != "keep_must" && block.Enforced != "all" && block.Enforced != "none" {
			return errors.New("невалидный enforced")
		}
		for _, scope := range block.Scopes {
			if strings.TrimSpace(scope) == "" || strings.Contains(scope, ",") {
				return errors.New("невалидный scope политики")
			}
		}
	}
	if _, err := safePath(root, m.Docs.Root); err != nil {
		return err
	}
	if m.Template.ID != "" && !projectID.MatchString(m.Template.ID) {
		return errors.New("невалидный template.id")
	}
	if m.Template.Digest != "" && (!regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(m.Template.Digest) || m.Template.ID == "") {
		return errors.New("невалидный template.digest")
	}
	return nil
}

func loadManifest(root string) (Manifest, error) {
	var m Manifest
	path, err := safePath(root, manifestFile)
	if err != nil {
		return m, err
	}
	data, err := readLimited(path, 256<<10)
	if err != nil {
		return m, fmt.Errorf("манифест: %w", err)
	}
	if err = decodeYAML(data, &m, true); err != nil {
		return m, err
	}
	return m, m.validate(root)
}

func readLimited(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("превышен размер файла")
	}
	return data, nil
}
