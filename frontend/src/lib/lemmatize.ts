/**
 * Tiny client-side lemmatizer (docs/00 §4: "点击词后本地 lemmatization").
 * Strategy: known-surface map first, then a small suffix-rule cascade where
 * every candidate must hit the known-lemma set; otherwise return the
 * lowercased token unchanged.
 */
import { SURFACE_TO_LEMMA } from './wordData'

export function lemmatize(token: string): string {
  const lower = token.trim().toLowerCase()
  if (lower.length === 0) return lower

  // Known surface forms (lemma, inflection or glossary headword) map directly.
  const known = SURFACE_TO_LEMMA.get(lower)
  if (known) return known

  const knownLemmas = SURFACE_TO_LEMMA

  // Possessive: dog's -> dog
  if (lower.endsWith("'s") && knownLemmas.has(lower.slice(0, -2))) return lower.slice(0, -2)

  const candidates: string[] = []

  // Plural / 3rd person
  if (lower.endsWith('ies')) candidates.push(`${lower.slice(0, -3)}y`)
  if (lower.endsWith('es')) candidates.push(lower.slice(0, -2))
  if (lower.endsWith('s') && !lower.endsWith('ss')) candidates.push(lower.slice(0, -1))

  // Gerund / progressive: running -> runn -> run, making -> mak -> make
  if (lower.endsWith('ing')) {
    const stem = lower.slice(0, -3)
    candidates.push(stem, `${stem}e`)
    if (stem.length >= 3 && stem.at(-1) === stem.at(-2)) candidates.push(stem.slice(0, -1))
  }

  // Past: suspended -> suspend, loved -> love, shared -> share, stopped -> stop
  if (lower.endsWith('ed')) {
    const stem = lower.slice(0, -2)
    candidates.push(stem, `${stem}e`, lower.slice(0, -1))
    if (stem.length >= 3 && stem.at(-1) === stem.at(-2)) candidates.push(stem.slice(0, -1))
  }

  for (const candidate of candidates) {
    if (candidate.length > 1 && knownLemmas.has(candidate)) return candidate
  }
  return lower
}
