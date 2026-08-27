package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanitizeRequestLogHeadersRemovesCredentialHeaders(t *testing.T) {
	headers := map[string][]string{
		"Authorization": {"Bearer secret"},
		"Cookie":        {"session=secret"},
		"Content-Type":  {"application/json"},
		"X-Request-Id":  {"request-1"},
	}

	sanitized := sanitizeRequestLogHeaders(headers)

	require.Equal(t, []string{"[REDACTED]"}, sanitized["Authorization"])
	require.Equal(t, []string{"[REDACTED]"}, sanitized["Cookie"])
	require.Equal(t, []string{"application/json"}, sanitized["Content-Type"])
	require.Equal(t, []string{"request-1"}, sanitized["X-Request-Id"])
}

func TestTruncateRequestLogBodyPreservesByteLimitAndMarker(t *testing.T) {
	body, truncated := truncateRequestLogBody([]byte("0123456789"), 4)

	require.True(t, truncated)
	require.Equal(t, []byte("0123"), body)
}

func TestTruncateRequestLogBodyKeepsShortBody(t *testing.T) {
	body, truncated := truncateRequestLogBody([]byte("body"), 8)

	require.False(t, truncated)
	require.Equal(t, []byte("body"), body)
}

func TestSanitizeRequestLogHeadersMasksKeyLikeHeaders(t *testing.T) {
	headers := map[string][]string{
		"X-Api-Key": {"secret"},
		"X-Custom":  {"visible"},
	}

	sanitized := sanitizeRequestLogHeaders(headers)

	require.Equal(t, []string{"[REDACTED]"}, sanitized["X-Api-Key"])
	require.Equal(t, []string{"visible"}, sanitized["X-Custom"])
}
