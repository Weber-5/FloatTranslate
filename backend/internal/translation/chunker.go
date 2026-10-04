// chunker.go implements Phase 3 long text chunking (docs/06 §8, frozen
// contract): long text is split into sequentially translated chunks with
//
//  1. Markdown block boundary   (blank-line separated blocks are the
//     primary units; adjacent small blocks are
//     merged up to the budget),
//  2. Paragraph                 (list items for list blocks),
//  3. Sentence                  ([.!?] + whitespace/end, delimiter kept
//     with the sentence; CJK punctuation is
//     never a sentence boundary),
//  4. Char-budget fallback      (clause punctuation ,;: then a hard cut at
//     budget runes).
//
// A chunk never exceeds the budget with exactly two exceptions: fenced code
// blocks (never split inside) and other atomic blocks (tables, single-line
// headings, horizontal rules, HTML comments) that alone exceed the budget.
// A single sentence is never split unless it alone exceeds the budget.
package translation

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// DefaultChunkBudget is the frozen per-chunk target budget in runes
// (docs/06 §8: chunk target ≤ 1800 chars, measured with RuneCount).
const DefaultChunkBudget = 1800

// SplitMarkdown partitions input into chunks of at most budget runes each
// following the frozen split order documented on the package chunker. It is
// a pure function and an (almost) lossless partition of input: chunks
// concatenated reproduce the input except for trimmed trailing whitespace
// per chunk and any leading blank lines.
func SplitMarkdown(input string, budget int) []string {
	if budget <= 0 {
		budget = DefaultChunkBudget
	}
	blocks := splitBlocks(input)
	if len(blocks) == 0 {
		return nil
	}

	type unit struct {
		span      block
		mergeable bool // may be packed together with the previous units
	}
	var units []unit
	for _, b := range blocks {
		if utf8.RuneCountInString(strings.TrimRight(input[b.start:b.end], " \t\n")) <= budget {
			units = append(units, unit{span: b, mergeable: true})
			continue
		}
		for _, part := range oversizedUnits(input, b, budget) {
			units = append(units, unit{span: part, mergeable: false})
		}
	}

	var chunks []string
	curStart, curEnd := -1, -1
	appendChunk := func(start, end int) {
		chunks = append(chunks, strings.TrimRight(input[start:end], " \t\n"))
	}
	for _, u := range units {
		if curStart >= 0 && u.mergeable && utf8.RuneCountInString(input[curStart:u.span.end]) <= budget {
			curEnd = u.span.end
			continue
		}
		if curStart >= 0 {
			appendChunk(curStart, curEnd)
			curStart = -1
		}
		if u.mergeable {
			curStart, curEnd = u.span.start, u.span.end
			continue
		}
		appendChunk(u.span.start, u.span.end)
	}
	if curStart >= 0 {
		appendChunk(curStart, curEnd)
	}
	return chunks
}

// oversizedUnits splits one over-budget block into unit spans. Units of a
// split block are never merged with neighbouring blocks (the "\n\n" assembly
// join keeps their boundaries visible).
func oversizedUnits(input string, b block, budget int) []block {
	atomic := []block{b}
	switch b.kind {
	case blockFence, blockTable, blockRule, blockHTMLComment:
		// Atomic blocks are never split, even when over budget.
		return atomic
	case blockHeading:
		if !strings.Contains(input[b.start:b.end], "\n") {
			return atomic
		}
		// Degenerate block (heading glued to following text without a blank
		// line): fall through to the paragraph treatment.
		return paragraphUnits(input, b, budget)
	case blockList:
		var out []block
		for _, part := range packSpans(input, listItemUnits(input, b, budget), budget) {
			out = append(out, block{start: part.start, end: part.end, kind: blockList})
		}
		return out
	default: // paragraph, blockquote
		return paragraphUnits(input, b, budget)
	}
}

// paragraphUnits splits an over-budget prose block at sentence (then clause,
// then hard-cut) granularity.
func paragraphUnits(input string, b block, budget int) []block {
	var out []block
	for _, part := range packSpans(input, sentenceUnits(input[b.start:b.end], b.start, budget), budget) {
		out = append(out, block{start: part.start, end: part.end, kind: blockParagraph})
	}
	return out
}

// blockKind classifies a markdown block for chunking decisions.
type blockKind int

const (
	blockParagraph blockKind = iota
	blockFence
	blockHeading
	blockRule
	blockTable
	blockHTMLComment
	blockBlockquote
	blockList
)

