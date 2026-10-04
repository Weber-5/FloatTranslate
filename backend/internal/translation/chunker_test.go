package translation

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// repeatSentence builds a paragraph of n copies of a 46-rune sentence.
func repeatSentence(n int) string {
	return strings.Repeat("The quick brown fox jumps over the lazy dog. ", n)
}

// stripSpace removes every whitespace rune so chunk partitions can be
// compared against the original content.
func stripSpace(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			continue
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

// trimSp mirrors the trailing-whitespace trim SplitMarkdown applies to every
// chunk.
func trimSp(s string) string {
	return strings.TrimRight(s, " \t\n")
}

func TestDefaultChunkBudgetIsFrozen(t *testing.T) {
	if DefaultChunkBudget != 1800 {
		t.Fatalf("DefaultChunkBudget = %d, frozen value is 1800", DefaultChunkBudget)
	}
}

func TestSplitMarkdownTable(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		budget int
		want   []string
	}{
		{
			name:  "empty input yields no chunks",
			input: "",
			want:  nil,
		},
		{
			name:  "blank-only input yields no chunks",
			input: "\n\n   \n\n",
			want:  nil,
		},
		{
			name:  "single short paragraph stays whole",
			input: "Hello, world! This is a sentence.",
			want:  []string{"Hello, world! This is a sentence."},
		},
		{
			name:  "small blocks merge up to budget preserving blank lines",
			input: "# Title\n\nShort intro.\n\n- item one\n- item two\n\n> a quote\n\n<!-- keep -->\n\n---",
			want:  []string{"# Title\n\nShort intro.\n\n- item one\n- item two\n\n> a quote\n\n<!-- keep -->\n\n---"},
		},
		{
			name: "merge stops before exceeding budget",
			// Two ~900-rune paragraphs: 1802 > 1800, so they cannot merge.
			input: repeatSentence(20) + "\n\n" + repeatSentence(20),
			want:  []string{trimSp(repeatSentence(20)), trimSp(repeatSentence(20))},
		},
		{
			name:  "blank-line runs inside a merged chunk are preserved",
			input: "Alpha.\n\n\n\nBeta.\n\n\n\nGamma.",
			want:  []string{"Alpha.\n\n\n\nBeta.\n\n\n\nGamma."},
		},
		{
			name:  "fenced code block is never split and merges when it fits",
			input: repeatSentence(20) + "\n\n```go\nfunc main() {\n\tfmt.Println(\"hi\")\n}\n```\n\n" + repeatSentence(20),
			want: []string{
				// Merged chunks are verbatim input ranges: the paragraph's
				// trailing separator space stays (only chunk-boundary tails
				// are trimmed).
				repeatSentence(20) + "\n\n```go\nfunc main() {\n\tfmt.Println(\"hi\")\n}\n```",
				trimSp(repeatSentence(20)),
			},
		},
		{
			name:  "over-budget fenced code block stays whole",
			input: "```text\n" + strings.Repeat("x", DefaultChunkBudget+400) + "\n```",
			want:  []string{"```text\n" + strings.Repeat("x", DefaultChunkBudget+400) + "\n```"},
		},
		{
			name:  "tilde fence with blank lines stays one block",
			input: "intro line\n\n~~~\nline one\n\nline two\n~~~\n\noutro line",
			want:  []string{"intro line\n\n~~~\nline one\n\nline two\n~~~\n\noutro line"},
		},
		{
			name:   "over-budget paragraph splits at sentences keeping delimiters",
			input:  "Short one. Short two. Short three.",
			budget: 30,
			want:   []string{"Short one. Short two.", " Short three."},
		},
		{
			name: "over-budget block merges with neither neighbour",
			// repeatSentence(45) is 2069 runes > budget: it splits at sentence
			// granularity (40 + 5 sentences) while "head." and "tail." stay
			// separate chunks (split-block pieces never merge outwards).
			input: "head.\n\n" + repeatSentence(45) + "\n\ntail.",
			want: []string{
				"head.",
				trimSp(repeatSentence(40)),
				" " + trimSp(repeatSentence(5)),
				"tail.",
			},
		},
		{
			name: "over-budget list splits at items keeping markers",
			input: strings.Repeat("- item with a moderately long line of prose here. ", 25) + "\n" +
				strings.Repeat("- another item with a moderately long line of prose here. ", 25),
			want: []string{
				strings.TrimRight(strings.Repeat("- item with a moderately long line of prose here. ", 25), " "),
				strings.TrimRight(strings.Repeat("- another item with a moderately long line of prose here. ", 25), " "),
			},
		},
		{
			name:  "input of only protected span placeholders stays whole",
			input: "⟦P0⟧ ⟦P1⟧ ⟦P2⟧ and some connecting words.",
			want:  []string{"⟦P0⟧ ⟦P1⟧ ⟦P2⟧ and some connecting words."},
		},
		{
			name:  "table stays atomic even when over budget",
			input: "| col a | col b |\n|---|---|\n|" + strings.Repeat(" cell |", 500),
			want:  []string{"| col a | col b |\n|---|---|\n|" + strings.Repeat(" cell |", 500)},
		},
		{
			name:  "horizontal rule and heading survive merging",
			input: "***\n\n# Heading\n\nBody text.",
			want:  []string{"***\n\n# Heading\n\nBody text."},
		},
		{
			name:  "zero or negative budget falls back to the default",
			input: "A short paragraph.",
			want:  []string{"A short paragraph."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SplitMarkdown(tt.input, tt.budget)
			if len(got) != len(tt.want) {
				t.Fatalf("chunks = %d, want %d\n got: %q\nwant: %q", len(got), len(tt.want), got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("chunk %d = %q, want %q", i, got[i], tt.want[i])
				}
			}
			// The chunks must reproduce the original content (partition
			// property, whitespace-insensitive).
			if stripSpace(strings.Join(got, "")) != stripSpace(tt.input) {
				t.Errorf("chunk partition lost content:\n got %q\nwant %q", strings.Join(got, ""), tt.input)
			}
		})
	}
}

