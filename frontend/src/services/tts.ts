/**
 * Local TTS wrapper (docs/00 §4: UK/US 发音调用本地 TTS，不消耗 LLM).
 */
export type SpeechAccent = 'en-GB' | 'en-US'

export function isTtsAvailable(): boolean {
  return typeof window !== 'undefined' && 'speechSynthesis' in window
}

export function speak(text: string, accent: SpeechAccent): void {
  if (!isTtsAvailable() || text.trim().length === 0) return
  const synth = window.speechSynthesis
  synth.cancel()
  const utterance = new SpeechSynthesisUtterance(text)
  utterance.lang = accent
  const voices = synth.getVoices()
  const voice =
    voices.find((candidate) => candidate.lang === accent) ??
    voices.find((candidate) => candidate.lang.replace('_', '-').startsWith(accent.slice(0, 2)))
  if (voice) utterance.voice = voice
  utterance.rate = 0.95
  synth.speak(utterance)
}
