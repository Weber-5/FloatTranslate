import { describe, expect, it } from 'vitest'
import { createSseParser, type SseEvent } from '@/api/sse'

function collect(): { events: SseEvent[]; push: (chunk: string) => void; flush: () => void } {
  const events: SseEvent[] = []
  const parser = createSseParser((event) => events.push(event))
  return { events, push: (chunk) => parser.push(chunk), flush: () => parser.flush() }
}

function frame(event: string, payload: Record<string, unknown>): string {
  return `event: ${event}\ndata: ${JSON.stringify({ generation_id: 'gen-1', seq: 1, data: payload })}\n\n`
}

describe('SSE parser (docs/04 §11)', () => {
  it('dispatches a complete frame from a single chunk', () => {
    const { events, push } = collect()
    push(frame('generation.started', { chat_id: 'c1', model: 'm', thinking: true }))
    expect(events).toHaveLength(1)
    expect(events[0].event).toBe('generation.started')
    const envelope = events[0].data as { generation_id: string; data: Record<string, unknown> }
    expect(envelope.generation_id).toBe('gen-1')
    expect(envelope.data.chat_id).toBe('c1')
  })

  it('reassembles frames split mid-line across chunk boundaries', () => {
    const { events, push } = collect()
    const raw = frame('content.delta', { text: 'hello' })
    // Feed one byte at a time — the frame must only dispatch once complete.
    for (const char of raw) push(char)
    expect(events).toHaveLength(1)
    expect(events[0].event).toBe('content.delta')
    expect((events[0].data as { data: { text: string } }).data.text).toBe('hello')
  })

  it('handles a chunk boundary exactly at the newline', () => {
    const { events, push } = collect()
    const raw = frame('reasoning.delta', { text: 'th' })
    const split = raw.indexOf('\n') + 1
    push(raw.slice(0, split))
    expect(events).toHaveLength(0)
    push(raw.slice(split))
    expect(events).toHaveLength(1)
    expect(events[0].event).toBe('reasoning.delta')
  })

  it('ignores `: ping` keep-alive comments', () => {
    const { events, push, flush } = collect()
    push(': ping\n\n')
    expect(events).toHaveLength(0)
    push(frame('generation.started', {}))
    expect(events).toHaveLength(1)
    flush()
    expect(events).toHaveLength(1)
  })

  it('dispatches every contract event type', () => {
    const { events, push } = collect()
    push(frame('generation.started', { chat_id: 'c1' }))
    push(frame('reasoning.delta', { text: 'a' }))
    push(frame('content.delta', { text: 'b' }))
    push(
      frame('generation.completed', {
        message_id: 'm1',
        reasoning_content: 'a',
        content: 'b',
        finish_reason: 'stop',
      }),
    )
    push(frame('generation.error', { code: 'X', message: 'boom', retryable: true }))
    push(frame('generation.cancelled', {}))
    expect(events.map((event) => event.event)).toEqual([
      'generation.started',
      'reasoning.delta',
      'content.delta',
      'generation.completed',
      'generation.error',
      'generation.cancelled',
    ])
  })

  it('keeps parsing after a malformed data JSON line', () => {
    const { events, push } = collect()
    push('event: content.delta\ndata: {not json}\n\n')
    expect(events).toHaveLength(0)
    push(frame('content.delta', { text: 'ok' }))
    expect(events).toHaveLength(1)
    expect((events[0].data as { data: { text: string } }).data.text).toBe('ok')
  })

  it('flushes a trailing frame without a final blank line', () => {
    const { events, push, flush } = collect()
    push('event: generation.cancelled\ndata: {"generation_id":"gen-9","seq":2,"data":{}}')
    expect(events).toHaveLength(0)
    flush()
    expect(events).toHaveLength(1)
    expect(events[0].event).toBe('generation.cancelled')
  })

  it('tolerates CRLF line endings', () => {
    const { events, push } = collect()
    push(`event: generation.started\r\ndata: {"generation_id":"g","seq":1,"data":{}}\r\n\r\n`)
    expect(events).toHaveLength(1)
    expect(events[0].event).toBe('generation.started')
  })
})
