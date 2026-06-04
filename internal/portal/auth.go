package portal

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	OAuthClientID      = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	AuthorizeURL       = "https://claude.ai/oauth/authorize"
	CodeCallbackURL    = "https://platform.claude.com/oauth/code/callback"
	TokenURL           = "https://platform.claude.com/v1/oauth/token"
	OAuthRiskNotice    = "Codex Portal uses unofficial Anthropic OAuth subscription access. This may violate service terms or create account risk. Continue only if you accept that risk."
	AnthropicVersion   = "2023-06-01"
	AnthropicBetas     = "oauth-2025-04-20,interleaved-thinking-2025-05-14"
	AnthropicUserAgent = "claude-cli/2.1.87 (external, cli)"
)

var OAuthScopes = []string{
	"org:create_api_key",
	"user:profile",
	"user:inference",
	"user:sessions:claude_code",
	"user:mcp_servers",
	"user:file_upload",
}

type TokenSet struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type OAuthStart struct {
	URL         string `json:"url"`
	RedirectURI string `json:"redirect_uri"`
	State       string `json:"state"`
	Verifier    string `json:"verifier"`
}

type AuthStore struct {
	Home       string
	HTTPClient *http.Client
	TokenURL   string
	Now        func() time.Time
}

func (s AuthStore) client() *http.Client {
	if s.HTTPClient != nil {
		return s.HTTPClient
	}
	return http.DefaultClient
}

func (s AuthStore) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s AuthStore) tokenURL() string {
	if s.TokenURL != "" {
		return s.TokenURL
	}
	return TokenURL
}

func NewOAuthStart() (OAuthStart, error) {
	verifier, challenge, err := GeneratePKCE()
	if err != nil {
		return OAuthStart{}, err
	}
	state, err := randomBase64URL(32)
	if err != nil {
		return OAuthStart{}, err
	}
	u, err := url.Parse(AuthorizeURL)
	if err != nil {
		return OAuthStart{}, err
	}
	q := u.Query()
	q.Set("code", "true")
	q.Set("client_id", OAuthClientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", CodeCallbackURL)
	q.Set("scope", strings.Join(OAuthScopes, " "))
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	u.RawQuery = q.Encode()
	return OAuthStart{
		URL:         u.String(),
		RedirectURI: CodeCallbackURL,
		State:       state,
		Verifier:    verifier,
	}, nil
}

func GeneratePKCE() (verifier string, challenge string, err error) {
	verifier, err = randomBase64URL(32)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

func randomBase64URL(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func parseCallbackInput(input string) (code string, state string, ok bool) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", "", false
	}
	if u, err := url.Parse(trimmed); err == nil && u.Scheme != "" {
		code = u.Query().Get("code")
		state = u.Query().Get("state")
		return code, state, code != "" && state != ""
	}
	parts := strings.Split(trimmed, "#")
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		return parts[0], parts[1], true
	}
	q, err := url.ParseQuery(trimmed)
	if err == nil {
		code = q.Get("code")
		state = q.Get("state")
		return code, state, code != "" && state != ""
	}
	return "", "", false
}

func (s AuthStore) Exchange(ctx context.Context, callbackInput string, start OAuthStart) (TokenSet, error) {
	code, state, ok := parseCallbackInput(callbackInput)
	if !ok {
		return TokenSet{}, errors.New("callback must include code and state")
	}
	if start.State != "" && state != start.State {
		return TokenSet{}, errors.New("OAuth state mismatch")
	}
	body := map[string]any{
		"code":          code,
		"state":         state,
		"grant_type":    "authorization_code",
		"client_id":     OAuthClientID,
		"redirect_uri":  start.RedirectURI,
		"code_verifier": start.Verifier,
	}
	tok, err := s.tokenRequest(ctx, body)
	if err != nil {
		return TokenSet{}, err
	}
	if err := s.Save(tok); err != nil {
		return TokenSet{}, err
	}
	return tok, nil
}

