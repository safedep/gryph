package session

import "context"

type ctxKey struct{}

// WithSession returns a derived context that carries the given session.
func WithSession(ctx context.Context, sess *Session) context.Context {
	if sess == nil {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, sess)
}

// FromContext returns the session previously stored via WithSession, if any.
func FromContext(ctx context.Context) (*Session, bool) {
	if ctx == nil {
		return nil, false
	}
	sess, ok := ctx.Value(ctxKey{}).(*Session)
	return sess, ok && sess != nil
}
