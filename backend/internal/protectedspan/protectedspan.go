// Package protectedspan implements Phase 2 protected span v1 (docs/00 §4,
// docs/06 §3): before translated text is sent to the provider, content that
// must survive translation untouched is replaced with unique placeholders
// ⟦P0⟧…⟦Pn⟧ and restored verbatim after schema validation.
//
// Protected v1: fenced code blocks, inline code, URLs, $…$ / $$…$$ math,
// Windows paths (drive-letter\…), /unix/paths and numbers-only tokens.
// Placeholders are unique per Mask call (= per chunk, docs/06 §8).
package protectedspan

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Placeholder delimiters. The ⟦ ⟧ brackets are chosen so they cannot appear
// inside ASCII source text and survive most tokenizer round-trips.
const (
	// PlaceholderPrefix is the shared prefix of every placeholder (⟦P).
	PlaceholderPrefix = "⟦P"
	placeholderSuffix = "⟧"
)

// Span is one protected region of the original text.
type Span struct {
	// Placeholder is the unique marker substituted into the masked text
	// (⟦P0⟧, ⟦P1⟧, …).
	Placeholder string
	// Content is the original protected text.
	Content string
}

// masker is one protected-region pattern with a validator for context
// requirements the regex alone cannot express (Go's RE2 has no lookbehind).
type masker struct {
	re      *regexp.Regexp
	precede func(before rune) bool // nil = any preceding character allowed
}

// preceders accepted for patterns that could otherwise swallow ordinary
// words (unix paths: "and/or" must NOT be masked).
func pathPrecede(before rune) bool {
	if before < 0 {
		return true // start of text
	}
	switch before {
	case ' ', '\t', '\n', '\r':
		return true
	case '(', '[', '{', '（', '【', '「', '『', '〈':
		return true
	}
	return false
}

var maskers = []masker{
	// 1. Fenced code blocks (``` … ```), highest priority.
	{re: regexp.MustCompile("(?s)`{3}.*?`{3}")},
	// 2. Inline code spans.
	{re: regexp.MustCompile("`[^`\n]+`")},
	// 3. URLs.
	{re: regexp.MustCompile(`(?i)\b(?:https?://|www\.)[^\s<>"'` + "`" + `]+`)},
	// 4. Display math ($$…$$) before inline math ($…$).
	{re: regexp.MustCompile(`(?s)\$\$.+?\$\$`)},
	{re: regexp.MustCompile(`\$[^$\n]+\$`)},
	// 5. Windows paths (C:\… or C:/…).
	{re: regexp.MustCompile(`[A-Za-z]:[\\/][^\s"']+`)},
	// 6. Unix paths (/usr/local/bin …) — must not start mid-word.
	{re: regexp.MustCompile(`/[\w@.%+-]+(?:/[\w@.%+-]+)+/?`), precede: pathPrecede},
	// 7. Numbers-only tokens (optionally grouped / decimal / percent).
	{re: regexp.MustCompile(`\d+(?:[.,]\d+)*%?`)},
}

// Masked is the result of Mask.
type Masked struct {
	// Text is the input with every protected region replaced by its
	// placeholder.
	Text string
	// Spans lists the placeholders in first-appearance order.
	Spans []Span
}

