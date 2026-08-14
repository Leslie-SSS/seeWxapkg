import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  ACTIVE_TASK_MAX_AGE_MS,
  ACTIVE_TASK_STORAGE_KEY,
  applyTaskDetail,
  applyTerminalEvent,
  mapTaskStatusToUI,
  parseStoredActiveTask,
  useSeeWxapkgUpload,
} from './useSeeWxapkgUpload'
import { api, type Diagnostic, type ProgressEvent, type TaskResponse } from '../api/client'

const TASK_A = '11111111-1111-4111-8111-111111111111'
const TASK_B = '22222222-2222-4222-8222-222222222222'

afterEach(() => {
  sessionStorage.clear()
  vi.restoreAllMocks()
})

describe('useSeeWxapkgUpload helpers', () => {
  it('maps terminal statuses to UI states', () => {
    expect(mapTaskStatusToUI('completed')).toBe('completed')
    expect(mapTaskStatusToUI('partial')).toBe('partial')
    expect(mapTaskStatusToUI('failed')).toBe('failed')
    expect(mapTaskStatusToUI('verifying')).toBe('processing')
    expect(mapTaskStatusToUI('')).toBe('idle')
  })

  it('applies task detail into upload state', () => {
    const detail: TaskResponse = {
      id: 'task-1',
      status: 'partial',
      progress: 100,
      currentStage: 'partial',
      currentMessage: '深度恢复存在缺口',
      diagnosticsCount: 12,
      artifacts: {
        fileCount: 8,
        downloadUrl: '/api/download/task-1',
        reportUrl: '/api/tasks/task-1/report',
        diagnosticsUrl: '/api/tasks/task-1/diagnostics',
      },
      score: {
        overall: 72,
        manifest: 100,
        js: 70,
        wxml: 60,
        wxss: 65,
        decompileHit: true,
        fallbackUsed: true,
        generatedRatio: 25,
        fallbackPenalty: 10,
        verifierPassed: false,
      },
    }

    const next = applyTaskDetail(
      {
        isUploading: true,
        progress: 0,
        stage: '',
        message: '',
        status: 'processing',
        isComplete: false,
        connectionInterrupted: false,
      },
      detail
    )

    expect(next.status).toBe('partial')
    expect(next.fileCount).toBe(8)
    expect(next.downloadUrl).toBe('/api/download/task-1')
    expect(next.recoveryScore?.fallbackUsed).toBe(true)
    expect(next.isComplete).toBe(true)
  })

  it('keeps authoritative terminal fields when a late detail response omits or regresses them', () => {
    const terminal = applyTerminalEvent(
      {
        isUploading: true,
        progress: 95,
        stage: 'packaging',
        message: '打包中',
        status: 'processing',
        archiveSize: 4096,
        isComplete: false,
        connectionInterrupted: false,
      },
      {
        type: 'complete',
        taskId: TASK_A,
        stage: 'completed',
        status: 'completed',
        percent: 100,
        message: '恢复完成',
        fileCount: 12,
        downloadUrl: '/api/download/result',
        reportUrl: '/api/tasks/result/report',
        diagnosticsCount: 9,
      }
    )

    const next = applyTaskDetail(terminal, {
      ...taskDetail(TASK_A, 'packaging', 80),
      diagnosticsCount: 0,
      artifacts: {
        fileCount: 1,
        downloadUrl: '/api/download/stale',
        reportUrl: '/api/tasks/stale/report',
      },
      errorMessage: '晚到的失败信息',
    })

    expect(next.status).toBe('completed')
    expect(next.progress).toBe(100)
    expect(next.stage).toBe('completed')
    expect(next.message).toBe('恢复完成')
    expect(next.fileCount).toBe(12)
    expect(next.archiveSize).toBe(4096)
    expect(next.downloadUrl).toBe('/api/download/result')
    expect(next.reportUrl).toBe('/api/tasks/result/report')
    expect(next.diagnosticsCount).toBe(9)
    expect(next.error).toBeUndefined()
    expect(next.isComplete).toBe(true)
  })

  it('uses terminal event artifacts and keeps detail fetch errors non-fatal', () => {
    const next = applyTerminalEvent(
      {
        isUploading: true,
        progress: 90,
        stage: 'packaging',
        message: '打包中',
        status: 'processing',
        fileCount: 1,
        downloadUrl: '/api/download/stale',
        isComplete: false,
        connectionInterrupted: true,
      },
      {
        type: 'complete',
        stage: 'completed',
        status: 'completed',
        percent: 100,
        message: '完成',
        taskId: 'task-1',
        fileCount: 12,
        downloadUrl: '/api/download/task-1',
        reportUrl: '/api/tasks/task-1/report',
      },
      '详情暂时不可用'
    )

    expect(next.isComplete).toBe(true)
    expect(next.fileCount).toBe(12)
    expect(next.downloadUrl).toBe('/api/download/task-1')
    expect(next.error).toBeUndefined()
    expect(next.warning).toBe('详情暂时不可用')
    expect(next.connectionInterrupted).toBe(false)
  })

  it('clears a stale failure message when a terminal success arrives', () => {
    const next = applyTerminalEvent(
      {
        isUploading: true,
        progress: 90,
        stage: 'packaging',
        message: '打包中',
        status: 'processing',
        error: '之前的错误',
        errorCode: 'unpack_failed',
        errorDetail: '旧根因',
        isComplete: false,
        connectionInterrupted: false,
      },
      {
        type: 'complete',
        stage: 'completed',
        status: 'completed',
        percent: 100,
        message: '完成',
        taskId: 'task-1',
      }
    )

    expect(next.status).toBe('completed')
    expect(next.error).toBeUndefined()
    expect(next.errorCode).toBeUndefined()
    expect(next.errorDetail).toBeUndefined()
  })
})

