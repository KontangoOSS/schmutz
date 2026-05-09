package identity

import "context"

type Caller struct {
	Name           string
	RoleAttributes []string
}

func (c Caller) HasRole(attr string) bool {
	for _, a := range c.RoleAttributes {
		if a == attr {
			return true
		}
	}
	return false
}

type ctxKey struct{}

var callerKey ctxKey

func WithCaller(ctx context.Context, c Caller) context.Context {
	return context.WithValue(ctx, callerKey, c)
}

func FromContext(ctx context.Context) (Caller, bool) {
	c, ok := ctx.Value(callerKey).(Caller)
	return c, ok
}
