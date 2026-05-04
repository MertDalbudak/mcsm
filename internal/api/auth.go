package api

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/MertDalbudak/mcsm/internal/config"
	"golang.org/x/crypto/argon2"
)

// Authenticator validates bearer tokens against the configured argon2id hashes.
type Authenticator struct {
	tokens []resolvedToken
}

type resolvedToken struct {
	name    string
	scopes  []string
	hashRaw string // full PHC string from config
	parsed  *argonHash
}

// NewAuthenticator parses every token's PHC argon2id hash up-front so that
// per-request verification is just a Compare against a fixed parameterization.
func NewAuthenticator(tokens []config.Token) (*Authenticator, error) {
	out := make([]resolvedToken, 0, len(tokens))
	for _, t := range tokens {
		ph, err := parseArgon2id(t.Hash)
		if err != nil {
			return nil, err
		}
		out = append(out, resolvedToken{
			name:    t.Name,
			scopes:  t.Scopes,
			hashRaw: t.Hash,
			parsed:  ph,
		})
	}
	return &Authenticator{tokens: out}, nil
}

// authenticate is middleware that requires Authorization: Bearer <token>.
// On success it puts a *TokenInfo on the request context.
func (a *Authenticator) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if header == "" {
			WriteError(w, r, http.StatusUnauthorized, CodeMissingToken, "missing Authorization header", nil)
			return
		}
		const prefix = "Bearer "
		if !strings.HasPrefix(header, prefix) {
			WriteError(w, r, http.StatusUnauthorized, CodeMissingToken, "Authorization header must use Bearer scheme", nil)
			return
		}
		raw := strings.TrimSpace(header[len(prefix):])
		if raw == "" {
			WriteError(w, r, http.StatusUnauthorized, CodeMissingToken, "empty bearer token", nil)
			return
		}
		tok := a.match(raw)
		if tok == nil {
			WriteError(w, r, http.StatusUnauthorized, CodeInvalidToken, "invalid token", nil)
			return
		}
		ctx := WithToken(r.Context(), &TokenInfo{Name: tok.name, Scopes: tok.scopes})
		// Tell outer middleware which token authenticated us. The audit
		// log uses this; access log + recoverer ignore it.
		if rec, ok := w.(interface{ SetTokenName(string) }); ok {
			rec.SetTokenName(tok.name)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *Authenticator) match(raw string) *resolvedToken {
	rawBytes := []byte(raw)
	for i := range a.tokens {
		t := &a.tokens[i]
		derived := argon2.IDKey(rawBytes, t.parsed.salt, t.parsed.t, t.parsed.m, t.parsed.p, t.parsed.keyLen)
		if subtle.ConstantTimeCompare(derived, t.parsed.hash) == 1 {
			return t
		}
	}
	return nil
}

// requireScope returns middleware that gates a handler on a particular scope.
// It runs after authenticate; the token must already be on the context.
func requireScope(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tok := TokenFromContext(r.Context())
			if !tok.HasScope(scope) {
				WriteError(w, r, http.StatusForbidden, CodeScopeDenied,
					"token lacks required scope",
					map[string]any{"required": scope, "have": tok.Scopes},
				)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
