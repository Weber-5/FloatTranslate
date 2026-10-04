/**
 * Deterministic in-memory word data for Mock mode and the client-side
 * lemmatizer. Pure data + pure helpers — no API or Vue imports.
 *
 * The "suspended" entry matches the frozen contract byte-for-byte:
 *   WordTranslation { word: "suspended", lemma: "suspend",
 *     phonetic_uk: "səˈspendɪd", phonetic_us: "səˈspendɪd", ... }
 */
import type { WordTranslation } from '@/api/types'

interface WordEntry {
  phonetic_uk: string
  phonetic_us: string
  parts_of_speech: { part: string; meanings: string[] }[]
  synonyms: string[]
  inflections: string[]
}

export const WORD_ENTRIES: Record<string, WordEntry> = {
  suspend: {
    phonetic_uk: 'səˈspend',
    phonetic_us: 'səˈspend',
    parts_of_speech: [
      { part: 'verb', meanings: ['暂停；中止', '悬挂；悬浮'] },
      { part: 'adjective', meanings: ['暂停的；中止的'] },
    ],
    synonyms: ['paused', 'halted', 'deferred'],
    inflections: ['suspend', 'suspends', 'suspending', 'suspended'],
  },
  run: {
    phonetic_uk: 'rʌn',
    phonetic_us: 'rʌn',
    parts_of_speech: [
      { part: 'verb', meanings: ['跑；奔跑', '运行；运转', '经营；管理'] },
      { part: 'noun', meanings: ['跑步；奔跑', '一段路程'] },
    ],
    synonyms: ['jog', 'sprint', 'dash'],
    inflections: ['run', 'runs', 'running', 'ran'],
  },
  pause: {
    phonetic_uk: 'pɔːz',
    phonetic_us: 'pɑːz',
    parts_of_speech: [
      { part: 'verb', meanings: ['暂停；停顿', '中止'] },
      { part: 'noun', meanings: ['暂停；停顿'] },
    ],
    synonyms: ['stop', 'halt', 'break'],
    inflections: ['pause', 'pauses', 'paused', 'pausing'],
  },
  quick: {
    phonetic_uk: 'kwɪk',
    phonetic_us: 'kwɪk',
    parts_of_speech: [
      { part: 'adjective', meanings: ['快的；迅速的', '机敏的；敏锐的'] },
      { part: 'adverb', meanings: ['快速地'] },
    ],
    synonyms: ['fast', 'rapid', 'swift'],
    inflections: ['quick', 'quicker', 'quickest'],
  },
  fox: {
    phonetic_uk: 'fɒks',
    phonetic_us: 'fɑːks',
    parts_of_speech: [
      { part: 'noun', meanings: ['狐狸', '狡猾的人'] },
      { part: 'verb', meanings: ['欺骗；使迷惑'] },
    ],
    synonyms: [],
    inflections: ['fox', 'foxes', 'foxed', 'foxing'],
  },
  jump: {
    phonetic_uk: 'dʒʌmp',
    phonetic_us: 'dʒʌmp',
    parts_of_speech: [
      { part: 'verb', meanings: ['跳；跳跃', '暴涨；猛增'] },
      { part: 'noun', meanings: ['跳跃；跃起'] },
    ],
    synonyms: ['leap', 'hop', 'bound'],
    inflections: ['jump', 'jumps', 'jumping', 'jumped'],
  },
  lazy: {
    phonetic_uk: 'ˈleɪzi',
    phonetic_us: 'ˈleɪzi',
    parts_of_speech: [{ part: 'adjective', meanings: ['懒惰的；懒散的', '悠闲的；慢悠悠的'] }],
    synonyms: ['idle', 'sluggish', 'indolent'],
    inflections: ['lazy', 'lazier', 'laziest'],
  },
  dog: {
    phonetic_uk: 'dɒɡ',
    phonetic_us: 'dɔːɡ',
    parts_of_speech: [
      { part: 'noun', meanings: ['狗；犬', '家伙（口语）'] },
      { part: 'verb', meanings: ['跟踪；尾随'] },
    ],
    synonyms: ['hound', 'pup', 'canine'],
    inflections: ['dog', 'dogs', 'dogged', 'dogging'],
  },
  hello: {
    phonetic_uk: 'həˈləʊ',
    phonetic_us: 'həˈloʊ',
    parts_of_speech: [
      { part: 'interjection', meanings: ['你好；哈喽'] },
      { part: 'noun', meanings: ['问候；招呼'] },
    ],
    synonyms: ['hi', 'greetings'],
    inflections: ['hello', 'hellos'],
  },
  world: {
    phonetic_uk: 'wɜːld',
    phonetic_us: 'wɜːrld',
    parts_of_speech: [{ part: 'noun', meanings: ['世界；天下', '领域；界'] }],
    synonyms: ['earth', 'globe', 'planet'],
    inflections: ['world', 'worlds'],
  },
  time: {
    phonetic_uk: 'taɪm',
    phonetic_us: 'taɪm',
    parts_of_speech: [
      { part: 'noun', meanings: ['时间；时候', '次；回', '时代；时期'] },
      { part: 'verb', meanings: ['计时；安排时间'] },
    ],
    synonyms: ['moment', 'hour', 'period'],
    inflections: ['time', 'times', 'timed', 'timing'],
  },
  book: {
    phonetic_uk: 'bʊk',
    phonetic_us: 'bʊk',
    parts_of_speech: [
      { part: 'noun', meanings: ['书；书籍', '账簿'] },
      { part: 'verb', meanings: ['预订；预约'] },
    ],
    synonyms: ['volume', 'text', 'tome'],
    inflections: ['book', 'books', 'booked', 'booking'],
  },
  read: {
    phonetic_uk: 'riːd',
    phonetic_us: 'riːd',
    parts_of_speech: [
      { part: 'verb', meanings: ['阅读；朗读', '读到；判读'] },
      { part: 'noun', meanings: ['阅读；读物'] },
    ],
    synonyms: ['scan', 'peruse', 'skim'],
    inflections: ['read', 'reads', 'reading'],
  },
  friend: {
    phonetic_uk: 'frend',
    phonetic_us: 'frend',
    parts_of_speech: [{ part: 'noun', meanings: ['朋友；友人', '支持者'] }],
    synonyms: ['buddy', 'pal', 'companion'],
    inflections: ['friend', 'friends', 'friended', 'friending'],
  },
}

