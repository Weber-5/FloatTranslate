package chat

import (
	"strings"
	"testing"

	"github.com/Weber-5/FloatTranslate/backend/internal/repository"
)

// mustRow is a small helper for compose fixtures.
func mustRow(role, content string) repository.MessageRow {
	return repository.MessageRow{ID: "m_" + role + "_" + content, Role: role, Content: content}
}

func TestComposeMessagesFrozenOrder(t *testing.T) {
	msgs := ComposeMessages(ComposeInput{
		SystemPrompt:   "SYS",
		GlobalContext:  "GLOBAL",
		ChatContext:    "CONV",
		CompactSummary: "SUMMARY",
		ReferenceText:  "REF",
		History: []repository.MessageRow{
			mustRow("user", "u1"),
			mustRow("assistant", "a1"),
			mustRow("system_internal", "should be skipped"),
			mustRow("user", "u2"),
		},
	})
	want := []struct{ role, contentContains string }{
		{"system", "SYS"},
		{"system", "GLOBAL"},
		{"system", "CONV"},
		{"system", "SUMMARY"},
		{"system", "REF"},
		{"user", "u1"},
		{"assistant", "a1"},
		{"user", "u2"},
	}
	if len(msgs) != len(want) {
		t.Fatalf("messages = %d, want %d: %+v", len(msgs), len(want), msgs)
	}
	for i, w := range want {
		if msgs[i].Role != w.role {
			t.Errorf("msgs[%d].role = %q, want %q", i, msgs[i].Role, w.role)
		}
		if !strings.Contains(msgs[i].Content, w.contentContains) {
			t.Errorf("msgs[%d].content = %q, want it to contain %q", i, msgs[i].Content, w.contentContains)
		}
	}
	// system_internal rows never reach the provider.
	for _, m := range msgs {
		if strings.Contains(m.Content, "should be skipped") {
			t.Errorf("system_internal row leaked into the provider messages: %+v", m)
		}
	}
}

func TestComposeMessagesEmptyContextsDropped(t *testing.T) {
	msgs := ComposeMessages(ComposeInput{
		SystemPrompt: DefaultSystemPrompt,
		History:      []repository.MessageRow{mustRow("user", "hi")},
	})
	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want 2 (system + user): %+v", len(msgs), msgs)
	}
	if msgs[0].Role != "system" || msgs[0].Content != DefaultSystemPrompt {
		t.Errorf("system message = %+v", msgs[0])
	}
	if msgs[1].Role != "user" || msgs[1].Content != "hi" {
		t.Errorf("user message = %+v", msgs[1])
	}
}

func TestEstimateTokens(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"abcd", 1},
		{"abcdefgh", 2},
		{strings.Repeat("中", 12), 3}, // 12 runes / 4
	}
	for _, c := range cases {
		if got := EstimateTokens(c.in); got != c.want {
			t.Errorf("EstimateTokens(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestBuildTranscriptSkipsSystemRows(t *testing.T) {
	out := buildTranscript([]repository.MessageRow{
		mustRow("user", "你好"),
		mustRow("system_internal", "noise"),
		mustRow("assistant", "你好！"),
	})
	if !strings.Contains(out, "user: 你好") || !strings.Contains(out, "assistant: 你好！") {
		t.Errorf("transcript = %q", out)
	}
	if strings.Contains(out, "noise") {
		t.Errorf("system rows must not enter the transcript: %q", out)
	}
}
