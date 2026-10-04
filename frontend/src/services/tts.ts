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
  // Prefer an installed voice matching the requested accent, then any
  // English voice; when none is installed the lang tag still steers the
  // WebView2 default voice.
  const voices = synth.getVoices()
  const voice =
    voices.find((candidate) => candidate.lang.replace('_', '-') === accent) ??
    voices.find((candidate) => candidate.lang.replace('_', '-').startsWith(accent.slice(0, 2)))
  if (voice) utterance.voice = voice
  utterance.rate = 1.0
  synth.speak(utterance)
}
