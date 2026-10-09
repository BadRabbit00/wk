package wk

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
)

type Check struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

func checksResult(checks []Check, failUnavailable bool) result {
	code := 0
	var lines []string
	for _, check := range checks {
		lines = append(lines, fmt.Sprintf("%s %s: %s", check.Status, check.Name, check.Message))
		if check.Status == "fail" || (failUnavailable && check.Status == "warn") {
			code = 1
		}
	}
	return result{Data: map[string]any{"checks": checks, "exit_code": code}, Text: strings.Join(lines, "\n"), Code: code}
}

func (a App) doctor(ctx context.Context) result {
	m, err := loadManifest(a.Root)
	if err != nil {
		return checksResult([]Check{{"manifest", "fail", err.Error()}}, false)
	}
	checks := []Check{{"manifest", "ok", "манифест валиден"}}
	c := Client{Origin: m.Wiki.URL, Token: a.Token, HTTP: a.HTTP}
	add := func(name string, err error, message string) {
		if err != nil {
			checks = append(checks, Check{name, "fail", err.Error()})
		} else {
			checks = append(checks, Check{name, "ok", message})
		}
	}
	var health struct {
		Status string `json:"status"`
	}
	err = c.request(ctx, "GET", "/api/v1/health", nil, &health)
	if err == nil && health.Status != "ok" {
		err = errors.New("status не равен ok")
	}
	add("wiki", err, "вика доступна")
	var who struct {
		Name      string `json:"name"`
		Role      string `json:"role"`
		Kind      string `json:"kind"`
		Clearance string `json:"clearance"`
	}
	err = c.request(ctx, "GET", "/api/v1/whoami", nil, &who)
	var status apiError
	if errors.As(err, &status) && status.Status == 401 {
		err = errors.New("анонимный доступ выключен на сервере")
	}
	if err == nil && (who.Name == "" || !slices.Contains([]string{"reader", "writer", "reviewer", "admin"}, who.Role) || !slices.Contains([]string{"human", "agent"}, who.Kind) || !slices.Contains([]string{"public", "internal", "restricted"}, who.Clearance)) {
		err = errors.New("невалидный ответ whoami")
	}
	add("whoami", err, fmt.Sprintf("name=%s role=%s kind=%s clearance=%s", who.Name, who.Role, who.Kind, who.Clearance))
	if err == nil && who.Role == "reader" {
		checks[len(checks)-1].Status = "warn"
		checks[len(checks)-1].Message += "; предложения создавать нельзя"
	}
	var profiles []struct {
		ID string `json:"id"`
	}
	err = c.request(ctx, "GET", "/api/v1/profiles", nil, &profiles)
	if err == nil && !slices.ContainsFunc(profiles, func(p struct {
		ID string `json:"id"`
	}) bool {
		return p.ID == m.Wiki.Profile
	}) {
		err = errors.New("профиль отсутствует: " + m.Wiki.Profile)
	}
	add("profile", err, m.Wiki.Profile)
	var scopes []string
	err = c.request(ctx, "GET", "/api/v1/scopes", nil, &scopes)
	if err == nil {
		var missing []string
		for _, scope := range m.Wiki.Scopes {
			if !slices.Contains(scopes, scope) {
				missing = append(missing, scope)
			}
		}
		if len(missing) > 0 {
			err = errors.New("отсутствуют scopes: " + strings.Join(missing, ", "))
		}
	}
	add("scopes", err, "все scopes существуют")
	pol, policyErr := a.policies(ctx, c, m, false)
	if policyErr == nil && pol.Code != 0 {
		policyErr = errors.New(strings.TrimSpace(pol.Text))
	}
	add("policies", policyErr, "политики актуальны")
	var rpc struct {
		Result *struct {
			Tools []jsonTool `json:"tools"`
		} `json:"result"`
		Error any `json:"error"`
	}
	err = c.request(ctx, "POST", "/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": map[string]any{}}, &rpc)
	if err == nil && (rpc.Error != nil || rpc.Result == nil || rpc.Result.Tools == nil) {
		err = errors.New("невалидный ответ MCP tools/list")
	}
	add("mcp", err, "MCP доступен")
	if errors.As(err, &status) && status.Status == 404 {
		checks[len(checks)-1] = Check{"mcp", "warn", "MCP не включён (404)"}
	}
	err = c.request(ctx, "GET", "/agent-api/healthz", nil, &health)
	if err == nil && health.Status != "ok" {
		err = errors.New("агент не готов")
	}
	add("agent", err, "агент доступен")
	if err != nil {
		checks[len(checks)-1].Status = "warn"
	}
	rep, docsErr := a.docsReport(m, m.Docs.Root, ".", false)
	if docsErr == nil && rep.Code(false) != 0 {
		docsErr = errors.New(strings.TrimSpace(rep.Text()))
	}
	add("docs", docsErr, strings.TrimSpace(rep.Text()))
	if docsErr == nil && len(rep.Warnings) > 0 {
		checks[len(checks)-1].Status = "warn"
	}
	add("skills", checkSkills(a.Root), "четыре навыка валидны для Claude Code и Codex")
	output, err := git(ctx, a.Root, "status", "--porcelain")
	check := Check{"git", "ok", "рабочее дерево чистое"}
	if err != nil {
		check = Check{"git", "warn", "git status недоступен"}
	} else if strings.TrimSpace(output) != "" {
		check = Check{"git", "warn", "есть незакоммиченные изменения"}
	}
	checks = append(checks, check)
	return checksResult(checks, false)
}

type jsonTool struct {
	Name string `json:"name"`
}

func git(ctx context.Context, root string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = root
	for _, entry := range os.Environ() {
		key := strings.SplitN(entry, "=", 2)[0]
		if key != "WIKI_TOKEN" && !strings.HasPrefix(key, "GIT_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0")
	output, err := cmd.Output()
	if err != nil {
		return "", errors.New("команда git не выполнена")
	}
	return string(output), nil
}
