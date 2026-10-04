// Package schemas holds embedded copies of the FloatTranslate LLM structured
// output JSON Schemas (schemas/word_translation.schema.json and
// schemas/text_translation.schema.json from the repository root) plus the
// backend-internal per-chunk text schema introduced by Phase 3 long text
// chunking (text_chunk.schema.json has no repository-root contract copy).
package schemas

import _ "embed"

//go:embed word_translation.schema.json
var wordTranslationSchema []byte

//go:embed text_translation.schema.json
var textTranslationSchema []byte

//go:embed text_chunk.schema.json
var textChunkSchema []byte

// WordTranslation returns the embedded word translation schema JSON.
func WordTranslation() string { return string(wordTranslationSchema) }

// TextTranslation returns the embedded text translation schema JSON.
func TextTranslation() string { return string(textTranslationSchema) }

// TextChunk returns the embedded per-chunk text translation schema JSON
// (Phase 3: {"translated_markdown", "segments"} for a single chunk).
func TextChunk() string { return string(textChunkSchema) }
