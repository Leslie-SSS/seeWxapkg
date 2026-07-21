import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiClient } from './client'

describe('ApiClient.compile', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('surfaces 413 responses with a specific upload size error', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response('<html><h1>413 Request Entity Too Large</h1></html>', {
          status: 413,
          headers: {
            'content-type': 'text/html',
          },
        })
      )
    )

    const client = new ApiClient('/api')

    await expect(
      client.compile({
        file: new File(['payload'], '__APP__.wxapkg'),
      })
    ).rejects.toThrow('上传失败：文件过大，超过服务允许的上传大小')
  })

  it('surfaces backend JSON error messages', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ message: '文件必须是 .wxapkg 格式' }), {
          status: 400,
          headers: {
            'content-type': 'application/json',
          },
        })
      )
    )

    const client = new ApiClient('/api')

    await expect(
      client.compile({
        file: new File(['payload'], 'bad.txt'),
      })
    ).rejects.toThrow('文件必须是 .wxapkg 格式')
  })

  it('surfaces network failures as connectivity errors', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')))

    const client = new ApiClient('/api')

    await expect(
      client.compile({
        file: new File(['payload'], '__APP__.wxapkg'),
      })
    ).rejects.toThrow('网络异常，无法连接上传服务')
  })
})

describe('ApiClient.getGithubStars', () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('loads current star metadata from the same-origin API', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ stars: 321, stale: false }), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      })
    )
    vi.stubGlobal('fetch', fetchMock)

    const client = new ApiClient('/api')
    await expect(client.getGithubStars()).resolves.toEqual({ stars: 321, stale: false })

    expect(fetchMock).toHaveBeenCalledOnce()
    expect(fetchMock.mock.calls[0][0]).toBe('/api/github/stars')
    expect(fetchMock.mock.calls[0][1].signal).toBeInstanceOf(AbortSignal)
  })

  it('rejects invalid star counts instead of displaying untrusted metadata', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ stars: -1, stale: false }), {
          status: 200,
          headers: { 'content-type': 'application/json' },
        })
      )
    )

    const client = new ApiClient('/api')
    await expect(client.getGithubStars()).rejects.toThrow('GitHub 数据暂时无法加载')
  })

  it('rejects metadata that does not truthfully declare cache freshness', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ stars: 12, stale: 'false' }), {
          status: 200,
          headers: { 'content-type': 'application/json' },
        })
      )
    )

    const client = new ApiClient('/api')
    await expect(client.getGithubStars()).rejects.toThrow('GitHub 数据暂时无法加载')
  })

  it('stops waiting for GitHub metadata after four seconds', async () => {
    vi.useFakeTimers()
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation((_input: RequestInfo | URL, init?: RequestInit) => {
        return new Promise<Response>((_resolve, reject) => {
          init?.signal?.addEventListener('abort', () => {
            reject(new DOMException('aborted', 'AbortError'))
          })
        })
      })
    )

    const client = new ApiClient('/api')
    const rejection = expect(client.getGithubStars()).rejects.toThrow('请求超时')

    await vi.advanceTimersByTimeAsync(4_000)
    await rejection
  })
})

