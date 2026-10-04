package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestRedactionNeverEmitsSecretMarker(t *testing.T) {
	secret := "supersecrettoken123"
	redactor := NewRedactor()
	redactor.Register(secret)

	var buf bytes.Buffer
	logger := New(&buf, slog.LevelDebug, redactor)

	logger.Info("user message", slog.String("note", "the token is "+secret+" keep it safe"))
	logger.Info("headers",
		slog.String("authorization", "Bearer "+secret),
		slog.String("api_key", "sk-abc123xyz"),
		slog.String("session_token", secret),
		slog.String("request", `POST /x with "api_key":"sk-987654" inline`),
	)
	logger.Info("unregistered bearer header: Bearer Zx9qLm2Np4RsTu8V")

	out := buf.String()
	for _, banned := range []string{
		secret,
		"sk-abc123xyz",
		"sk-987654",
		"Zx9qLm2Np4RsTu8V",
	} {
		if strings.Contains(out, banned) {
			t.Errorf("log output leaks secret %q:\n%s", banned, out)
		}
	}
	if !strings.Contains(out, RedactedPlaceholder) {
		t.Errorf("expected %s in output:\n%s", RedactedPlaceholder, out)
	}
	if strings.Count(out, RedactedPlaceholder) < 5 {
		t.Errorf("expected multiple redactions, got:\n%s", out)
	}
}

func TestRedactStringPatterns(t *testing.T) {
	r := NewRedactor()
	r.Register("sess_tok_value_9182")
	cases := []struct{ in, wantAbsent, wantContain string }{
		{
			in:          "Authorization: Bearer sess_tok_value_9182",
			wantAbsent:  "sess_tok_value_9182",
			wantContain: RedactedPlaceholder,
		},
		{
			in:          `{"api_key":"sk-plain-value"}`,
			wantAbsent:  "sk-plain-value",
			wantContain: RedactedPlaceholder,
		},
	}
	for _, c := range cases {
		got := r.Redact(c.in)
		if strings.Contains(got, c.wantAbsent) {
			t.Errorf("Redact(%q) = %q, still contains %q", c.in, got, c.wantAbsent)
		}
		if !strings.Contains(got, c.wantContain) {
			t.Errorf("Redact(%q) = %q, missing %q", c.in, got, c.wantContain)
		}
	}
}

func TestRedactPreservesCleanValues(t *testing.T) {
	r := NewRedactor()
	r.Register("a-registered-secret-value")
	clean := "GET /api/v1/history 200 12ms"
	if got := r.Redact(clean); got != clean {
		t.Errorf("Redact mangled clean value: %q", got)
	}
	if got := r.Redact("nothing to see"); got != "nothing to see" {
		t.Errorf("Redact mangled clean value: %q", got)
	}
}
