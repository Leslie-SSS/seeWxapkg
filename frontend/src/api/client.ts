const API_BASE = '/api'

interface CompileRequest {
  file: File
  appId?: string
  beautify?: boolean
  decompile?: boolean
}

export interface ProgressEvent {
  type: 'progress' | 'complete' | 'partial' | 'error'
  stage: string
  percent: number
  message: string
  status?: string
  fileCount?: number
  taskId?: string
  downloadUrl?: string
  reportUrl?: string
  diagnosticsUrl?: string
  diagnosticsCount?: number
  errorCode?: string
  error?: string
}

export type ProgressConnectionState = 'interrupted' | 'restored'

const POLL_FAILURE_NOTICE_THRESHOLD = 3
const DETAIL_REQUEST_TIMEOUT_MS = 10_000
const PUBLIC_METADATA_TIMEOUT_MS = 4_000

interface CompileResponse {
  success: boolean
  taskId: string
  message: string
  downloadUrl?: string
}

export interface Diagnostic {
  code: string
  severity: 'info' | 'warn' | 'error'
  message: string
  file?: string
  stage?: string
  metadata?: Record<string, unknown>
}

export interface RecoveryScore {
  overall: number
  manifest: number
  js: number
  wxml: number
  wxss: number
  decompileHit: boolean
  fallbackUsed: boolean
  generatedRatio: number
  fallbackPenalty: number
  verifierPassed: boolean
}

export interface PackageProfile {
  isEncrypted: boolean
  isStandardWxapkg: boolean
  isWeChat4xLike: boolean
  isSubPackage: boolean
  isGamePackage: boolean
  hasAppConfigJSON: boolean
  hasAppServiceJS: boolean
  hasWorkersJS: boolean
  hasPageFrameHTML: boolean
  hasPageFrameJS: boolean
  hasAppWxssJS: boolean
  indexFileCount: number
  suspectedVariant: string
}

interface ArtifactFile {
  path: string
  kind: string
  source: string
}

interface ArtifactSummary {
  fileCount: number
  downloadUrl?: string
  reportUrl?: string
  diagnosticsUrl?: string
  artifactsUrl?: string
  packageProfileUrl?: string
  files?: ArtifactFile[]
  downloadReady?: boolean
  archiveSize?: number
  sourceBreakdown?: Record<string, number>
}

export interface StageResult {
  stage: string
  success: boolean
  partial: boolean
  status: string
  startedAt?: string
  finishedAt?: string
  durationMs?: number
  attempt?: number
  engine?: string
  sourceBreakdown?: Record<string, number>
  message?: string
  metrics?: Record<string, unknown>
  diagnostics?: Diagnostic[]
}

export interface TaskResponse {
  id: string
  status:
    | 'queued'
    | 'classifying'
    | 'decrypting'
    | 'unpacking'
    | 'normalizing'
    | 'recovering_manifest'
    | 'recovering_js'
    | 'recovering_wxml'
    | 'recovering_wxss'
    | 'fallback_recovering'
    | 'formatting'
    | 'verifying'
    | 'packaging'
    | 'completed'
    | 'partial'
    | 'failed'
  progress: number
  currentStage?: string
  currentMessage?: string
  profile?: PackageProfile
  stages?: StageResult[]
  score?: RecoveryScore
  artifacts?: ArtifactSummary
  reports?: Record<string, string>
  diagnosticsCount: number
  errorCode?: string
  errorMessage?: string
}

interface GithubStarsResponse {
  stars: number
  stale: boolean
}

export class ApiClient {
  private base: string

  constructor(base: string = API_BASE) {
    this.base = base
  }

  async compile(request: CompileRequest): Promise<CompileResponse> {
    const formData = new FormData()
    formData.append('file', request.file)
    if (request.appId) {
      formData.append('appId', request.appId)
    }
    if (request.beautify !== undefined) {
      formData.append('beautify', request.beautify.toString())
    }
    if (request.decompile !== undefined) {
      formData.append('decompile', request.decompile.toString())
    }

    let response: Response
    try {
      response = await fetch(`${this.base}/compile`, {
        method: 'POST',
        body: formData,
      })
    } catch {
      throw new Error('网络异常，无法连接上传服务')
    }

    if (!response.ok) {
      throw new Error(await extractErrorMessage(response))
    }

    return response.json()
  }