describe('ApiClient.subscribeProgress', () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('falls back to persisted task polling when SSE disconnects', async () => {
    vi.useFakeTimers()
    let lastSource: MockEventSource | undefined
    class MockEventSource {
      onmessage: ((event: MessageEvent) => void) | null = null
      onerror: (() => void) | null = null
      close = vi.fn()

      constructor(_url: string) {
        lastSource = this
      }
    }
    vi.stubGlobal('EventSource', MockEventSource)
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            id: 'task-1',
            status: 'completed',
            progress: 100,
            diagnosticsCount: 0,
            artifacts: { downloadUrl: '/api/download/task-1', fileCount: 4 },
          }),
          { status: 200, headers: { 'content-type': 'application/json' } }
        )
      )
    )

    const events: Array<{ type: string; taskId?: string }> = []
    const client = new ApiClient('/api')
    const unsubscribe = client.subscribeProgress('task-1', (event) => events.push(event))

    lastSource?.onerror?.()
    await vi.runAllTimersAsync()

    expect(events).toHaveLength(1)
    expect(events[0]).toMatchObject({ type: 'complete', taskId: 'task-1' })
    expect(lastSource?.close).toHaveBeenCalled()
    unsubscribe()
  })

  it('reports repeated polling failures and clears the notice when polling recovers', async () => {
    vi.useFakeTimers()
    let lastSource: MockEventSource | undefined
    class MockEventSource {
      onmessage: ((event: MessageEvent) => void) | null = null
      onerror: (() => void) | null = null
      close = vi.fn()

      constructor(_url: string) {
        lastSource = this
      }
    }
    vi.stubGlobal('EventSource', MockEventSource)

    const fetchMock = vi
      .fn()
      .mockRejectedValueOnce(new TypeError('offline'))
      .mockRejectedValueOnce(new TypeError('offline'))
      .mockRejectedValueOnce(new TypeError('offline'))
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            id: 'task-1',
            status: 'recovering_js',
            progress: 45,
            currentMessage: '处理中',
            diagnosticsCount: 0,
          }),
          { status: 200, headers: { 'content-type': 'application/json' } }
        )
      )
    vi.stubGlobal('fetch', fetchMock)

    const events: Array<{ type: string; taskId?: string }> = []
    const connectionStates: string[] = []
    const client = new ApiClient('/api')
    const unsubscribe = client.subscribeProgress(
      'task-1',
      (event) => events.push(event),
      (state) => connectionStates.push(state)
    )

    lastSource?.onerror?.()
    await vi.advanceTimersByTimeAsync(0)
    expect(connectionStates).toEqual([])

    await vi.advanceTimersByTimeAsync(1_000)
    expect(connectionStates).toEqual([])

    await vi.advanceTimersByTimeAsync(1_000)
    expect(connectionStates).toEqual(['interrupted'])
    expect(events).toEqual([])

    await vi.advanceTimersByTimeAsync(1_000)
    expect(connectionStates).toEqual(['interrupted', 'restored'])
    expect(events[0]).toMatchObject({ type: 'progress', taskId: 'task-1', percent: 45 })

    unsubscribe()
  })

  it('uses polling when the browser cannot create an event stream', async () => {
    vi.useFakeTimers()
    class UnsupportedEventSource {
      constructor(_url: string) {
        throw new Error('EventSource unavailable')
      }
    }
    vi.stubGlobal('EventSource', UnsupportedEventSource)
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            id: 'task-1',
            status: 'completed',
            progress: 100,
            diagnosticsCount: 0,
          }),
          { status: 200, headers: { 'content-type': 'application/json' } }
        )
      )
    )

    const events: Array<{ type: string; taskId?: string }> = []
    const client = new ApiClient('/api')
    const unsubscribe = client.subscribeProgress('task-1', (event) => events.push(event))

    await vi.advanceTimersByTimeAsync(0)

    expect(events[0]).toMatchObject({ type: 'complete', taskId: 'task-1' })
    unsubscribe()
  })

  it('does not emit a polling result that resolves after unsubscribe', async () => {
    vi.useFakeTimers()
    let lastSource: MockEventSource | undefined
    let resolveFetch!: (response: Response) => void
    class MockEventSource {
      onmessage: ((event: MessageEvent) => void) | null = null
      onerror: (() => void) | null = null
      close = vi.fn()

      constructor(_url: string) {
        lastSource = this
      }
    }
    vi.stubGlobal('EventSource', MockEventSource)
    vi.stubGlobal(
      'fetch',
      vi.fn().mockReturnValue(
        new Promise<Response>((resolve) => {
          resolveFetch = resolve
        })
      )
    )

    const events: Array<{ type: string }> = []
    const client = new ApiClient('/api')
    const unsubscribe = client.subscribeProgress('task-1', (event) => events.push(event))

    lastSource?.onerror?.()
    await vi.advanceTimersByTimeAsync(0)
    unsubscribe()
    resolveFetch(
      new Response(
        JSON.stringify({
          id: 'task-1',
          status: 'completed',
          progress: 100,
          diagnosticsCount: 0,
        }),
        { status: 200, headers: { 'content-type': 'application/json' } }
      )
    )
    await Promise.resolve()
    await Promise.resolve()

    expect(events).toEqual([])
  })

  it('times out task detail requests instead of waiting forever', async () => {
    vi.useFakeTimers()
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation((_input: RequestInfo | URL, init?: RequestInit) => {
        return new Promise<Response>((_resolve, reject) => {
          init?.signal?.addEventListener('abort', () => {
            reject(new DOMException('aborted', 'AbortError'))
          })
        })
      })
    )

    const client = new ApiClient('/api')
    const rejection = expect(client.getTask('task-1')).rejects.toThrow('请求超时')

    await vi.advanceTimersByTimeAsync(10_000)
    await rejection
  })
})
