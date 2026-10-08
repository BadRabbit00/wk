package wk

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

var githubTemplate = regexp.MustCompile(`^github:([A-Za-z0-9][A-Za-z0-9-]*)/([A-Za-z0-9][A-Za-z0-9_.-]*)#([a-z0-9][a-z0-9-]{0,63})$`)

func (a App) templates(ctx context.Context, c Client, args []string) (result, error) {
	if len(args) == 1 && args[0] == "list" {
		var schema struct {
			Templates map[string]ProjectTemplate `json:"project_templates"`
		}
		if err := c.request(ctx, "GET", "/api/v1/schema", nil, &schema); err != nil {
			return result{}, err
		}
		items := make([]ProjectTemplate, 0, len(schema.Templates))
		var lines []string
		for _, id := range sortedKeys(schema.Templates) {
			v := schema.Templates[id]
			items = append(items, v)
			lines = append(lines, fmt.Sprintf("%s — %s (profile=%s)", id, v.Title, v.Profile))
		}
		if len(lines) == 0 {
			lines = append(lines, "Шаблонов нет")
		}
		return result{Data: items, Text: strings.Join(lines, "\n")}, nil
	}
	if len(args) == 2 && args[0] == "show" {
		if !projectID.MatchString(args[1]) {
			return result{}, errors.New("ожидается ID шаблона из вики")
		}
		t, err := a.fetchTemplate(ctx, c, args[1], false, "")
		if err != nil {
			return result{}, err
		}
		data, digest := t.canonical()
		return result{Data: map[string]any{"template": t, "digest": digest}, Text: string(data) + "digest: " + digest}, nil
	}
	return result{}, errors.New("используй template list или template show ID")
}

func (a App) fetchTemplate(ctx context.Context, c Client, source string, offline bool, pinned string) (ProjectTemplate, error) {
	var empty ProjectTemplate
	id := source
	match := githubTemplate.FindStringSubmatch(source)
	if strings.HasPrefix(source, "github:") && match == nil {
		return empty, errors.New("ожидается github:owner/repo#id")
	}
	if match != nil {
		id = match[3]
	}
	if !projectID.MatchString(id) {
		return empty, errors.New("невалидный ID шаблона")
	}
	if offline {
		return a.cachedTemplate(id, pinned)
	}
	path := "/api/v1/schema/project-templates/" + id
	if match != nil {
		// WIKI_TOKEN belongs to the wiki origin, never to GitHub.
		c = Client{Origin: "https://raw.githubusercontent.com", HTTP: a.HTTP}
		path = "/" + match[1] + "/" + match[2] + "/HEAD/schema/project-templates/" + id + ".yaml"
	}
	var data []byte
	if err := c.request(ctx, "GET", path, nil, &data); err != nil {
		return empty, err
	}
	if a.Token != "" && strings.Contains(string(data), a.Token) {
		return empty, errors.New("ответ шаблона содержит credential; сохранение запрещено")
	}
	t, err := parseTemplate(data)
	if err != nil {
		return empty, err
	}
	if t.ID != id {
		return empty, errors.New("ID полученного шаблона не совпадает с запросом")
	}
	canonical, digest := t.canonical()
	if redact(pretty(t), a.Token) != pretty(t) {
		return empty, errors.New("шаблон содержит credential; сохранение запрещено")
	}
	if pinned != "" && pinned != digest {
		return empty, errors.New("digest шаблона не совпадает с манифестом")
	}
	if err := os.MkdirAll(a.Cache, 0700); err != nil {
		return empty, fmt.Errorf("кэш шаблонов: %w", err)
	}
	if err := atomicWrite(a.Cache, id+"-"+strings.TrimPrefix(digest, "sha256:")+".yaml", canonical); err != nil {
		return empty, err
	}
	return t, nil
}

func (a App) cachedTemplate(id, pinned string) (ProjectTemplate, error) {
	var empty ProjectTemplate
	readDir := a.readCache
	if readDir == nil {
		readDir = os.ReadDir
	}
	entries, err := readDir(a.Cache)
	if err != nil {
		return empty, errors.New("локальный кэш шаблонов недоступен")
	}
	type candidate struct {
		name     string
		modified time.Time
	}
	var candidates []candidate
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), id+"-") || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		if pinned != "" && entry.Name() != id+"-"+strings.TrimPrefix(pinned, "sha256:")+".yaml" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return empty, err
		}
		candidates = append(candidates, candidate{entry.Name(), info.ModTime()})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].modified.Equal(candidates[j].modified) {
			return candidates[i].name < candidates[j].name
		}
		return candidates[i].modified.After(candidates[j].modified)
	})
	if len(candidates) == 0 {
		return empty, errors.New("шаблон отсутствует в локальном кэше")
	}
	name := candidates[0].name
	path, err := safePath(a.Cache, name)
	if err != nil {
		return empty, err
	}
	data, err := readLimited(path, 4*maxTemplateBytes)
	if err != nil {
		return empty, err
	}
	if a.Token != "" && strings.Contains(string(data), a.Token) {
		return empty, errors.New("кэш содержит credential")
	}
	t, err := parseTemplate(data)
	if err != nil {
		return empty, err
	}
	_, digest := t.canonical()
	if t.ID != id || name != id+"-"+strings.TrimPrefix(digest, "sha256:")+".yaml" {
		return empty, errors.New("повреждён кэш шаблона: digest не совпадает")
	}
	return t, nil
}