describe('active task recovery record', () => {
  it('accepts only a recent, well-formed task record', () => {
    const now = 1_800_000_000_000
    expect(
      parseStoredActiveTask(JSON.stringify({ taskId: TASK_A, savedAt: now - 1_000 }), now)
    ).toEqual({ taskId: TASK_A, savedAt: now - 1_000 })

    expect(
      parseStoredActiveTask(
        JSON.stringify({ taskId: TASK_A, savedAt: now - ACTIVE_TASK_MAX_AGE_MS - 1 }),
        now
      )
    ).toBeUndefined()
    expect(
      parseStoredActiveTask(JSON.stringify({ taskId: '../old-task', savedAt: now }), now)
    ).toBeUndefined()
    expect(parseStoredActiveTask('{broken', now)).toBeUndefined()
  })

  it('removes stale records without querying or changing the idle screen', () => {
    sessionStorage.setItem(
      ACTIVE_TASK_STORAGE_KEY,
      JSON.stringify({ taskId: TASK_A, savedAt: Date.now() - ACTIVE_TASK_MAX_AGE_MS - 1 })
    )
    const getTask = vi.spyOn(api, 'getTask')

    const { result } = renderHook(() => useSeeWxapkgUpload())

    expect(result.current.status).toBe('idle')
    expect(getTask).not.toHaveBeenCalled()
    expect(sessionStorage.getItem(ACTIVE_TASK_STORAGE_KEY)).toBeNull()
  })

  it('restores a recent in-progress task in the same tab', async () => {
    sessionStorage.setItem(
      ACTIVE_TASK_STORAGE_KEY,
      JSON.stringify({ taskId: TASK_A, savedAt: Date.now() })
    )
    vi.spyOn(api, 'getTask').mockResolvedValue(taskDetail(TASK_A, 'recovering_js', 42))
    const subscribe = vi.spyOn(api, 'subscribeProgress').mockReturnValue(vi.fn())

    const { result } = renderHook(() => useSeeWxapkgUpload())

    await waitFor(() => expect(result.current.progress).toBe(42))
    expect(result.current.taskId).toBe(TASK_A)
    expect(result.current.isUploading).toBe(true)
    expect(subscribe).toHaveBeenCalledWith(TASK_A, expect.any(Function), expect.any(Function))
    expect(parseStoredActiveTask(sessionStorage.getItem(ACTIVE_TASK_STORAGE_KEY))?.taskId).toBe(
      TASK_A
    )
  })

  it('does not let a delayed old recovery overwrite a new upload', async () => {
    sessionStorage.setItem(
      ACTIVE_TASK_STORAGE_KEY,
      JSON.stringify({ taskId: TASK_A, savedAt: Date.now() })
    )
    let resolveOldTask!: (detail: TaskResponse) => void
    vi.spyOn(api, 'getTask').mockReturnValue(
      new Promise<TaskResponse>((resolve) => {
        resolveOldTask = resolve
      })
    )
    vi.spyOn(api, 'compile').mockResolvedValue({
      success: true,
      taskId: TASK_B,
      message: 'ok',
    })
    vi.spyOn(api, 'subscribeProgress').mockReturnValue(vi.fn())

    const { result } = renderHook(() => useSeeWxapkgUpload())
    await waitFor(() => expect(api.getTask).toHaveBeenCalledWith(TASK_A))

    await act(async () => {
      await result.current.upload(new File(['data'], '__APP__.wxapkg'))
    })
    expect(result.current.taskId).toBe(TASK_B)

    await act(async () => {
      resolveOldTask(taskDetail(TASK_A, 'recovering_wxml', 88))
      await Promise.resolve()
    })

    expect(result.current.taskId).toBe(TASK_B)
    expect(result.current.progress).toBe(0)
    expect(parseStoredActiveTask(sessionStorage.getItem(ACTIVE_TASK_STORAGE_KEY))?.taskId).toBe(
      TASK_B
    )
  })

  it('renders core terminal detail without waiting for diagnostics', async () => {
    sessionStorage.setItem(
      ACTIVE_TASK_STORAGE_KEY,
      JSON.stringify({ taskId: TASK_A, savedAt: Date.now() })
    )
    vi.spyOn(api, 'getTask').mockResolvedValue({
      ...taskDetail(TASK_A, 'completed', 100),
      diagnosticsCount: 1,
      artifacts: {
        fileCount: 12,
        downloadUrl: `/api/download/${TASK_A}`,
      },
    })
    let resolveDiagnostics!: (diagnostics: Diagnostic[]) => void
    vi.spyOn(api, 'getTaskDiagnostics').mockReturnValue(
      new Promise<Diagnostic[]>((resolve) => {
        resolveDiagnostics = resolve
      })
    )
    const subscribe = vi.spyOn(api, 'subscribeProgress').mockReturnValue(vi.fn())

    const { result } = renderHook(() => useSeeWxapkgUpload())

    await waitFor(() => expect(result.current.status).toBe('completed'))
    expect(result.current.downloadUrl).toBe(`/api/download/${TASK_A}`)
    expect(result.current.diagnostics).toBeUndefined()
    expect(subscribe).not.toHaveBeenCalled()

    await act(async () => {
      resolveDiagnostics([{ code: 'WXML_RECOVERED', severity: 'info', message: '页面结构已恢复' }])
      await Promise.resolve()
    })
    expect(result.current.diagnostics?.[0]?.code).toBe('WXML_RECOVERED')
  })

  it('does not let delayed diagnostics from an old operation write into a new upload', async () => {
    sessionStorage.setItem(
      ACTIVE_TASK_STORAGE_KEY,
      JSON.stringify({ taskId: TASK_A, savedAt: Date.now() })
    )
    vi.spyOn(api, 'getTask').mockResolvedValue({
      ...taskDetail(TASK_A, 'completed', 100),
      diagnosticsCount: 1,
    })
    let resolveDiagnostics!: (diagnostics: Diagnostic[]) => void
    vi.spyOn(api, 'getTaskDiagnostics').mockReturnValue(
      new Promise<Diagnostic[]>((resolve) => {
        resolveDiagnostics = resolve
      })
    )
    vi.spyOn(api, 'compile').mockResolvedValue({
      success: true,
      // Reusing the ID deliberately proves the operation guard independently
      // from the task-ID guard.
      taskId: TASK_A,
      message: 'ok',
    })
    vi.spyOn(api, 'subscribeProgress').mockReturnValue(vi.fn())

    const { result } = renderHook(() => useSeeWxapkgUpload())
    await waitFor(() => expect(api.getTaskDiagnostics).toHaveBeenCalledWith(TASK_A))

    await act(async () => {
      await result.current.upload(new File(['data'], '__APP__.wxapkg'))
    })
    expect(result.current.taskId).toBe(TASK_A)
    expect(result.current.status).toBe('processing')

    await act(async () => {
      resolveDiagnostics([{ code: 'OLD_TASK', severity: 'warn', message: '这是旧任务的诊断' }])
      await Promise.resolve()
    })

    expect(result.current.taskId).toBe(TASK_A)
    expect(result.current.diagnostics).toBeUndefined()
    expect(result.current.warning).toBeUndefined()
  })
})

