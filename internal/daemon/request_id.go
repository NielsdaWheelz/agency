package daemon

import (
	"context"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

const maxRequestIDLength = 128

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type requestIDContextKey struct{}

func newRequestID() string {
	return uuid.New().String()
}

func normalizeRequestID(candidate string) string {
	trimmed := strings.TrimSpace(candidate)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) > maxRequestIDLength {
		return ""
	}
	if !requestIDPattern.MatchString(trimmed) {
		return ""
	}
	return trimmed
}

func resolveOrGenerateRequestID(candidate string) string {
	if normalized := normalizeRequestID(candidate); normalized != "" {
		return normalized
	}
	return newRequestID()
}

func requestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	raw, _ := ctx.Value(requestIDContextKey{}).(string)
	return normalizeRequestID(raw)
}

func withRequestIDContext(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDContextKey{}, requestID)
}