// block is a byte span [start, end) of the input. Span ends include the
// trailing blank-line separator up to the next block (the last block ends at
// len(input)), so the concatenation of all block texts equals the input.
type block struct {
	start, end int
	kind       blockKind
}

var (
	fenceOpenRe  = regexp.MustCompile("^[`]{3,}|^[~]{3,}")
	atxHeadingRe = regexp.MustCompile(`^#{1,6}(?:\s|$)`)
	hrRe         = regexp.MustCompile(`^\s{0,3}(?:(?:-\s*){3,}|(?:\*\s*){3,}|(?:_\s*){3,})$`)
	tableLineRe  = regexp.MustCompile(`^\s{0,3}\|`)
	blockquoteRe = regexp.MustCompile(`^\s{0,3}>`)
	listMarkerRe = regexp.MustCompile(`^\s{0,3}(?:[-*+]|\d{1,9}[.)])(?:\s|$)`)
)

// splitBlocks groups the input into blank-line separated blocks: consecutive
// non-blank lines belong to the same block until a blank line appears. Fenced
// code blocks (``` or ~~~) are never broken apart: blank lines inside a fence
// do not split. Lines before the first block (leading blanks) belong to no
// block.
func splitBlocks(s string) []block {
	var blocks []block
	curStart := -1
	curKind := blockParagraph
	prevBlank := true // no block open yet
	inFence := false
	fenceChar := byte('`')
	fenceLen := 0

	closeAt := func(end int) {
		blocks = append(blocks, block{start: curStart, end: end, kind: curKind})
		curStart = -1
	}

	for offset := 0; offset < len(s); {
		nl := strings.IndexByte(s[offset:], '\n')
		line, next := s[offset:], len(s)
		if nl >= 0 {
			line, next = s[offset:offset+nl], offset+nl+1
		}
		if inFence {
			trimmed := strings.TrimSpace(line)
			if len(trimmed) >= fenceLen && strings.Trim(trimmed, string(fenceChar)) == "" {
				inFence = false
			}
			prevBlank = false
			offset = next
			continue
		}
		if strings.TrimSpace(line) == "" {
			prevBlank = true
			offset = next
			continue
		}
		if curStart >= 0 && prevBlank {
			closeAt(offset)
		}
		if curStart < 0 {
			trimmed := strings.TrimLeft(line, " \t")
			curStart = offset
			curKind = blockParagraph
			if m := fenceOpenRe.FindString(trimmed); m != "" {
				curKind = blockFence
				fenceChar = m[0]
				fenceLen = len(m)
				inFence = true
			} else if atxHeadingRe.MatchString(trimmed) {
				curKind = blockHeading
			} else if hrRe.MatchString(line) {
				curKind = blockRule
			} else if tableLineRe.MatchString(trimmed) {
				curKind = blockTable
			} else if strings.HasPrefix(trimmed, "<!--") {
				curKind = blockHTMLComment
			} else if blockquoteRe.MatchString(trimmed) {
				curKind = blockBlockquote
			} else if listMarkerRe.MatchString(line) {
				curKind = blockList
			}
		}
		prevBlank = false
		offset = next
	}
	if curStart >= 0 {
		closeAt(len(s))
	}
	return blocks
}

// sentenceUnits returns sentence spans of text (offset by base). A sentence
// longer than the budget falls back to clause boundaries and, as a last
// resort, a hard cut at budget runes.
func sentenceUnits(text string, base, budget int) []chunkSpan {
	var out []chunkSpan
	for _, sp := range sentenceSpans(text) {
		if utf8.RuneCountInString(text[sp.start:sp.end]) <= budget {
			out = append(out, chunkSpan{start: base + sp.start, end: base + sp.end})
			continue
		}
		for _, c := range clauseUnits(text[sp.start:sp.end], budget) {
			out = append(out, chunkSpan{start: base + sp.start + c.start, end: base + sp.start + c.end})
		}
	}
	return out
}

// sentenceSpans splits s after a delimiter in [.!?] that is followed by
// whitespace or the end of the text, keeping the delimiter with the sentence.
// CJK/fullwidth punctuation is intentionally never a boundary (English input,
// docs/06 §8).
func sentenceSpans(s string) []chunkSpan {
	var out []chunkSpan
	start := 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		if r != '.' && r != '!' && r != '?' {
			continue
		}
		if i >= len(s) {
			out = append(out, chunkSpan{start: start, end: i})
			start = i
			break
		}
		next, _ := utf8.DecodeRuneInString(s[i:])
		if next == ' ' || next == '\t' || next == '\n' {
			out = append(out, chunkSpan{start: start, end: i})
			start = i
		}
	}
	if start < len(s) {
		out = append(out, chunkSpan{start: start, end: len(s)})
	}
	return out
}