// TestSplitMarkdownRespectsBudget asserts the frozen budget invariant: no
// chunk exceeds the budget unless it is a single atomic block.
func TestSplitMarkdownRespectsBudget(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		budget int
	}{
		{"long paragraphs", repeatSentence(60), 1800},
		{"small budget", repeatSentence(60), 200},
		{"tiny budget", repeatSentence(60), 17},
		{"list", strings.Repeat(strings.Repeat("list item text here. ", 20)+"\n", 8), 300},
		{"quote", "> " + repeatSentence(40), 400},
		{"cjk content", strings.Repeat("这是一段用于测试切块的中文内容，包含逗号，也包含句号。", 60), 300},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for i, chunk := range SplitMarkdown(c.input, c.budget) {
				if n := utf8.RuneCountInString(chunk); n > c.budget {
					t.Errorf("chunk %d has %d runes > budget %d", i, n, c.budget)
				}
			}
		})
	}
}

func TestSplitMarkdownSentenceFallback(t *testing.T) {
	// 60 sentences of 44 runes (+ separator): the 1800 budget packs 40, then 20.
	input := repeatSentence(20) + " " + repeatSentence(20) + " " + repeatSentence(20)
	chunks := SplitMarkdown(input, 0)
	if len(chunks) != 2 {
		t.Fatalf("chunks = %d, want 2", len(chunks))
	}
	if n := strings.Count(chunks[0], "lazy dog."); n != 40 {
		t.Errorf("chunk 0 holds %d sentences, want 40", n)
	}
	if n := strings.Count(chunks[1], "lazy dog."); n != 20 {
		t.Errorf("chunk 1 holds %d sentences, want 20", n)
	}
	if !strings.HasSuffix(chunks[0], ".") {
		t.Errorf("first chunk must end at a sentence delimiter: %q", tail(chunks[0], 60))
	}
	if !strings.HasSuffix(chunks[1], ".") {
		t.Errorf("last chunk must keep its final delimiter: %q", tail(chunks[1], 60))
	}
}

