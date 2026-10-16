package wk

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"wk-cli/internal/docscheck"
)

var policyRE = regexp.MustCompile(`(?s)<!-- wikisvc:begin version=([a-f0-9]{8}) -->.*?<!-- wikisvc:end -->`)
var policyVersionRE = regexp.MustCompile(`^[a-f0-9]{8}$`)
var policyLineRE = regexp.MustCompile(`(?m)^- \[(MUST|SHOULD)\] .* → (rule-[a-z0-9]+(?:-[a-z0-9]+)*)\s*$`)
var networkUnavailable = errors.New("сеть недоступна: запрос к вике не выполнен")

type exitError struct {
	code int
	err  error
}

func (e exitError) Error() string { return e.err.Error() }

type compiledPolicies struct {
	Version    string `json:"version"`
	Markdown   string `json:"markdown"`
	OverBudget bool   `json:"over_budget"`
	UsedTokens int    `json:"used_tokens"`
}

func compilePolicies(ctx context.Context, c Client, b PolicyBlock) (compiledPolicies, error) {
	q := url.Values{}
	if b.Profile != "" {
		q.Set("profile", b.Profile)
	}
	if len(b.Scopes) > 0 {
		q.Set("scopes", strings.Join(b.Scopes, ","))
	}
	if b.BudgetTokens != nil {
		q.Set("budget_tokens", strconv.Itoa(*b.BudgetTokens))
	}
	if b.Enforced != "" {
		q.Set("enforced", b.Enforced)
	}
	var compiled compiledPolicies
	if err := c.request(ctx, "GET", "/api/v1/policies/compile?"+q.Encode(), nil, &compiled); err != nil {
		return compiled, err
	}
	if !policyVersionRE.MatchString(compiled.Version) || strings.Contains(compiled.Markdown, "<!-- wikisvc:") {
		return compiled, errors.New("невалидный ответ компиляции политик")
	}
	return compiled, nil
}
func policyVersion(text string) (string, error) {
	matches := policyRE.FindAllStringSubmatch(text, -1)
	if strings.Count(text, "<!-- wikisvc:begin") != len(matches) || len(matches) > 1 {
		return "", errors.New("повреждён или неоднозначен блок политик")
	}
	if len(matches) == 0 {
		return "", nil
	}
	return matches[0][1], nil
}
func exportPolicy(current string, c compiledPolicies) (string, error) {
	// pathlib.read_text uses universal newlines in the server's exporter.
	current = strings.ReplaceAll(strings.ReplaceAll(current, "\r\n", "\n"), "\r", "\n")
	version, err := policyVersion(current)
	if err != nil {
		return "", err
	}
	block := "<!-- wikisvc:begin version=" + c.Version + " -->\n" + strings.TrimRightFunc(c.Markdown, func(r rune) bool { return unicode.IsSpace(r) || r >= 0x1c && r <= 0x1f }) + "\n<!-- wikisvc:end -->"
	if version != "" {
		return policyRE.ReplaceAllStringFunc(current, func(string) string { return block }), nil
	}
	if current != "" {
		current += "\n\n"
	}
	return current + block + "\n", nil
}
func readPolicy(root string, b PolicyBlock) (string, error) {
	path, err := safePath(root, b.File)
	if err != nil {
		return "", err
	}
	data, err := readLimited(path, 8<<20)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return string(data), err
}
func (a App) policies(ctx context.Context, c Client, m Manifest, sync bool) (result, error) {
	var checks []Check
	var lines []string
	code := 0
	for _, b := range m.PolicyBlocks {
		current, err := readPolicy(a.Root, b)
		if err != nil {
			return result{}, err
		}
		old, err := policyVersion(current)
		if err != nil {
			return result{}, err
		}
		compiled, err := compilePolicies(ctx, c, b)
		if err != nil {
			if !sync && errors.Is(err, networkUnavailable) {
				return result{}, exitError{3, errors.New("сеть недоступна: проверка не выполнена")}
			}
			return result{}, err
		}
		check := Check{b.File, "ok", "политики актуальны"}
		if sync {
			updated, _ := exportPolicy(current, compiled) // current was validated above
			message := b.File + ": без изменений"
			if updated != current {
				if a.Token != "" && strings.Contains(updated, a.Token) {
					return result{}, errors.New("credential не может быть записан на диск")
				}
				if err := a.write(a.Root, b.File, []byte(updated)); err != nil {
					return result{}, err
				}
				if old == "" {
					old = "нет блока"
				}
				message = fmt.Sprintf("%s: %s → %s", b.File, old, compiled.Version)
			}
			lines = append(lines, message)
			check.Message = message
		} else {
			if old == "" {
				check.Status = "fail"
				check.Message = "блока политик нет: выполни `wk policy sync`"
			} else if old != compiled.Version {
				check.Status = "fail"
				check.Message = "политики изменились, выполни `wk policy sync`"
			} else if expected, _ := exportPolicy("", compiled); policyRE.FindString(strings.ReplaceAll(strings.ReplaceAll(current, "\r\n", "\n"), "\r", "\n"))+"\n" != expected {
				// The version marker alone can be kept while the text inside is rewritten.
				check.Status = "fail"
				check.Message = "блок политик изменён вручную, выполни `wk policy sync`"
			}
			if check.Status == "fail" {
				code = 1
			}
			lines = append(lines, fmt.Sprintf("%s %s: %s", check.Status, b.File, check.Message))
		}
		checks = append(checks, check)
		if compiled.OverBudget {
			lines = append(lines, fmt.Sprintf("ВНИМАНИЕ %s: превышен бюджет политик, токенов: %d", b.File, compiled.UsedTokens))
		}
	}
	return result{Data: map[string]any{"checks": checks, "exit_code": code}, Text: strings.Join(lines, "\n"), Code: code}, nil
}
func localPolicies(root string, m Manifest) ([]docscheck.Policy, error) {
	var policies []docscheck.Policy
	for _, b := range m.PolicyBlocks {
		current, err := readPolicy(root, b)
		if err != nil {
			return nil, err
		}
		if _, err := policyVersion(current); err != nil {
			return nil, err
		}
		for _, line := range policyLineRE.FindAllStringSubmatch(policyRE.FindString(current), -1) {
			policies = append(policies, docscheck.Policy{ID: line[2], Level: strings.ToLower(line[1]), Profile: b.Profile})
		}
	}
	return policies, nil
}
