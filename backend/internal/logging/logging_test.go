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

// TestRedactionSessionTokenValueEverywhere freezes the Phase 5 rule: the
// session token VALUE (as the middleware knows it) must never appear in any
// log line, no matter the attribute key it travels under.
func TestRedactionSessionTokenValueEverywhere(t *testing.T) {
	token := "sess-value-x7K92mQ"
	redactor := NewRedactor()
	redactor.Register(token)

	var buf bytes.Buffer
	logger := New(&buf, slog.LevelInfo, redactor)

	logger.Info("bearer leaked: "+token, "token", token, "nested", "prefix-"+token+"-suffix")
	logger.Info("auth header", slog.String("authorization", "Bearer "+token))

	out := buf.String()
	if strings.Contains(out, token) {
		t.Errorf("session token value leaked:\n%s", out)
	}
	if strings.Count(out, RedactedPlaceholder) < 4 {
		t.Errorf("expected several redactions:\n%s", out)
	}
}

// TestRedactionCredentialValue freezes the Phase 5 rule: a Credential
// Manager value loaded at runtime must be registered and therefore redacted
// wherever it would appear.
func TestRedactionCredentialValue(t *testing.T) {
	apiKey := "sk-credential-marker-31337"
	redactor := NewRedactor()
	redactor.Register(apiKey)

	var buf bytes.Buffer
	logger := New(&buf, slog.LevelInfo, redactor)
	logger.Info("provider call", slog.String("api_key", apiKey), slog.String("msg", "key="+apiKey))

	out := buf.String()
	if strings.Contains(out, apiKey) {
		t.Errorf("credential value leaked:\n%s", out)
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

func TestContentLoggingGuard(t *testing.T) {
	t.Setenv(envContentDebug, "")

	var buf bytes.Buffer
	logger := New(&buf, slog.LevelDebug, NewRedactor())

	// Default (env unset): content logs are dropped entirely.
	LogContent(logger, slog.LevelInfo, "user content", "text", "hello world")
	if buf.Len() != 0 {
		t.Errorf("content log emitted while FT_DEBUG_CONTENT is unset:\n%s", buf.String())
	}
	if ContentEnabled() {
		t.Error("ContentEnabled must default to false")
	}

	// Explicit opt-in: the line is emitted (and still redacted).
	t.Setenv(envContentDebug, "1")
	LogContent(logger, slog.LevelInfo, "user content", "text", "hello secretvalue99")
	out := buf.String()
	if !strings.Contains(out, "user content") {
		t.Errorf("content log not emitted with FT_DEBUG_CONTENT=1:\n%s", out)
	}
	if !strings.Contains(out, "secretvalue99") {
		t.Errorf("content log missing payload:\n%s", out)
	}
}
