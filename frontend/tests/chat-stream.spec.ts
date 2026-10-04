/**
 * Real-client SSE streaming (docs/04 §11): POST fetch + ReadableStream
 * parsing, event dispatch, transport errors and the cancel endpoint.
 */
import { describe, expect, it, vi, afterEach } from 'vitest'
import { ApiError, createRealClient, type BackendConfig } from '@/api'

const config: BackendConfig = {
  base_url: 'http://127.0.0.1:8123/api/v1',
  token: 'session-token-1',
  data_root: 'C:\\data',
}

/** Builds a Response whose body streams `whole` in small chunks. */
function sseResponse(whole: string, chunkSize = 5, hang = false): Response {
  const encoder = new TextEncoder()
  let offset = 0
  const stream = new ReadableStream<Uint8Array>({
    pull(controller) {
      if (offset >= whole.length) {
        if (!hang) controller.close()
        return
      }
      const slice = whole.slice(offset, offset + chunkSize)
      offset += chunkSize
      controller.enqueue(encoder.encode(slice))
    },
  })
  return new Response(stream, {
    status: 200,
    headers: { 'Content-Type': 'text/event-stream' },
  })
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('real SSE stream client', () => {
  it('POSTs the generation request with the bearer token and dispatches the full happy path', async () => {
    const frames =
      'event: generation.started\n' +
      'data: {"generation_id":"gen-1","seq":0,"data":{"chat_id":"c1","model":"m","thinking":true}}\n\n' +
      ': ping\n\n' +
      'event: reasoning.delta\n' +
      'data: {"generation_id":"gen-1","seq":1,"data":{"text":"th"}}\n\n' +
      'event: content.delta\n' +
      'data: {"generation_id":"gen-1","seq":2,"data":{"text":"he"}}\n\n' +
      'event: generation.completed\n' +
      'data: {"generation_id":"gen-1","seq":3,"data":{"message_id":"m1","reasoning_content":"th","content":"he","finish_reason":"stop"}}\n\n'

    const calls: { url: string; init?: RequestInit }[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        calls.push({ url: String(input), init })
        return sseResponse(frames)
      }),
    )

    const client = createRealClient(config)
    const events: string[] = []
    const handle = await client.streamChatGeneration(
      'c1',
      { content: 'hi', thinking: true },
      {
        onStarted: (id, info) => {
          events.push(`started:${id}:${info?.model}`)
        },
        onReasoningDelta: (delta) => events.push(`reasoning:${delta}`),
        onContentDelta: (delta) => events.push(`content:${delta}`),
        onCompleted: (id, data) => events.push(`completed:${id}:${data?.message_id}`),
        onCancelled: () => events.push('cancelled'),
        onError: () => events.push('error'),
      },
    )
    const outcome = await handle.promise

    expect(outcome).toBe('completed')
    expect(events).toEqual([
      'started:gen-1:m',
      'reasoning:th',
      'content:he',
      'completed:gen-1:m1',
    ])
    expect(calls).toHaveLength(1)
    expect(calls[0].url).toBe('http://127.0.0.1:8123/api/v1/chats/c1/generations')
    expect(calls[0].init?.method).toBe('POST')
    expect((calls[0].init?.headers as Record<string, string>).Authorization).toBe(
      'Bearer session-token-1',
    )
    expect(JSON.parse(String(calls[0].init?.body))).toEqual({ content: 'hi', thinking: true })
  })

  it('maps a mid-stream generation.error event to onError and resolves the outcome', async () => {
    const frames =
      'event: generation.started\n' +
      'data: {"generation_id":"gen-2","seq":0,"data":{}}\n\n' +
      'event: generation.error\n' +
      'data: {"generation_id":"gen-2","seq":1,"data":{"code":"PROVIDER_FAILED","message":"boom","retryable":true}}\n\n'

    vi.stubGlobal('fetch', vi.fn(async () => sseResponse(frames)))
    const client = createRealClient(config)
    const errors: ApiError[] = []
    const handle = await client.streamChatGeneration('c1', { content: 'hi' }, {
      onError: (error) => errors.push(error),
    })
    const outcome = await handle.promise
    expect(outcome).toBe('error')
    expect(errors).toHaveLength(1)
    expect(errors[0].code).toBe('PROVIDER_FAILED')
    expect(errors[0].retryable).toBe(true)
  })

  it('rejects before streaming when the backend answers 409 GENERATION_ALREADY_ACTIVE', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              error: { code: 'GENERATION_ALREADY_ACTIVE', message: 'busy', retryable: false },
            }),
            { status: 409, headers: { 'Content-Type': 'application/json' } },
          ),
      ),
    )
    const client = createRealClient(config)
    const err = await client
      .streamChatGeneration('c1', { content: 'hi' }, {})
      .catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).code).toBe('GENERATION_ALREADY_ACTIVE')
  })

  it('cancel() aborts the reader and calls the cancel endpoint with the generation id', async () => {
    const frames =
      'event: generation.started\n' +
      'data: {"generation_id":"gen-9","seq":0,"data":{}}\n\n' +
      // Stream then hangs open forever (no terminal event).
      'event: content.delta\n' +
      'data: {"generation_id":"gen-9","seq":1,"data":{"text":"partial"}}\n\n'
    const calls: string[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        calls.push(`${String(input)} ${String(init?.method)}`)
        if (String(input).endsWith('/cancel')) {
          return new Response(null, { status: 202 })
        }
        return sseResponse(frames, 5, true)
      }),
    )

    const client = createRealClient(config)
    let startedId = ''
    const handle = await client.streamChatGeneration('c1', { content: 'hi' }, {
      onStarted: (id) => {
        startedId = id
      },
    })
    await vi.waitFor(() => expect(startedId).toBe('gen-9'))
    handle.cancel()
    const outcome = await handle.promise
    expect(outcome).toBe('cancelled')
    expect(generationIdOf(handle)).toBe('gen-9')
    await vi.waitFor(() => {
      expect(calls.some((call) => call.includes('/chats/c1/generations/gen-9/cancel'))).toBe(true)
      expect(calls.some((call) => call.includes('POST'))).toBe(true)
    })
  })

  it('surfaces STREAM_CLOSED when the server ends the stream without a terminal event', async () => {
    const frames =
      'event: generation.started\n' +
      'data: {"generation_id":"gen-3","seq":0,"data":{}}\n\n' +
      'event: content.delta\n' +
      'data: {"generation_id":"gen-3","seq":1,"data":{"text":"half"}}\n\n'
    vi.stubGlobal('fetch', vi.fn(async () => sseResponse(frames)))
    const client = createRealClient(config)
    const errors: ApiError[] = []
    const handle = await client.streamChatGeneration('c1', { content: 'hi' }, {
      onError: (error) => errors.push(error),
    })
    const outcome = await handle.promise
    expect(outcome).toBe('error')
    expect(errors[0].code).toBe('STREAM_CLOSED')
  })

  it('regenerateChat POSTs /chats/{id}/regenerate and streams the replacement', async () => {
    const frames =
      'event: generation.started\n' +
      'data: {"generation_id":"gen-4","seq":0,"data":{}}\n\n' +
      'event: generation.completed\n' +
      'data: {"generation_id":"gen-4","seq":1,"data":{"message_id":"m2","content":"new","finish_reason":"stop"}}\n\n'
    let url = ''
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        url = String(input)
        return sseResponse(frames)
      }),
    )
    const client = createRealClient(config)
    let content = ''
    const handle = await client.regenerateChat('c1', {
      onCompleted: (_id, data) => {
        content = data?.content ?? ''
      },
    })
    expect(await handle.promise).toBe('completed')
    expect(url).toBe('http://127.0.0.1:8123/api/v1/chats/c1/regenerate')
    expect(content).toBe('new')
  })
})

function generationIdOf(handle: { generationId: string }): string {
  return handle.generationId
}