/**
 * Mock glossary (~40 common words) used for deterministic text translation:
 * every known token is replaced by its Chinese gloss, unknown tokens stay.
 */
export const GLOSS: ReadonlyMap<string, string> = new Map([
  ['the', '这'],
  ['a', '一'],
  ['an', '一'],
  ['is', '是'],
  ['are', '是'],
  ['was', '是'],
  ['were', '是'],
  ['be', '是'],
  ['and', '和'],
  ['or', '或'],
  ['of', '的'],
  ['to', '到'],
  ['in', '在…里'],
  ['on', '在…上'],
  ['with', '和…一起'],
  ['for', '为了'],
  ['it', '它'],
  ['this', '这'],
  ['that', '那'],
  ['over', '在…上方'],
  ['hello', '你好'],
  ['world', '世界'],
  ['time', '时间'],
  ['good', '好的'],
  ['run', '跑'],
  ['suspend', '暂停'],
  ['pause', '暂停'],
  ['quick', '快速的'],
  ['brown', '棕色的'],
  ['fox', '狐狸'],
  ['jump', '跳'],
  ['lazy', '懒惰的'],
  ['dog', '狗'],
  ['book', '书'],
  ['read', '阅读'],
  ['write', '写'],
  ['friend', '朋友'],
  ['work', '工作'],
  ['study', '学习'],
  ['light', '光'],
  ['water', '水'],
  ['music', '音乐'],
  ['day', '天'],
  ['night', '夜晚'],
  ['make', '制作'],
  ['people', '人们'],
])

/** surface form (lowercase) -> lemma, built from every entry + inflection. */
export const SURFACE_TO_LEMMA: ReadonlyMap<string, string> = (() => {
  const map = new Map<string, string>()
  for (const [lemma, entry] of Object.entries(WORD_ENTRIES)) {
    map.set(lemma.toLowerCase(), lemma)
    for (const inflection of entry.inflections) map.set(inflection.toLowerCase(), lemma)
  }
  for (const lemma of GLOSS.keys()) {
    if (!map.has(lemma)) map.set(lemma, lemma)
  }
  return map
})()

function derivePhonetic(base: string, surface: string, lemma: string): string {
  if (!base || surface === lemma) return base
  if (surface === `${lemma}ed`) return `${base}ɪd`
  if (surface === `${lemma}ing`) return `${base}ɪŋ`
  if (surface === `${lemma}s`) return `${base}s`
  return base
}

/**
 * Build a full WordTranslation for an arbitrary surface form.
 * Known words come from WORD_ENTRIES (with derived inflection phonetics);
 * unknown words fall back to a deterministic generic entry.
 */
export function buildWordTranslation(surface: string): WordTranslation {
  const trimmed = surface.trim()
  const lower = trimmed.toLowerCase()
  const lemma = SURFACE_TO_LEMMA.get(lower) ?? lower
  const entry = WORD_ENTRIES[lemma]
  if (entry) {
    return {
      word: trimmed,
      lemma,
      phonetic_uk: derivePhonetic(entry.phonetic_uk, lower, lemma),
      phonetic_us: derivePhonetic(entry.phonetic_us, lower, lemma),
      parts_of_speech: entry.parts_of_speech,
      synonyms: [...entry.synonyms],
      inflections: [...entry.inflections],
    }
  }
  const gloss = GLOSS.get(lower)
  return {
    word: trimmed,
    lemma,
    phonetic_uk: '',
    phonetic_us: '',
    parts_of_speech: [
      { part: 'other', meanings: gloss ? [gloss] : ['（Mock 本地词库暂未收录该词）'] },
    ],
    synonyms: [],
    inflections: [lemma],
  }
}
