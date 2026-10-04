import { describe, expect, it, vi, afterEach } from 'vitest'
import {
  ApiError,
  BACKEND_UNAVAILABLE,
  BACKEND_UNAVAILABLE_MESSAGE,
  createRealClient,
  probeBackendHealth,
  type BackendConfig,
} from '@/api'

const config: BackendConfig = {
  // The Tauri-provided base_url includes the /api/v1 prefix (openapi servers url).
  base_url: 'http://127.0.0.1:8123/api/v1',
  token: 'session-token-1',
  data_root: 'C:\\data',
}

function jsonResponse(status: number, body: unknown, statusText = ''): Response {
  return new Response(JSON.stringify(body), {
    status,
    statusText,
    headers: { 'Content-Type': 'application/json' },
  })
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('real fetch client', () => {
  it('maps a 400 error envelope to a typed ApiError (code, message, retryable)', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        jsonResponse(400, {
          error: {
            code: 'UNSUPPORTED_LANGUAGE',
            message: 'FloatTranslate 1.0 暂仅支持英译中',
            retryable: false,
          },
        }),
      ),
    )
    const client = createRealClient(config)
    const err = await client.createTranslation('你好').catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    const apiError = err as ApiError
    expect(apiError.code).toBe('UNSUPPORTED_LANGUAGE')
    expect(apiError.message).toBe('FloatTranslate 1.0 暂仅支持英译中')
    expect(apiError.retryable).toBe(false)
  })

  it('maps network failures to BACKEND_UNAVAILABLE (retryable)', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => {
      throw new TypeError('fetch failed')
    }))
    const client = createRealClient(config)
    const err = (await client.getSettings().catch((e: unknown) => e)) as ApiError
    expect(err).toBeInstanceOf(ApiError)
    expect(err.code).toBe(BACKEND_UNAVAILABLE)
    expect(err.retryable).toBe(true)
    expect(err.message).toBe(BACKEND_UNAVAILABLE_MESSAGE)
  })

  it('maps non-JSON error responses (backend down) to BACKEND_UNAVAILABLE', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response('<html>502 Bad Gateway</html>', { status: 502 })),
    )
    const client = createRealClient(config)
    const err = (await client.listHistory().catch((e: unknown) => e)) as ApiError
    expect(err.code).toBe(BACKEND_UNAVAILABLE)
    expect(err.retryable).toBe(true)
  })

  it('sends the bearer token on business calls', async () => {
    let capturedUrl = ''
    let capturedAuth: string | undefined
    let capturedStatus: Response | null = null
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        capturedUrl = String(input)
        capturedAuth = (init?.headers as Record<string, string>).Authorization
        capturedStatus = jsonResponse(200, { supports_thinking: true })
        return capturedStatus
      }),
    )
    const client = createRealClient(config)
    const caps = await client.getRuntimeCapabilities()
    expect(caps.supports_thinking).toBe(true)
    expect(capturedUrl).toBe('http://127.0.0.1:8123/api/v1/runtime/capabilities')
    expect(capturedAuth).toBe('Bearer session-token-1')
  })

  it('round-trips the opaque history cursor verbatim as a query param', async () => {
    let requestedUrl = ''
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        requestedUrl = String(input)
        return jsonResponse(200, { items: [], next_cursor: null })
      }),
    )
    const client = createRealClient(config)
    const cursor = 'MTcwMDAwMDAwMHx0cmUtMDAx'
    await client.listHistory({ cursor, limit: 50 })
    expect(requestedUrl).toContain(`cursor=${encodeURIComponent(cursor)}`)
  })

  it('POSTs the submitted provider config to /settings/provider/test', async () => {
    let capturedBody: unknown = null
    let capturedUrl = ''
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        capturedUrl = String(input)
        capturedBody = JSON.parse(String(init?.body ?? '{}'))
        return jsonResponse(200, { ok: true, message: 'pong' })
      }),
    )
    const client = createRealClient(config)
    const result = await client.testProviderConnection({
      mode: 'openai_compatible',
      base_url: 'https://api.example.com',
      translation_model: 'm1',
      chat_model: 'm2',
      api_key: 'sk-live',
    })
    expect(result.ok).toBe(true)
    expect(capturedUrl).toContain('/settings/provider/test')
    expect(capturedBody).toEqual({
      mode: 'openai_compatible',
      base_url: 'https://api.example.com',
      translation_model: 'm1',
      chat_model: 'm2',
      api_key: 'sk-live',
    })
  })

  it('omits api_key from the test body when the input is empty', async () => {
    let capturedBody: Record<string, unknown> = {}
    vi.stubGlobal(
      'fetch',
      vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
        capturedBody = JSON.parse(String(init?.body ?? '{}'))
        return jsonResponse(200, { ok: true })
      }),
    )
    const client = createRealClient(config)
    await client.testProviderConnection({
      mode: 'deepseek',
      base_url: 'https://api.deepseek.com',
      translation_model: 'deepseek-flash',
      chat_model: 'deepseek-flash',
      api_key: '   ',
    })
    expect('api_key' in capturedBody).toBe(false)
  })
})

describe('backend health probe (/health, no auth)', () => {
  it('returns true when /health answers 200 and sends no Authorization header', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      // /health sits at the host root even when base_url ends with /api/v1.
      expect(String(input)).toBe('http://127.0.0.1:8123/health')
      expect(init?.headers).toBeUndefined()
      return new Response(JSON.stringify({ status: 'ok' }), { status: 200 })
    })
    vi.stubGlobal('fetch', fetchMock)
    await expect(probeBackendHealth(config.base_url)).resolves.toBe(true)
  })

  it('returns false when /health is unreachable or errors', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => {
      throw new TypeError('fetch failed')
    }))
    await expect(probeBackendHealth(config.base_url)).resolves.toBe(false)

    vi.stubGlobal('fetch', vi.fn(async () => new Response('nope', { status: 500 })))
    await expect(probeBackendHealth(config.base_url)).resolves.toBe(false)
  })
})