// TestSplitMarkdownHardCutSentence covers the last-resort char-budget
// fallback for a single sentence without any internal punctuation.
func TestSplitMarkdownHardCutSentence(t *testing.T) {
	input := strings.Repeat("ab", DefaultChunkBudget) // 3600 runes, one "sentence"
	chunks := SplitMarkdown(input, 1800)
	if len(chunks) != 2 {
		t.Fatalf("chunks = %d, want 2", len(chunks))
	}
	if got := utf8.RuneCountInString(chunks[0]); got != 1800 {
		t.Errorf("first hard-cut chunk = %d runes, want 1800", got)
	}
	if got := utf8.RuneCountInString(chunks[1]); got != 1800 {
		t.Errorf("second hard-cut chunk = %d runes, want 1800", got)
	}
	if strings.Join(chunks, "") != input {
		t.Errorf("hard cut lost content")
	}
}

// TestSplitMarkdownHardCutIsRuneSafe cuts multi-byte content at rune
// boundaries (no invalid UTF-8 fragments).
func TestSplitMarkdownHardCutIsRuneSafe(t *testing.T) {
	input := strings.Repeat("中文内容", DefaultChunkBudget) // CJK, no ASCII punctuation
	chunks := SplitMarkdown(input, 1500)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for i, chunk := range chunks {
		if !utf8.ValidString(chunk) {
			t.Errorf("chunk %d is not valid UTF-8", i)
		}
	}
	if stripSpace(strings.Join(chunks, "")) != stripSpace(input) {
		t.Errorf("hard cut lost content")
	}
}

// TestSplitMarkdownNoCJKSentenceEnd verifies CJK punctuation is never treated
// as an English sentence end (docs/06 §8: only .!? count).
func TestSplitMarkdownNoCJKSentenceEnd(t *testing.T) {
	// A 。 in the middle must not create a boundary; the whole text fits the
	// budget so it must stay a single chunk.
	input := "This is one sentence。And it still contains the full stop at the end."
	if got := SplitMarkdown(input, 1800); len(got) != 1 || got[0] != input {
		t.Errorf("CJK fullstop must not split: got %q", got)
	}
	// Direct check of the sentence scanner: only the ASCII dot is a boundary.
	scannerInput := "One。Two. Three"
	spans := sentenceSpans(scannerInput)
	if len(spans) != 2 {
		t.Fatalf("sentence spans = %d, want 2 (%v)", len(spans), spans)
	}
	if got := scannerInput[spans[0].start:spans[0].end]; got != "One。Two." {
		t.Errorf("span 0 = %q, want %q", got, "One。Two.")
	}
	if got := scannerInput[spans[1].start:spans[1].end]; got != " Three" {
		t.Errorf("span 1 = %q, want %q", got, " Three")
	}
}

// TestSplitMarkdownProtectedSpanPlaceholdersStayInt verifies placeholders are
// never broken across chunk boundaries at sentence granularity.
func TestSplitMarkdownProtectedSpanPlaceholdersStayInt(t *testing.T) {
	input := strings.Repeat("Leading words here. ", 45) + " ⟦P7⟧ middle ⟦P8⟧ " + strings.Repeat("Trailing words here. ", 45)
	chunks := SplitMarkdown(input, 1800)
	var joined strings.Builder
	for _, c := range chunks {
		joined.WriteString(c)
	}
	for _, ph := range []string{"⟦P7⟧", "⟦P8⟧"} {
		if !strings.Contains(joined.String(), ph) {
			t.Errorf("placeholder %s broken across chunks in %q", ph, joined.String())
		}
	}
}

func tail(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}
