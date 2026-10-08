package wk

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func (a App) propose(ctx context.Context, c Client, args []string) (result, error) {
	f := flags("propose")
	file := f.String("file", "", "page file")
	title := f.String("title", "", "proposal title")
	kind := f.String("kind", "manual", "proposal kind")
	if err := parseFlags(f, args); err != nil {
		return result{}, err
	}
	if f.NArg() != 0 || *file == "" || *kind != "manual" {
		return result{}, errors.New("propose требует --file; поддерживается --kind manual")
	}
	path := *file
	if !filepath.IsAbs(path) {
		path = filepath.Join(a.Root, path)
	}
	data, err := readLimited(path, maxFileBytes)
	if err != nil {
		return result{}, err
	}
	id, pageTitle, err := pageMetadata(data)
	if err != nil {
		return result{}, err
	}
	if *title == "" {
		*title = pageTitle
	}
	if *title == "" {
		*title = id
	}
	var created struct {
		PID string `json:"pid"`
	}
	if err := c.request(ctx, "POST", "/api/v1/proposals", map[string]string{"title": *title, "kind": *kind}, &created); err != nil {
		return result{}, err
	}
	if !projectID.MatchString(created.PID) {
		return result{}, errors.New("вика вернула невалидный pid")
	}
	prefix := "/api/v1/proposals/" + created.PID
	// The existing server accepts a JSON string containing the complete Markdown.
	if err := c.request(ctx, "PUT", prefix+"/pages/"+id, string(data), nil); err != nil {
		return result{}, fmt.Errorf("предложение %s осталось черновиком: %w", created.PID, err)
	}
	var validation struct {
		Errors   []any `json:"errors"`
		Warnings []any `json:"warnings"`
	}
	if err := c.request(ctx, "GET", prefix+"/validate", nil, &validation); err != nil {
		return result{}, fmt.Errorf("предложение %s осталось черновиком: %w", created.PID, err)
	}
	if validation.Errors == nil {
		return result{}, fmt.Errorf("предложение %s осталось черновиком: невалидный ответ validate", created.PID)
	}
	if len(validation.Errors) > 0 {
		return result{Data: map[string]any{"pid": created.PID, "submitted": false, "errors": validation.Errors, "warnings": validation.Warnings}, Text: "Предложение " + created.PID + " не отправлено:\n" + pretty(validation.Errors), Code: 1}, nil
	}
	if err := c.request(ctx, "POST", prefix+"/submit", nil, nil); err != nil {
		return result{}, fmt.Errorf("предложение %s не отправлено: %w", created.PID, err)
	}
	link := c.Origin + "/proposals/" + created.PID
	return result{Data: map[string]any{"pid": created.PID, "url": link, "submitted": true, "warnings": validation.Warnings}, Text: created.PID + " " + link}, nil
}

func pageMetadata(data []byte) (string, string, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return "", "", errors.New("страница должна содержать YAML frontmatter с id")
	}
	end := strings.Index(text[4:], "\n---\n")
	if end < 0 {
		return "", "", errors.New("frontmatter не закрыт")
	}
	var node yaml.Node
	if yaml.Unmarshal([]byte(text[4:4+end]), &node) != nil || len(node.Content) != 1 || node.Content[0].Kind != yaml.MappingNode {
		return "", "", errors.New("невалидный frontmatter")
	}
	if err := validateNode(&node, false); err != nil {
		return "", "", err
	}
	var fields struct {
		ID    string `yaml:"id"`
		Title string `yaml:"title"`
	}
	if node.Decode(&fields) != nil || !projectID.MatchString(fields.ID) {
		return "", "", errors.New("невалидный id страницы")
	}
	return fields.ID, fields.Title, nil
}
