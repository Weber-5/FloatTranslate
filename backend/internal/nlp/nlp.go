// Package nlp implements the local text processing required before a
// translation request reaches the provider: normalization, language
// detection, word/text classification, tokenization and rule-based
// lemmatization (docs/00 §4, docs/06 §7).
package nlp

import (
	"regexp"
	"strings"
	"unicode"
)

// Kind values used across the API and storage layers.
const (
	KindWord = "word"
	KindText = "text"
)

// MaxWordLength is the maximum length of a single alphabetic token still
// classified as a word (docs: ≤40 chars).
const MaxWordLength = 40

// NormalizeInput trims surrounding whitespace and unifies line endings to
// "\n". It never alters inner content beyond line endings — the normalized
// input is the cache-key basis, so it must stay stable.
func NormalizeInput(input string) string {
	s := strings.ReplaceAll(input, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.TrimSpace(s)
}

// DetectLanguage reports whether English is the dominant language of input
// by comparing ASCII letter count against non-ASCII letter count. Mixed
// Chinese/English input is allowed as long as English dominates (docs/00 §4).
// Returns "en" when supported, "other" otherwise.
func DetectLanguage(input string) string {
	var ascii, other int
	for _, r := range input {
		switch {
		case r < unicode.MaxASCII && unicode.IsLetter(r):
			ascii++
		case unicode.IsLetter(r):
			other++
		}
	}
	if other > ascii {
		return "other"
	}
	return "en"
}

// IsSupportedLanguage reports whether DetectLanguage classifies input as
// English-dominant.
func IsSupportedLanguage(input string) bool {
	return DetectLanguage(input) == "en"
}

var wordShape = regexp.MustCompile(`^[A-Za-z]+(?:['’-][A-Za-z]+)*$`)

// ClassifyKind heuristically decides whether input is a single dictionary
// word (no spaces, ≤40 chars, letters with optional internal
// hyphens/apostrophes) or free text. Everything else is text.
func ClassifyKind(input string) string {
	s := NormalizeInput(input)
	if s == "" || len(s) > MaxWordLength {
		return KindText
	}
	if strings.ContainsAny(s, " \t\n") {
		return KindText
	}
	if wordShape.MatchString(s) {
		return KindWord
	}
	return KindText
}

var (
	urlPattern     = regexp.MustCompile(`(?i)\b(?:https?://|www\.)[^\s]+`)
	fencedPattern  = regexp.MustCompile("(?s)`{3}.*?`{3}")
	inlinePattern  = regexp.MustCompile("`[^`\n]*`")
	techPattern    = regexp.MustCompile("[_={}\\[\\]<>|\\\\/~^$@#&+]")
	wordRunPattern = regexp.MustCompile(`[A-Za-z]+(?:['’-][A-Za-z]+)*`)
)

// Tokenize extracts human-readable English word tokens from text.
//
// Excluded by design: URLs, fenced/inline code spans, chunks that look like
// code or technical identifiers (containing _ = { } [ ] < > | \ / ~ ^ $ @
// # & +), standalone numbers and punctuation. Punctuation is split from
// words; token order follows the input; duplicates are preserved.
func Tokenize(text string) []string {
	s := fencedPattern.ReplaceAllString(text, " ")
	s = inlinePattern.ReplaceAllString(s, " ")
	s = urlPattern.ReplaceAllString(s, " ")
	var tokens []string
	for _, chunk := range strings.Fields(s) {
		if techPattern.MatchString(chunk) {
			continue
		}
		tokens = append(tokens, wordRunPattern.FindAllString(chunk, -1)...)
	}
	return tokens
}

// Lemmatize applies conservative rule-based English suffix stripping:
//   - "-ing" → strip (undoing consonant doubling: running → run)
//   - "-ies" → "-y" (studies → study)
//   - "-ed"  → strip (stopped → stop)
//   - "-es"  → strip after sibilants (boxes → box, watches → watch)
//   - "-s"   → strip (cats → cat)
//
// When a rule would produce something shorter than 3 characters or no rule
// applies, the (lowercased) original word is returned as a conservative
// fallback. This is intentionally not a full dictionary lemmatizer.
func Lemmatize(word string) string {
	w := strings.ToLower(strings.TrimSpace(word))
	w = strings.TrimSuffix(w, "'s")
	w = strings.TrimSuffix(w, "’s")
	switch {
	case strings.HasSuffix(w, "ing") && len(w) >= 6:
		if stem := undoDoubling(w[:len(w)-3]); len(stem) >= 3 {
			return stem
		}
	case strings.HasSuffix(w, "ies") && len(w) >= 5:
		return w[:len(w)-3] + "y"
	case strings.HasSuffix(w, "ed") && len(w) >= 5:
		if stem := undoDoubling(w[:len(w)-2]); len(stem) >= 3 {
			return stem
		}
	case strings.HasSuffix(w, "es") && len(w) >= 5 && endsWithSibilant(w[:len(w)-2]):
		return w[:len(w)-2]
	case strings.HasSuffix(w, "s") && len(w) > 3 && !strings.HasSuffix(w, "ss"):
		return w[:len(w)-1]
	}
	return w
}

// undoDoubling reverses CVC consonant doubling introduced by -ing/-ed
// suffixes (running → runn → run, stopped → stopp → stop) while keeping
// legitimate double letters of base words (fall, class, buzz).
func undoDoubling(stem string) string {
	n := len(stem)
	if n >= 2 && stem[n-1] == stem[n-2] {
		switch stem[n-1] {
		case 's', 'l', 'z':
			return stem
		default:
			return stem[:n-1]
		}
	}
	return stem
}

func endsWithSibilant(stem string) bool {
	return strings.HasSuffix(stem, "s") || strings.HasSuffix(stem, "x") ||
		strings.HasSuffix(stem, "z") || strings.HasSuffix(stem, "ch") ||
		strings.HasSuffix(stem, "sh")
}
