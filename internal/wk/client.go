package wk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type apiError struct{ Status int }

func (e apiError) Error() string { return fmt.Sprintf("вика вернула HTTP %d", e.Status) }

type Client struct {
	Origin string
	Token  string
	HTTP   *http.Client
}

func (c Client) request(ctx context.Context, method, path string, payload any, out any) error {
	var body []byte
	if payload != nil {
		var err error
		body, err = json.Marshal(payload)
		if err != nil {
			return errors.New("не удалось кодировать запрос")
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Origin+path, bytes.NewReader(body))
	if err != nil {
		return errors.New("не удалось создать запрос")
	}
	req.Header.Set("Accept", "application/json, text/event-stream")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	h := http.Client{Timeout: 10 * time.Second}
	if c.HTTP != nil {
		h = *c.HTTP
	}
	h.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	response, err := h.Do(req)
	if err != nil {
		return networkUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return apiError{response.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 4*maxTemplateBytes+1))
	if err != nil {
		return errors.New("не удалось прочитать ответ вики")
	}
	if len(data) > 4*maxTemplateBytes {
		return errors.New("превышен размер ответа вики")
	}
	if strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		var event []string
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "data:") {
				event = append(event, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			}
		}
		data = []byte(strings.Join(event, "\n"))
	}
	if raw, ok := out.(*[]byte); ok {
		*raw = data
		return nil
	}
	if out != nil && json.Unmarshal(data, out) != nil {
		return errors.New("невалидный JSON в ответе вики")
	}
	return nil
}