  subscribeProgress(
    taskId: string,
    onEvent: (event: ProgressEvent) => void,
    onConnectionStateChange?: (state: ProgressConnectionState) => void
  ): () => void {
    let eventSource: EventSource | null = null
    let pollTimer: ReturnType<typeof setTimeout> | null = null
    let stopped = false
    let pollingStarted = false
    let consecutivePollFailures = 0
    let interruptionReported = false
    let pollAbortController: AbortController | null = null

    const markConnectionRestored = () => {
      consecutivePollFailures = 0
      if (!interruptionReported) return

      interruptionReported = false
      onConnectionStateChange?.('restored')
    }

    const markPollFailure = () => {
      consecutivePollFailures += 1
      if (interruptionReported || consecutivePollFailures < POLL_FAILURE_NOTICE_THRESHOLD) {
        return
      }

      interruptionReported = true
      onConnectionStateChange?.('interrupted')
    }

    const stop = () => {
      stopped = true
      eventSource?.close()
      eventSource = null
      if (pollTimer !== null) {
        clearTimeout(pollTimer)
        pollTimer = null
      }
      pollAbortController?.abort()
      pollAbortController = null
    }

    const emitTask = (detail: TaskResponse) => {
      const terminal =
        detail.status === 'completed' || detail.status === 'partial' || detail.status === 'failed'
      const type: ProgressEvent['type'] =
        detail.status === 'completed'
          ? 'complete'
          : detail.status === 'partial'
            ? 'partial'
            : detail.status === 'failed'
              ? 'error'
              : 'progress'
      onEvent({
        type,
        taskId: detail.id,
        stage: detail.currentStage ?? detail.status,
        status: detail.status,
        percent: detail.progress,
        message: detail.currentMessage ?? detail.errorMessage ?? '',
        fileCount: detail.artifacts?.fileCount,
        downloadUrl: detail.artifacts?.downloadUrl,
        reportUrl: detail.artifacts?.reportUrl,
        diagnosticsUrl: detail.artifacts?.diagnosticsUrl,
        diagnosticsCount: detail.diagnosticsCount,
        errorCode: detail.errorCode,
        error: detail.errorMessage,
      })
      if (terminal) {
        stop()
      }
    }

    const poll = async () => {
      if (stopped) return
      const controller = new AbortController()
      pollAbortController = controller
      try {
        const detail = await this.getTask(taskId, controller.signal)
        if (stopped || controller.signal.aborted) return
        markConnectionRestored()
        emitTask(detail)
      } catch {
        if (stopped || controller.signal.aborted) return
        markPollFailure()
      } finally {
        if (pollAbortController === controller) {
          pollAbortController = null
        }
      }
      if (!stopped) {
        pollTimer = setTimeout(() => void poll(), 1000)
      }
    }

    const startPolling = () => {
      if (stopped || pollingStarted) return
      pollingStarted = true
      void poll()
    }

    const handleMessage = (e: MessageEvent) => {
      try {
        const event = JSON.parse(e.data) as ProgressEvent
        markConnectionRestored()
        onEvent(event)

        if (event.type === 'complete' || event.type === 'partial' || event.type === 'error') {
          stop()
        }
      } catch {
        // 静默处理解析错误，避免暴露内部信息
      }
    }

    const handleError = () => {
      eventSource?.close()
      eventSource = null
      startPolling()
    }

    try {
      eventSource = new EventSource(`${this.base}/events?taskId=${taskId}`)
      eventSource.onmessage = handleMessage
      eventSource.onerror = handleError
    } catch {
      startPolling()
    }

    return stop
  }

  async getTask(taskId: string, signal?: AbortSignal): Promise<TaskResponse> {
    const response = await fetchWithTimeout(`${this.base}/tasks/${taskId}`, undefined, signal)
    if (!response.ok) {
      throw new Error('任务详情暂时无法加载')
    }
    return response.json()
  }

  async getTaskDiagnostics(taskId: string, signal?: AbortSignal): Promise<Diagnostic[]> {
    const response = await fetchWithTimeout(
      `${this.base}/tasks/${taskId}/diagnostics`,
      undefined,
      signal
    )
    if (!response.ok) {
      throw new Error('处理提示暂时无法加载')
    }
    return response.json()
  }

  async getGithubStars(signal?: AbortSignal): Promise<GithubStarsResponse> {
    const response = await fetchWithTimeout(
      `${this.base}/github/stars`,
      undefined,
      signal,
      PUBLIC_METADATA_TIMEOUT_MS
    )
    if (!response.ok) {
      throw new Error('GitHub 数据暂时无法加载')
    }

    const payload = (await response.json()) as Partial<GithubStarsResponse>
    if (
      !Number.isSafeInteger(payload.stars) ||
      (payload.stars ?? -1) < 0 ||
      typeof payload.stale !== 'boolean'
    ) {
      throw new Error('GitHub 数据暂时无法加载')
    }

    return {
      stars: payload.stars as number,
      stale: payload.stale,
    }
  }
}

async function fetchWithTimeout(
  input: RequestInfo | URL,
  init?: RequestInit,
  externalSignal?: AbortSignal,
  timeoutMs = DETAIL_REQUEST_TIMEOUT_MS
): Promise<Response> {
  const controller = new AbortController()
  const abortFromExternal = () => controller.abort()

  if (externalSignal?.aborted) {
    controller.abort()
  } else {
    externalSignal?.addEventListener('abort', abortFromExternal, { once: true })
  }

  const timeout = setTimeout(() => controller.abort(), timeoutMs)
  try {
    return await fetch(input, { ...init, signal: controller.signal })
  } catch (error) {
    if (controller.signal.aborted && !externalSignal?.aborted) {
      throw new Error('请求超时，请稍后重试')
    }
    throw error
  } finally {
    clearTimeout(timeout)
    externalSignal?.removeEventListener('abort', abortFromExternal)
  }
}

export const api = new ApiClient()

async function extractErrorMessage(response: Response): Promise<string> {
  if (response.status === 413) {
    return '上传失败：文件过大，超过服务允许的上传大小'
  }

  const contentType = response.headers.get('content-type') ?? ''

  if (contentType.includes('application/json')) {
    try {
      const payload = (await response.json()) as { message?: string }
      if (payload.message) {
        return payload.message
      }
    } catch {
      // Ignore malformed JSON and fall back to generic error text.
    }
  }

  try {
    const text = await response.text()
    if (text.includes('413 Request Entity Too Large')) {
      return '上传失败：文件过大，超过服务允许的上传大小'
    }
  } catch {
    // Ignore body read failures and fall back to generic error text.
  }

  return `上传失败：服务返回 ${response.status}`
}