func (s AuthStore) Refresh(ctx context.Context) (TokenSet, error) {
	current, err := s.Load()
	if err != nil {
		return TokenSet{}, err
	}
	if current.RefreshToken == "" {
		return TokenSet{}, errors.New("auth file has no refresh token")
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(250*(1<<uint(attempt-1))) * time.Millisecond)
		}
		fresh, err := s.Load()
		if err != nil {
			return TokenSet{}, err
		}
		tok, err := s.tokenRequest(ctx, map[string]any{
			"grant_type":    "refresh_token",
			"refresh_token": fresh.RefreshToken,
			"client_id":     OAuthClientID,
		})
		if err == nil {
			if err := s.Save(tok); err != nil {
				return TokenSet{}, err
			}
			return tok, nil
		}
		lastErr = err
		var httpErr upstreamHTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode < 500 {
			break
		}
	}
	return TokenSet{}, lastErr
}

func (s AuthStore) AccessToken(ctx context.Context) (string, error) {
	tok, err := s.Load()
	if err != nil {
		return "", err
	}
	if tok.AccessToken != "" && tok.ExpiresAt.After(s.now().Add(2*time.Minute)) {
		return tok.AccessToken, nil
	}
	tok, err = s.Refresh(ctx)
	if err != nil {
		return "", err
	}
	return tok.AccessToken, nil
}

func (s AuthStore) tokenRequest(ctx context.Context, payload map[string]any) (TokenSet, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return TokenSet{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.tokenURL(), bytes.NewReader(data))
	if err != nil {
		return TokenSet{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("User-Agent", "axios/1.13.6")
	res, err := s.client().Do(req)
	if err != nil {
		return TokenSet{}, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return TokenSet{}, upstreamHTTPError{StatusCode: res.StatusCode, Message: strings.TrimSpace(string(msg))}
	}
	var decoded struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(res.Body).Decode(&decoded); err != nil {
		return TokenSet{}, err
	}
	if decoded.AccessToken == "" || decoded.RefreshToken == "" || decoded.ExpiresIn <= 0 {
		return TokenSet{}, errors.New("OAuth token response missing access token, refresh token, or expiry")
	}
	now := s.now().UTC()
	return TokenSet{
		AccessToken:  decoded.AccessToken,
		RefreshToken: decoded.RefreshToken,
		ExpiresAt:    now.Add(time.Duration(decoded.ExpiresIn) * time.Second),
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

func (s AuthStore) Load() (TokenSet, error) {
	data, err := os.ReadFile(authPath(s.Home))
	if err != nil {
		return TokenSet{}, err
	}
	var tok TokenSet
	if err := json.Unmarshal(data, &tok); err != nil {
		return TokenSet{}, err
	}
	return tok, nil
}

func (s AuthStore) Save(tok TokenSet) error {
	if err := EnsureHome(s.Home); err != nil {
		return err
	}
	if tok.CreatedAt.IsZero() {
		tok.CreatedAt = s.now().UTC()
	}
	tok.UpdatedAt = s.now().UTC()
	data, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(authPath(s.Home), append(data, '\n'), 0o600)
}

func (s AuthStore) Logout() error {
	err := os.Remove(authPath(s.Home))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func RunLogin(ctx context.Context, store AuthStore, in io.Reader, out io.Writer) error {
	fmt.Fprintln(out, OAuthRiskNotice)
	fmt.Fprintln(out)
	start, err := NewOAuthStart()
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "Open this URL, authenticate, then paste the redirected URL or code#state:")
	fmt.Fprintln(out, start.URL)
	fmt.Fprint(out, "> ")
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return err
		}
		return errors.New("no OAuth callback provided")
	}
	if _, err := store.Exchange(ctx, scanner.Text(), start); err != nil {
		return err
	}
	fmt.Fprintln(out, "Anthropic OAuth session saved under Portal home.")
	return nil
}

type upstreamHTTPError struct {
	StatusCode int
	Message    string
}

func (e upstreamHTTPError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("upstream HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("upstream HTTP %d: %s", e.StatusCode, e.Message)
}
