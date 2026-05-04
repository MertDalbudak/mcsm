package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MertDalbudak/mcsm/internal/config"
)

func newTestAuthenticator(t *testing.T) (*Authenticator, string) {
	t.Helper()
	pw := "secret-token-value"
	hash := makeHash(t, pw)
	a, err := NewAuthenticator([]config.Token{{
		Name: "demo", Hash: hash, Scopes: []string{"*"},
	}})
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}
	return a, pw
}

func TestAuthenticate_MissingHeader(t *testing.T) {
	a, _ := newTestAuthenticator(t)
	rr := httptest.NewRecorder()
	called := false
	h := a.authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d want 401", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `"missing_token"`) {
		t.Errorf("body: %s", rr.Body.String())
	}
	if called {
		t.Error("inner handler should not be called")
	}
}

func TestAuthenticate_NotBearer(t *testing.T) {
	a, _ := newTestAuthenticator(t)
	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("Authorization", "Basic abc")
	a.authenticate(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rr, r)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d want 401", rr.Code)
	}
}

func TestAuthenticate_InvalidToken(t *testing.T) {
	a, _ := newTestAuthenticator(t)
	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("Authorization", "Bearer wrong-token")
	a.authenticate(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rr, r)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d want 401", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `"invalid_token"`) {
		t.Errorf("body: %s", rr.Body.String())
	}
}

func TestAuthenticate_Valid_PutsTokenOnContext(t *testing.T) {
	a, pw := newTestAuthenticator(t)
	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("Authorization", "Bearer "+pw)
	var got *TokenInfo
	a.authenticate(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = TokenFromContext(r.Context())
	})).ServeHTTP(rr, r)
	if rr.Code != http.StatusOK { // default when no WriteHeader
		t.Errorf("status: %d", rr.Code)
	}
	if got == nil || got.Name != "demo" {
		t.Errorf("token info missing/wrong: %+v", got)
	}
}

func TestRequireScope(t *testing.T) {
	mw := requireScope("server:command")
	cases := []struct {
		name   string
		scopes []string
		want   int
	}{
		{"wildcard", []string{"*"}, http.StatusOK},
		{"exact", []string{"server:command"}, http.StatusOK},
		{"nope", []string{"server:read"}, http.StatusForbidden},
		{"empty", []string{}, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/x", nil)
			r = r.WithContext(WithToken(r.Context(), &TokenInfo{Name: "x", Scopes: tc.scopes}))
			mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})).ServeHTTP(rr, r)
			if rr.Code != tc.want {
				t.Errorf("got %d want %d (body=%s)", rr.Code, tc.want, rr.Body.String())
			}
		})
	}
}

func TestTokenInfo_HasScope(t *testing.T) {
	var nilTok *TokenInfo
	if nilTok.HasScope("anything") {
		t.Error("nil token should never have scope")
	}
	tok := &TokenInfo{Scopes: []string{"server:read", "server:command"}}
	if !tok.HasScope("server:read") {
		t.Error("exact match should pass")
	}
	if tok.HasScope("server:admin") {
		t.Error("missing scope should fail")
	}
	wild := &TokenInfo{Scopes: []string{"*"}}
	if !wild.HasScope("anything-at-all") {
		t.Error("wildcard should grant any scope")
	}
}