// clauseUnits splits an over-budget sentence after clause punctuation
// (,;: + whitespace/end) and hard-cuts clauses that alone exceed the budget.
func clauseUnits(s string, budget int) []chunkSpan {
	var pieces []chunkSpan
	for _, sp := range clauseSpans(s) {
		if utf8.RuneCountInString(s[sp.start:sp.end]) <= budget {
			pieces = append(pieces, sp)
			continue
		}
		pieces = append(pieces, hardCutSpans(s[sp.start:sp.end], sp.start, budget)...)
	}
	return packSpans(s, pieces, budget)
}

// clauseSpans splits s after a delimiter in [,;:] followed by whitespace or
// end of text, keeping the delimiter with the left clause.
func clauseSpans(s string) []chunkSpan {
	var out []chunkSpan
	start := 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		if r != ',' && r != ';' && r != ':' {
			continue
		}
		if i >= len(s) {
			out = append(out, chunkSpan{start: start, end: i})
			start = i
			break
		}
		next, _ := utf8.DecodeRuneInString(s[i:])
		if next == ' ' || next == '\t' || next == '\n' {
			out = append(out, chunkSpan{start: start, end: i})
			start = i
		}
	}
	if start < len(s) {
		out = append(out, chunkSpan{start: start, end: len(s)})
	}
	return out
}

// hardCutSpans cuts sub into pieces of at most budget runes on rune
// boundaries. It is the last-resort fallback (docs/06 §8 item 4).
func hardCutSpans(sub string, base, budget int) []chunkSpan {
	var out []chunkSpan
	start, count := 0, 0
	for i := 0; i < len(sub); {
		_, size := utf8.DecodeRuneInString(sub[i:])
		i += size
		count++
		if count == budget {
			out = append(out, chunkSpan{start: base + start, end: base + i})
			start = i
			count = 0
		}
	}
	if start < len(sub) {
		out = append(out, chunkSpan{start: base + start, end: base + len(sub)})
	}
	return out
}

// packSpans greedily packs sorted, contiguous spans into pieces whose
// combined rune count stays within budget. Pieces already within budget are
// never split further.
func packSpans(s string, spans []chunkSpan, budget int) []chunkSpan {
	if len(spans) == 0 {
		return nil
	}
	var out []chunkSpan
	cur := spans[0]
	for _, sp := range spans[1:] {
		if utf8.RuneCountInString(s[cur.start:sp.end]) <= budget {
			cur.end = sp.end
			continue
		}
		out = append(out, cur)
		cur = sp
	}
	return append(out, cur)
}

// listItemUnits returns spans for the items of a list block. An item alone
// over the budget falls back to sentence granularity.
func listItemUnits(input string, b block, budget int) []chunkSpan {
	var out []chunkSpan
	for _, item := range listItems(input, b) {
		if utf8.RuneCountInString(strings.TrimRight(input[item.start:item.end], " \t\n")) <= budget {
			out = append(out, item)
			continue
		}
		out = append(out, sentenceUnits(input[item.start:item.end], item.start, budget)...)
	}
	return out
}

// listItems splits a list block at its top-level marker lines (each item
// keeps its continuation lines).
func listItems(input string, b block) []chunkSpan {
	text := input[b.start:b.end]
	var out []chunkSpan
	itemStart := -1
	for offset := 0; offset < len(text); {
		nl := strings.IndexByte(text[offset:], '\n')
		line, next := text[offset:], len(text)
		if nl >= 0 {
			line, next = text[offset:offset+nl], offset+nl+1
		}
		if itemStart < 0 {
			if listMarkerRe.MatchString(line) {
				itemStart = offset
			}
		} else if listMarkerRe.MatchString(line) {
			out = append(out, chunkSpan{start: b.start + itemStart, end: b.start + offset})
			itemStart = offset
		}
		if nl < 0 {
			break
		}
		offset = next
	}
	if itemStart >= 0 {
		out = append(out, chunkSpan{start: b.start + itemStart, end: b.start + len(text)})
	}
	return out
}

// chunkSpan is a byte range [start, end) within a parent string.
type chunkSpan struct {
	start, end int
}
