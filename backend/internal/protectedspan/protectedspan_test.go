package protectedspan

import (
	"strings"
	"testing"
)

func TestMaskRestoreRoundTripVerbatim(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []string // protected snippets that must survive verbatim
	}{
		{"fenced code", "Intro\n```go\nfmt.Println(\"hi\")\n```\nOutro", []string{"```go\nfmt.Println(\"hi\")\n```"}},
		{"inline code", "Run `npm install` now", []string{"`npm install`"}},
		{"url", "Docs at https://example.com/a/b?x=1 end", []string{"https://example.com/a/b?x=1"}},
		{"www url", "See www.example.com/page here", []string{"www.example.com/page"}},
		{"display math", "The $$E = mc^2$$ formula", []string{"$$E = mc^2$$"}},
		{"inline math", "Value $a + b$ here", []string{"$a + b$"}},
		{"windows path", "Stored at C:\\Users\\test\\data ok", []string{`C:\Users\test\data`}},
		{"windows forward path", "Open C:/data/cache now", []string{"C:/data/cache"}},
		{"unix path", "Edit /usr/local/bin/tool today", []string{"/usr/local/bin/tool"}},
		{"number", "There are 12345 items", []string{"12345"}},
		{"decimal", "Pi is 3.14 exactly", []string{"3.14"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := Mask(tc.input)
			if len(m.Spans) == 0 {
				t.Fatalf("no spans detected in %q", tc.input)
			}
			if strings.Contains(m.Text, tc.want[0]) {
				t.Errorf("masked text still contains protected content: %q", m.Text)
			}
			restored, missing := Restore(m.Text, m.Spans)
			if len(missing) != 0 {
				t.Fatalf("unexpected missing placeholders: %v", missing)
			}
			if restored != tc.input {
				t.Errorf("round trip mismatch:\n got %q\nwant %q", restored, tc.input)
			}
		})
	}
}

func TestMaskPlaceholdersAreUniquePerCall(t *testing.T) {
	m := Mask("Use 1 and 1 and 2")
	if len(m.Spans) != 2 {
		t.Fatalf("spans = %d (%+v), want 2 (repeat content deduplicated)", len(m.Spans), m.Spans)
	}
	if m.Spans[0].Placeholder != "⟦P0⟧" || m.Spans[1].Placeholder != "⟦P1⟧" {
		t.Errorf("placeholders = %s, %s", m.Spans[0].Placeholder, m.Spans[1].Placeholder)
	}
	// A second call restarts numbering (uniqueness is per chunk).
	m2 := Mask("x 7")
	if m2.Spans[0].Placeholder != "⟦P0⟧" {
		t.Errorf("second call placeholder = %s", m2.Spans[0].Placeholder)
	}
}

func TestMaskPreservesPlainText(t *testing.T) {
	in := "The quick brown fox jumps over the lazy dog."
	m := Mask(in)
	if m.Text != in {
		t.Errorf("plain text modified: %q", m.Text)
	}
	if len(m.Spans) != 0 {
		t.Errorf("unexpected spans: %+v", m.Spans)
	}
}

func TestMaskDoesNotSwallowOrdinaryWordsAsPaths(t *testing.T) {
	m := Mask("This and/or that is a word/slash pair.")
	if len(m.Spans) != 0 {
		t.Errorf("ordinary slash words masked as paths: %+v → %q", m.Spans, m.Text)
	}
}

func TestMaskNestedProtectedContentOnce(t *testing.T) {
	// URL inside a fenced block: the fence wins, the URL inside is part of
	// the same span, not masked twice.
	m := Mask("```\nvisit https://x.com/a\n```")
	if len(m.Spans) != 1 {
		t.Fatalf("spans = %+v, want 1", m.Spans)
	}
	if !strings.Contains(m.Spans[0].Content, "https://x.com/a") {
		t.Errorf("fence content = %q", m.Spans[0].Content)
	}
}

func TestRestoreDetectsMissingPlaceholders(t *testing.T) {
	m := Mask("code `x=1` and url https://a.io/b")
	lost := m.Spans[0].Placeholder
	text := strings.ReplaceAll(m.Text, lost, "")
	restored, missing := Restore(text, m.Spans)
	if len(missing) != 1 || missing[0] != lost {
		t.Fatalf("missing = %v, want [%s]", missing, lost)
	}
	if restored != text {
		t.Errorf("text must stay untouched when restore fails: %q vs %q", restored, text)
	}
}

func TestRestoreIsCaseLenient(t *testing.T) {
	m := Mask("code `x=1` here")
	mangled := strings.ReplaceAll(m.Text, m.Spans[0].Placeholder, strings.ToLower(m.Spans[0].Placeholder))
	restored, missing := Restore(mangled, m.Spans)
	if len(missing) != 0 {
		t.Fatalf("unexpected missing: %v", missing)
	}
	if !strings.Contains(restored, "`x=1`") {
		t.Errorf("case-mangled placeholder not restored: %q", restored)
	}
}

func TestRestoreStringPartial(t *testing.T) {
	m := Mask("a 1 b 2 c")
	partial := "only ⟦P0⟧ here"
	got := RestoreString(partial, m.Spans)
	if !strings.Contains(got, m.Spans[0].Content) || strings.Contains(got, "⟦P") {
		t.Errorf("partial restore failed: %q", got)
	}
}
