/**
 * Paragraph tokenizer for the Text result view (docs/00 §4).
 *
 * English word tokens become clickable; punctuation, numbers, URLs and
 * `code spans` are protected and keep their plain layout.
 */

export type TokenKind = 'word' | 'protected' | 'other'

export interface TextToken {
  text: string
  kind: TokenKind
}

const TOKEN_PATTERN = /(https?:\/\/[^\s`]+|`[^`]+`|[A-Za-z][A-Za-z'’-]*|\s+|[^\sA-Za-z`]+)/g

export function isWordToken(text: string): boolean {
  return /^[A-Za-z][A-Za-z'’-]*$/.test(text)
}

export function tokenizeParagraph(source: string): TextToken[] {
  const tokens: TextToken[] = []
  for (const match of source.matchAll(TOKEN_PATTERN)) {
    const text = match[0]
    let kind: TokenKind = 'other'
    if (text.startsWith('http') || text.startsWith('`')) kind = 'protected'
    else if (isWordToken(text)) kind = 'word'
    tokens.push({ text, kind })
  }
  return tokens
}
