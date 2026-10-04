/**
 * Incremental SSE frame parser (docs/04 §11).
 *
 * The stream is a sequence of frames separated by blank lines. Each frame
 * carries `event: <name>` and `data: <json>` lines; `: ping` comment lines
 * are keep-alives and MUST be ignored. Chunks from the ReadableStream can
 * split a frame anywhere (including mid-line), so all bytes accumulate into
 * a line buffer and only complete frames are dispatched.
 */

export interface SseEvent {
  event: string
  /** Parsed JSON payload of the `data:` line. */
  data: unknown
}

export interface SseParser {
  /** Feed one raw chunk (any split point). Dispatches complete frames. */
  push(chunk: string): void
  /** Flush a trailing frame that was not terminated by a blank line. */
  flush(): void
}

/** Envelope carried inside every `data:` JSON line (docs/04 §11). */
export interface SseDataEnvelope {
  generation_id?: string
  seq?: number
  data?: Record<string, unknown>
}

export function createSseParser(onEvent: (event: SseEvent) => void): SseParser {
  let buffer = ''
  let eventName = ''
  let dataLines: string[] = []

  function dispatch(): void {
    if (eventName.length === 0 && dataLines.length === 0) return
    const raw = dataLines.join('\n')
    let data: unknown = undefined
    if (raw.length > 0) {
      try {
        data = JSON.parse(raw)
      } catch {
        // Malformed JSON: drop the frame entirely rather than dispatching an
        // event with an untrustworthy payload or killing the stream.
        reset()
        return
      }
    }
    const event = eventName.length > 0 ? eventName : 'message'
    reset()
    onEvent({ event, data })
  }

  function reset(): void {
    eventName = ''
    dataLines = []
  }

  function processLine(rawLine: string): void {
    // Normalise CRLF — the backend may emit \r\n line endings.
    const line = rawLine.endsWith('\r') ? rawLine.slice(0, -1) : rawLine
    if (line.length === 0) {
      // Blank line: end of frame.
      dispatch()
      return
    }
    if (line.startsWith(':')) return // keep-alive comment (`: ping`)
    const colon = line.indexOf(':')
    const field = colon === -1 ? line : line.slice(0, colon)
    let value = colon === -1 ? '' : line.slice(colon + 1)
    if (value.startsWith(' ')) value = value.slice(1)
    if (field === 'event') {
      eventName = value
    } else if (field === 'data') {
      dataLines.push(value)
    }
    // retry:/id: fields are not used by this contract and are ignored.
  }

  return {
    push(chunk: string): void {
      buffer += chunk
      let newlineIndex: number
      while ((newlineIndex = buffer.indexOf('\n')) !== -1) {
        const line = buffer.slice(0, newlineIndex)
        buffer = buffer.slice(newlineIndex + 1)
        processLine(line)
      }
    },
    flush(): void {
      if (buffer.length > 0) {
        const line = buffer
        buffer = ''
        processLine(line)
      }
      dispatch()
    },
  }
}
