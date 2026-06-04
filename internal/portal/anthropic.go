package portal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type AnthropicClient struct {
	BaseURL    string
	HTTPClient *http.Client
	Auth       AuthStore
}

func (c AnthropicClient) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func (c AnthropicClient) messagesURL() (string, error) {
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = DefaultAnthropicURL
	}
	u, err := url.Parse(base + "/v1/messages")
	if err != nil {
		return "", err
	}
	q := u.Query()
	if q.Get("beta") == "" {
		q.Set("beta", "true")
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (c AnthropicClient) DoMessages(ctx context.Context, body map[string]any, stream bool) (*http.Response, error) {
	return c.doMessages(ctx, body, stream, false)
}

func (c AnthropicClient) doMessages(ctx context.Context, body map[string]any, stream bool, refreshed bool) (*http.Response, error) {
	token, err := c.Auth.AccessToken(ctx)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	endpoint, err := c.messagesURL()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Anthropic-Version", AnthropicVersion)
	req.Header.Set("Anthropic-Beta", AnthropicBetas)
	req.Header.Set("User-Agent", AnthropicUserAgent)
	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	res, err := c.client().Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode == http.StatusUnauthorized {
		if refreshed {
			return res, nil
		}
		_ = res.Body.Close()
		if _, err := c.Auth.Refresh(ctx); err != nil {
			return nil, err
		}
		return c.doMessages(ctx, body, stream, true)
	}
	return res, nil
}

func modelDeniedError(status int, model string) string {
	switch status {
	case http.StatusForbidden:
		return fmt.Sprintf("Anthropic denied access to model %q for this OAuth account (HTTP 403). Try another /v1/models entry or check the subscription account.", model)
	case http.StatusNotFound:
		return fmt.Sprintf("Anthropic model %q was not found or is unavailable to this OAuth account (HTTP 404). Run `codex-portal models refresh` or choose another model.", model)
	default:
		return fmt.Sprintf("Anthropic request failed for model %q (HTTP %d).", model, status)
	}
}
