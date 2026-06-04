package portal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOAuthRefreshRotatesAndStoresTokens(t *testing.T) {
	var seenRefresh string
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		seenRefresh = body["refresh_token"].(string)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"expires_in":    3600,
		})
	}))
	defer tokenServer.Close()

	home := t.TempDir()
	now := time.Date(2026, 6, 4, 1, 0, 0, 0, time.UTC)
	store := AuthStore{Home: home, TokenURL: tokenServer.URL, Now: func() time.Time { return now }}
	if err := store.Save(TokenSet{
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		ExpiresAt:    now.Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	tok, err := store.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if seenRefresh != "old-refresh" {
		t.Fatalf("refresh used %q", seenRefresh)
	}
	if tok.AccessToken != "new-access" || tok.RefreshToken != "new-refresh" {
		t.Fatalf("unexpected refreshed token: %+v", tok)
	}
	stored, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if stored.RefreshToken != "new-refresh" {
		t.Fatalf("rotated refresh token was not stored: %+v", stored)
	}
}

func TestOAuthCallbackStateMustMatch(t *testing.T) {
	store := AuthStore{Home: t.TempDir()}
	_, err := store.Exchange(context.Background(), "code#different", OAuthStart{
		State:       "expected",
		Verifier:    "verifier",
		RedirectURI: CodeCallbackURL,
	})
	if err == nil {
		t.Fatalf("expected state mismatch error")
	}
}
