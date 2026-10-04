// Package schemas holds embedded copies of the FloatTranslate LLM structured
// output JSON Schemas (schemas/word_translation.schema.json and
// schemas/text_translation.schema.json from the repository root).
package schemas

import _ "embed"

//go:embed word_translation.schema.json
var wordTranslationSchema []byte

//go:embed text_translation.schema.json
var textTranslationSchema []byte

// WordTranslation returns the embedded word translation schema JSON.
func WordTranslation() string { return string(wordTranslationSchema) }

// TextTranslation returns the embedded text translation schema JSON.
func TextTranslation() string { return string(textTranslationSchema) }