describe('useSeeWxapkgUpload connection state', () => {
  it('uses the recommended full-recovery defaults when options are omitted', async () => {
    vi.spyOn(api, 'compile').mockResolvedValue({ success: true, taskId: TASK_A, message: 'ok' })
    vi.spyOn(api, 'subscribeProgress').mockReturnValue(vi.fn())

    const { result } = renderHook(() => useSeeWxapkgUpload())
    const file = new File(['data'], '__APP__.wxapkg')
    await act(async () => {
      await result.current.upload(file)
    })

    expect(api.compile).toHaveBeenCalledWith({
      file,
      appId: undefined,
      beautify: true,
      decompile: true,
    })
  })

  it('submits only once when upload is called twice in the same turn', async () => {
    let resolveCompile!: (response: { success: true; taskId: string; message: string }) => void
    const compile = vi.spyOn(api, 'compile').mockReturnValue(
      new Promise((resolve) => {
        resolveCompile = resolve
      })
    )
    vi.spyOn(api, 'subscribeProgress').mockReturnValue(vi.fn())

    const { result } = renderHook(() => useSeeWxapkgUpload())
    const file = new File(['data'], '__APP__.wxapkg')
    let firstUpload!: Promise<void>
    let secondUpload!: Promise<void>

    act(() => {
      firstUpload = result.current.upload(file)
      secondUpload = result.current.upload(file)
    })

    expect(compile).toHaveBeenCalledOnce()

    await act(async () => {
      resolveCompile({ success: true, taskId: TASK_A, message: 'ok' })
      await Promise.all([firstUpload, secondUpload])
    })

    expect(result.current.taskId).toBe(TASK_A)
    expect(api.subscribeProgress).toHaveBeenCalledOnce()
  })

  it('shows a non-fatal interruption and clears it after recovery', async () => {
    let connectionListener: ((state: 'interrupted' | 'restored') => void) | undefined
    vi.spyOn(api, 'compile').mockResolvedValue({ success: true, taskId: TASK_A, message: 'ok' })
    vi.spyOn(api, 'subscribeProgress').mockImplementation((_taskId, _onEvent, onConnection) => {
      connectionListener = onConnection
      return vi.fn()
    })

    const { result } = renderHook(() => useSeeWxapkgUpload())
    await act(async () => {
      await result.current.upload(new File(['data'], '__APP__.wxapkg'))
    })

    act(() => connectionListener?.('interrupted'))
    expect(result.current.connectionInterrupted).toBe(true)
    expect(result.current.status).toBe('processing')
    expect(result.current.error).toBeUndefined()

    act(() => connectionListener?.('restored'))
    expect(result.current.connectionInterrupted).toBe(false)
  })

  it('clears recovery state and the stored task on reset', async () => {
    let connectionListener: ((state: 'interrupted' | 'restored') => void) | undefined
    const unsubscribe = vi.fn()
    vi.spyOn(api, 'compile').mockResolvedValue({ success: true, taskId: TASK_A, message: 'ok' })
    vi.spyOn(api, 'subscribeProgress').mockImplementation((_taskId, _onEvent, onConnection) => {
      connectionListener = onConnection
      return unsubscribe
    })

    const { result } = renderHook(() => useSeeWxapkgUpload())
    await act(async () => {
      await result.current.upload(new File(['data'], '__APP__.wxapkg'))
    })
    act(() => connectionListener?.('interrupted'))

    act(() => result.current.reset())

    expect(result.current.status).toBe('idle')
    expect(result.current.connectionInterrupted).toBe(false)
    expect(sessionStorage.getItem(ACTIVE_TASK_STORAGE_KEY)).toBeNull()
    expect(unsubscribe).toHaveBeenCalledOnce()
  })

  it('stops the live subscription on unmount while keeping reload recovery available', async () => {
    const unsubscribe = vi.fn()
    vi.spyOn(api, 'compile').mockResolvedValue({ success: true, taskId: TASK_A, message: 'ok' })
    vi.spyOn(api, 'subscribeProgress').mockReturnValue(unsubscribe)

    const { result, unmount } = renderHook(() => useSeeWxapkgUpload())
    await act(async () => {
      await result.current.upload(new File(['data'], '__APP__.wxapkg'))
    })

    unmount()

    expect(unsubscribe).toHaveBeenCalledOnce()
    expect(parseStoredActiveTask(sessionStorage.getItem(ACTIVE_TASK_STORAGE_KEY))?.taskId).toBe(
      TASK_A
    )
  })

  it('keeps a completed result recoverable until the user explicitly resets it', async () => {
    let progressListener: ((event: ProgressEvent) => void) | undefined
    vi.spyOn(api, 'compile').mockResolvedValue({ success: true, taskId: TASK_A, message: 'ok' })
    vi.spyOn(api, 'getTask').mockResolvedValue(taskDetail(TASK_A, 'completed', 100))
    vi.spyOn(api, 'subscribeProgress').mockImplementation((_taskId, onEvent) => {
      progressListener = onEvent
      return vi.fn()
    })

    const { result } = renderHook(() => useSeeWxapkgUpload())
    await act(async () => {
      await result.current.upload(new File(['data'], '__APP__.wxapkg'))
    })
    expect(sessionStorage.getItem(ACTIVE_TASK_STORAGE_KEY)).not.toBeNull()

    await act(async () => {
      progressListener?.({
        type: 'complete',
        taskId: TASK_A,
        stage: 'completed',
        status: 'completed',
        percent: 100,
        message: '处理完成',
      })
      await Promise.resolve()
    })

    await waitFor(() => expect(result.current.isComplete).toBe(true))
    expect(parseStoredActiveTask(sessionStorage.getItem(ACTIVE_TASK_STORAGE_KEY))?.taskId).toBe(
      TASK_A
    )
  })

  it('restores a recent completed result after a same-tab refresh', async () => {
    sessionStorage.setItem(
      ACTIVE_TASK_STORAGE_KEY,
      JSON.stringify({ taskId: TASK_A, savedAt: Date.now() })
    )
    vi.spyOn(api, 'getTask').mockResolvedValue({
      ...taskDetail(TASK_A, 'completed', 100),
      artifacts: {
        fileCount: 12,
        archiveSize: 2048,
        downloadUrl: `/api/download/${TASK_A}`,
        reportUrl: `/api/tasks/${TASK_A}/report`,
      },
    })
    const subscribe = vi.spyOn(api, 'subscribeProgress').mockReturnValue(vi.fn())

    const { result } = renderHook(() => useSeeWxapkgUpload())

    await waitFor(() => expect(result.current.isComplete).toBe(true))
    expect(result.current.status).toBe('completed')
    expect(result.current.downloadUrl).toBe(`/api/download/${TASK_A}`)
    expect(subscribe).not.toHaveBeenCalled()
    expect(parseStoredActiveTask(sessionStorage.getItem(ACTIVE_TASK_STORAGE_KEY))?.taskId).toBe(
      TASK_A
    )
  })

  it('shows a terminal result immediately even when detail loading never resolves', async () => {
    let progressListener: ((event: ProgressEvent) => void) | undefined
    let connectionListener: ((state: 'interrupted' | 'restored') => void) | undefined
    const unsubscribe = vi.fn()
    vi.spyOn(api, 'compile').mockResolvedValue({ success: true, taskId: TASK_A, message: 'ok' })
    vi.spyOn(api, 'getTask').mockReturnValue(new Promise<TaskResponse>(() => {}))
    vi.spyOn(api, 'subscribeProgress').mockImplementation((_taskId, onEvent, onConnection) => {
      progressListener = onEvent
      connectionListener = onConnection
      return unsubscribe
    })

    const { result } = renderHook(() => useSeeWxapkgUpload())
    await act(async () => {
      await result.current.upload(new File(['data'], '__APP__.wxapkg'))
    })

    act(() => {
      progressListener?.({
        type: 'partial',
        taskId: TASK_A,
        stage: 'partial',
        status: 'partial',
        percent: 100,
        message: '结果已生成',
        fileCount: 12,
        downloadUrl: '/api/download/result',
      })
    })

    expect(result.current.status).toBe('partial')
    expect(result.current.isComplete).toBe(true)
    expect(result.current.downloadUrl).toBe('/api/download/result')
    expect(result.current.fileCount).toBe(12)
    expect(unsubscribe).toHaveBeenCalledOnce()

    act(() => {
      progressListener?.({
        type: 'progress',
        taskId: TASK_A,
        stage: 'packaging',
        status: 'packaging',
        percent: 55,
        message: '晚到的进度',
      })
      progressListener?.({
        type: 'error',
        taskId: TASK_A,
        stage: 'failed',
        status: 'failed',
        percent: 0,
        message: '晚到的错误',
      })
      connectionListener?.('interrupted')
    })

    expect(result.current.status).toBe('partial')
    expect(result.current.progress).toBe(100)
    expect(result.current.downloadUrl).toBe('/api/download/result')
    expect(result.current.error).toBeUndefined()
    expect(result.current.connectionInterrupted).toBe(false)
    expect(parseStoredActiveTask(sessionStorage.getItem(ACTIVE_TASK_STORAGE_KEY))?.taskId).toBe(
      TASK_A
    )
  })

  it('shows the terminal message when an error event has no separate error field', async () => {
    let progressListener: ((event: ProgressEvent) => void) | undefined
    const unsubscribe = vi.fn()
    vi.spyOn(api, 'compile').mockResolvedValue({ success: true, taskId: TASK_A, message: 'ok' })
    vi.spyOn(api, 'subscribeProgress').mockImplementation((_taskId, onEvent) => {
      progressListener = onEvent
      return unsubscribe
    })

    const { result } = renderHook(() => useSeeWxapkgUpload())
    await act(async () => {
      await result.current.upload(new File(['data'], '__APP__.wxapkg'))
    })

    act(() => {
      progressListener?.({
        type: 'error',
        taskId: TASK_A,
        stage: 'failed',
        status: 'failed',
        percent: 0,
        message: '处理服务暂时不可用',
      })
    })

    expect(result.current.status).toBe('failed')
    expect(result.current.error).toBe('处理服务暂时不可用')
    expect(unsubscribe).toHaveBeenCalledOnce()
    expect(sessionStorage.getItem(ACTIVE_TASK_STORAGE_KEY)).toBeNull()
  })

  it('keeps the backend error code from the error event', async () => {
    let progressListener: ((event: ProgressEvent) => void) | undefined
    const unsubscribe = vi.fn()
    vi.spyOn(api, 'compile').mockResolvedValue({ success: true, taskId: TASK_A, message: 'ok' })
    vi.spyOn(api, 'subscribeProgress').mockImplementation((_taskId, onEvent) => {
      progressListener = onEvent
      return unsubscribe
    })

    const { result } = renderHook(() => useSeeWxapkgUpload())
    await act(async () => {
      await result.current.upload(new File(['data'], '__APP__.wxapkg'))
    })

    act(() => {
      progressListener?.({
        type: 'error',
        taskId: TASK_A,
        stage: 'failed',
        status: 'failed',
        percent: 0,
        message: '解包失败',
        error: '解包失败',
        errorCode: 'unpack_failed',
      })
    })

    expect(result.current.status).toBe('failed')
    expect(result.current.errorCode).toBe('unpack_failed')
  })

  it('propagates errorCode and errorDetail from the task detail', () => {
    const next = applyTaskDetail(
      {
        isUploading: true,
        progress: 0,
        stage: '',
        message: '',
        status: 'processing',
        isComplete: false,
        connectionInterrupted: false,
      },
      {
        ...taskDetail(TASK_A, 'failed', 0),
        errorCode: 'unpack_failed',
        errorMessage: '解包失败',
        errorDetail: '首标记错误（期望 0xBE，实际 0x00）',
      }
    )

    expect(next.status).toBe('failed')
    expect(next.errorCode).toBe('unpack_failed')
    expect(next.errorDetail).toContain('首标记错误')
  })
})

function taskDetail(id: string, status: TaskResponse['status'], progress: number): TaskResponse {
  return {
    id,
    status,
    progress,
    currentStage: status,
    currentMessage: '处理中',
    diagnosticsCount: 0,
  }
}