// Mask replaces every protected region of text with unique placeholders.
// Regions already consumed by a higher-priority masker are never re-matched.
// When several patterns match at the same offset the pattern listed first
// (highest priority) wins.
func Mask(text string) Masked {
	var spans []Span
	index := make(map[string]int) // content → span index (deduplicate repeats)
	var sb strings.Builder

	pos := 0
	for pos < len(text) {
		bestStart, bestEnd := -1, -1
		for _, m := range maskers {
			start, end := findMatch(text, pos, m)
			if start < 0 {
				continue
			}
			// Earliest match wins; same offset is impossible across
			// patterns with identical starts unless one is a prefix of
			// the other, in which case the masker list order already
			// expressed the preference — prefer the longer end then.
			if bestStart < 0 || start < bestStart || (start == bestStart && end > bestEnd) {
				bestStart, bestEnd = start, end
			}
		}
		if bestStart < 0 {
			sb.WriteString(text[pos:])
			break
		}
		sb.WriteString(text[pos:bestStart])
		content := text[bestStart:bestEnd]
		if idx, ok := index[content]; ok {
			sb.WriteString(spans[idx].Placeholder)
		} else {
			ph := PlaceholderPrefix + itoa(len(spans)) + placeholderSuffix
			index[content] = len(spans)
			spans = append(spans, Span{Placeholder: ph, Content: content})
			sb.WriteString(ph)
		}
		pos = bestEnd
	}
	return Masked{Text: sb.String(), Spans: spans}
}

// findMatch locates the first match of m's pattern at or after offset whose
// context (preceding character) satisfies m.precede, returning absolute
// [start, end) or (-1, -1).
func findMatch(text string, offset int, m masker) (int, int) {
	start, end := offset, -1
	for {
		loc := m.re.FindStringIndex(text[start:])
		if loc == nil {
			return -1, -1
		}
		start, end = start+loc[0], start+loc[1]
		if m.precede == nil || m.precede(precedeRune(text, start)) {
			return start, end
		}
		// Rejected context: retry just after the rejected start position.
		start = nextRuneOffset(text, start)
		if start >= end && start >= len(text) {
			return -1, -1
		}
	}
}

// nextRuneOffset returns the byte offset of the rune after offset.
func nextRuneOffset(text string, offset int) int {
	if offset >= len(text) {
		return len(text)
	}
	_, size := utf8.DecodeRuneInString(text[offset:])
	return offset + size
}

// precedeRune returns the rune immediately before offset (-1 at start).
func precedeRune(text string, offset int) rune {
	if offset <= 0 {
		return -1
	}
	runes := []rune(text[:offset])
	if len(runes) == 0 {
		return -1
	}
	return runes[len(runes)-1]
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}

// Restore substitutes every placeholder found in out with its original
// content (exact match first, then a case-insensitive pass to survive
// case-mangling models). It is all-or-nothing: when any placeholder is
// missing from out the input is returned unchanged together with the list of
// missing placeholders, and the caller must run its single repair attempt.
func Restore(out string, spans []Span) (string, []string) {
	var missing []string
	for _, sp := range spans {
		if strings.Contains(out, sp.Placeholder) || indexFold(out, sp.Placeholder) >= 0 {
			continue
		}
		missing = append(missing, sp.Placeholder)
	}
	if len(missing) > 0 {
		return out, missing
	}
	res := out
	for _, sp := range spans {
		res = strings.ReplaceAll(res, sp.Placeholder, sp.Content)
		if i := indexFold(res, sp.Placeholder); i >= 0 {
			res = res[:i] + sp.Content + res[i+len(sp.Placeholder):]
		}
	}
	return res, nil
}

// indexFold returns the byte index of the first case-insensitive occurrence
// of substr in s, or -1.
func indexFold(s, substr string) int {
	n := len(substr)
	if n == 0 {
		return 0
	}
	for i := 0; i+n <= len(s); {
		if strings.EqualFold(s[i:i+n], substr) {
			return i
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		if size == 0 {
			return -1
		}
		i += size
	}
	return -1
}

// RestoreString replaces every placeholder present in s with its original
// content, regardless of whether the other placeholders appear. Unlike
// Restore it never fails: strings that legitimately contain only some of the
// placeholders (individual segments, source echoes) are restored as far as
// possible.
func RestoreString(s string, spans []Span) string {
	res := s
	for _, sp := range spans {
		res = strings.ReplaceAll(res, sp.Placeholder, sp.Content)
		if i := indexFold(res, sp.Placeholder); i >= 0 {
			res = res[:i] + sp.Content + res[i+len(sp.Placeholder):]
		}
	}
	return res
}
