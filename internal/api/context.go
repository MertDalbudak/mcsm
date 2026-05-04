package api

import "context"

type ctxKey int

const (
	ctxKeyTraceID ctxKey = iota
	ctxKeyToken
)

// TokenInfo is the authenticated principal carried on the request context
// after the auth middleware succeeds.
type TokenInfo struct {
	Name   string
	Scopes []string
}

// HasScope returns true if the token bears scope or the wildcard "*".
func (t *TokenInfo) HasScope(scope string) bool {
	if t == nil {
		return false
	}
	for _, s := range t.Scopes {
		if s == "*" || s == scope {
			return true
		}
	}
	return false
}

func WithTraceID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKeyTraceID, id)
}

func TraceIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyTraceID).(string); ok {
		return v
	}
	return ""
}

func WithToken(ctx context.Context, t *TokenInfo) context.Context {
	return context.WithValue(ctx, ctxKeyToken, t)
}

func TokenFromContext(ctx context.Context) *TokenInfo {
	if v, ok := ctx.Value(ctxKeyToken).(*TokenInfo); ok {
		return v
	}
	return nil
}
