import { useCallback, useEffect, useRef, useState } from 'react'
import {
  api,
  Diagnostic,
  ProgressConnectionState,
  ProgressEvent,
  RecoveryScore,
  TaskResponse,
  PackageProfile,
  StageResult,
} from '../api/client'

interface UploadState {
  isUploading: boolean
  progress: number
  stage: string
  message: string
  status: 'idle' | 'processing' | 'completed' | 'partial' | 'failed'
  fileCount?: number
  archiveSize?: number
  downloadUrl?: string
  reportUrl?: string
  diagnosticsUrl?: string
  diagnosticsCount?: number
  diagnostics?: Diagnostic[]
  recoveryScore?: RecoveryScore
  packageProfile?: PackageProfile
  stages?: StageResult[]
  error?: string
  warning?: string
  taskId?: string
  isComplete: boolean
  connectionInterrupted: boolean
}

export const ACTIVE_TASK_STORAGE_KEY = 'see-wxapkg.active-task.v1'
export const ACTIVE_TASK_MAX_AGE_MS = 12 * 60 * 60 * 1000
const TASK_ID_PATTERN = /^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/

interface StoredActiveTask {
  taskId: string
  savedAt: number
}

export function parseStoredActiveTask(
  raw: string | null,
  now = Date.now()
): StoredActiveTask | undefined {
  if (!raw) return undefined

  try {
    const value = JSON.parse(raw) as Partial<StoredActiveTask>
    if (
      typeof value.taskId !== 'string' ||
      !TASK_ID_PATTERN.test(value.taskId) ||
      typeof value.savedAt !== 'number' ||
      !Number.isFinite(value.savedAt) ||
      value.savedAt > now + 60_000 ||
      now - value.savedAt > ACTIVE_TASK_MAX_AGE_MS
    ) {
      return undefined
    }
    return { taskId: value.taskId, savedAt: value.savedAt }
  } catch {
    return undefined
  }
}

function readStoredActiveTask(): StoredActiveTask | undefined {
  try {
    const stored = parseStoredActiveTask(sessionStorage.getItem(ACTIVE_TASK_STORAGE_KEY))
    if (!stored) {
      sessionStorage.removeItem(ACTIVE_TASK_STORAGE_KEY)
    }
    return stored
  } catch {
    return undefined
  }
}

function storeActiveTask(taskId: string) {
  if (!TASK_ID_PATTERN.test(taskId)) return

  try {
    sessionStorage.setItem(
      ACTIVE_TASK_STORAGE_KEY,
      JSON.stringify({ taskId, savedAt: Date.now() } satisfies StoredActiveTask)
    )
  } catch {
    // Storage can be unavailable in private browsing. Live progress still works.
  }
}

function clearStoredActiveTask(taskId?: string) {
  try {
    if (taskId) {
      const stored = parseStoredActiveTask(sessionStorage.getItem(ACTIVE_TASK_STORAGE_KEY))
      if (stored?.taskId !== taskId) return
    }
    sessionStorage.removeItem(ACTIVE_TASK_STORAGE_KEY)
  } catch {
    // Storage recovery is optional and must never block a task.
  }
}

export function useSeeWxapkgUpload() {
  const [state, setState] = useState<UploadState>({
    isUploading: false,
    progress: 0,
    stage: '',
    message: '',
    status: 'idle',
    isComplete: false,
    connectionInterrupted: false,
  })

  const unsubscribeRef = useRef<(() => void) | null>(null)
  const activeTaskIdRef = useRef<string | null>(null)
  const terminalTaskIdRef = useRef<string | null>(null)
  const uploadInFlightRef = useRef(false)
  const operationRef = useRef(0)

  const stopSubscription = useCallback(() => {
    const unsubscribe = unsubscribeRef.current
    unsubscribeRef.current = null
    unsubscribe?.()
  }, [])

  const fetchTaskDetail = useCallback(async (taskId: string, operation: number) => {
    const detail = await api.getTask(taskId)

    const isCurrentTask = () =>
      operationRef.current === operation && activeTaskIdRef.current === taskId

    if (!isCurrentTask()) return detail

    setState((prev) => (isCurrentTask() ? applyTaskDetail(prev, detail) : prev))

    if (detail.diagnosticsCount > 0) {
      // Diagnostics are supplementary and can be large. Let the core result
      // render first, then enrich it without allowing an earlier task to
      // write into a newer operation.
      void (async () => {
        try {
          const diagnostics: Diagnostic[] = await api.getTaskDiagnostics(taskId)
          if (!isCurrentTask()) return
          setState((prev) =>
            isCurrentTask() ? { ...prev, diagnostics, warning: undefined } : prev
          )
        } catch {
          if (!isCurrentTask()) return
          setState((prev) =>
            isCurrentTask() ? { ...prev, warning: '处理提示详情暂时无法加载' } : prev
          )
        }
      })()
    }

    return detail
  }, [])

  const handleProgressEvent = useCallback(
    async (taskId: string, event: ProgressEvent) => {
      if (
        activeTaskIdRef.current !== taskId ||
        terminalTaskIdRef.current === taskId ||
        (event.taskId !== undefined && event.taskId !== taskId)
      ) {
        return
      }

      if (event.type === 'progress') {
        setState((prev) => ({
          ...prev,
          progress: event.percent,
          stage: event.stage,
          message: event.message,
          status: 'processing',
          connectionInterrupted: false,
        }))
        return
      }

      if (event.type === 'complete' || event.type === 'partial') {
        const operation = operationRef.current
        terminalTaskIdRef.current = taskId
        stopSubscription()
        // The stream event is already authoritative for terminal state and
        // download links. Show it immediately; optional detail requests must
        // never leave the user stuck on “processing”.
        setState((prev) => applyTerminalEvent(prev, event))
        // Keep the recent downloadable result in this tab so an accidental
        // refresh does not strand the user before they save the ZIP. The
        // explicit “continue” action and the 12-hour expiry still clear it.
        storeActiveTask(taskId)

        try {
          await fetchTaskDetail(taskId, operation)
        } catch (err) {
          if (
            operationRef.current === operation &&
            activeTaskIdRef.current === taskId &&
            terminalTaskIdRef.current === taskId
          ) {
            const detailError = err instanceof Error ? err.message : '任务详情暂时无法加载'
            setState((prev) =>
              operationRef.current === operation && activeTaskIdRef.current === taskId
                ? { ...prev, warning: detailError }
                : prev
            )
          }
        }
        return
      }

      if (event.type === 'error') {
        terminalTaskIdRef.current = taskId
        stopSubscription()
        setState((prev) => ({
          ...prev,
          isUploading: false,
          progress: 0,
          stage: 'failed',
          message: event.message,
          status: 'failed',
          error: event.error || event.message || '处理过程中遇到问题，请重试',
          isComplete: false,
          connectionInterrupted: false,
        }))
        clearStoredActiveTask(taskId)
        activeTaskIdRef.current = null
      }
    },
    [fetchTaskDetail, stopSubscription]
  )

  const handleConnectionState = useCallback(
    (taskId: string, connectionState: ProgressConnectionState) => {
      if (activeTaskIdRef.current !== taskId || terminalTaskIdRef.current === taskId) {
        return
      }

      setState((prev) => ({
        ...prev,
        connectionInterrupted: connectionState === 'interrupted',
      }))
    },
    []
  )

  const subscribeToTask = useCallback(
    (taskId: string) => {
      stopSubscription()
      const unsubscribe = api.subscribeProgress(
        taskId,
        (event: ProgressEvent) => {
          void handleProgressEvent(taskId, event)
        },
        (connectionState) => handleConnectionState(taskId, connectionState)
      )

      // Be defensive if an implementation delivers a terminal event while
      // establishing the subscription, before it returns its cleanup handle.
      if (terminalTaskIdRef.current === taskId || activeTaskIdRef.current !== taskId) {
        unsubscribe()
      } else {
        unsubscribeRef.current = unsubscribe
      }
    },
    [handleConnectionState, handleProgressEvent, stopSubscription]
  )

  useEffect(() => {
    const stored = readStoredActiveTask()
    if (!stored) return

    const operation = ++operationRef.current
    activeTaskIdRef.current = stored.taskId
    terminalTaskIdRef.current = null
    setState({
      isUploading: true,
      progress: 0,
      stage: 'queued',
      message: '正在加载上次任务...',
      status: 'processing',
      taskId: stored.taskId,
      isComplete: false,
      connectionInterrupted: false,
    })

    void (async () => {
      try {
        const detail = await fetchTaskDetail(stored.taskId, operation)
        if (operationRef.current !== operation || activeTaskIdRef.current !== stored.taskId) {
          return
        }

        if (detail.status === 'completed' || detail.status === 'partial') {
          terminalTaskIdRef.current = stored.taskId
          stopSubscription()
          return
        }
        if (detail.status === 'failed') {
          terminalTaskIdRef.current = stored.taskId
          stopSubscription()
          clearStoredActiveTask(stored.taskId)
          activeTaskIdRef.current = null
          return
        }
      } catch {
        if (operationRef.current !== operation || activeTaskIdRef.current !== stored.taskId) {
          return
        }
      }

      subscribeToTask(stored.taskId)
    })()
  }, [fetchTaskDetail, stopSubscription, subscribeToTask])

  useEffect(
    () => () => {
      operationRef.current += 1
      uploadInFlightRef.current = false
      stopSubscription()
      activeTaskIdRef.current = null
      terminalTaskIdRef.current = null
    },
    [stopSubscription]
  )

  const upload = useCallback(
    async (file: File, appId?: string, beautify = true, decompile = true) => {
      if (uploadInFlightRef.current) return
      uploadInFlightRef.current = true

      stopSubscription()
      const operation = ++operationRef.current
      activeTaskIdRef.current = null
      terminalTaskIdRef.current = null
      clearStoredActiveTask()

      setState({
        isUploading: true,
        progress: 0,
        stage: 'uploading',
        message: '正在上传文件...',
        status: 'processing',
        isComplete: false,
        connectionInterrupted: false,
      })

      try {
        // 上传文件
        const response = await api.compile({ file, appId, beautify, decompile })

        if (operationRef.current !== operation) return

        if (!response.success) {
          throw new Error(response.message)
        }

        activeTaskIdRef.current = response.taskId
        storeActiveTask(response.taskId)
        setState((prev) => ({
          ...prev,
          taskId: response.taskId,
          stage: 'processing',
          message: '开始处理...',
          status: 'processing',
        }))

        subscribeToTask(response.taskId)
      } catch (err) {
        if (operationRef.current !== operation) return

        activeTaskIdRef.current = null
        clearStoredActiveTask()
        setState({
          isUploading: false,
          progress: 0,
          stage: 'failed',
          message: '上传失败，请重试',
          status: 'failed',
          error: err instanceof Error ? err.message : '未知错误',
          isComplete: false,
          connectionInterrupted: false,
        })
      } finally {
        if (operationRef.current === operation) {
          uploadInFlightRef.current = false
        }
      }
    },
    [stopSubscription, subscribeToTask]
  )

  const reset = useCallback(() => {
    operationRef.current += 1
    uploadInFlightRef.current = false
    stopSubscription()
    activeTaskIdRef.current = null
    terminalTaskIdRef.current = null
    clearStoredActiveTask()
    setState({
      isUploading: false,
      progress: 0,
      stage: '',
      message: '',
      status: 'idle',
      isComplete: false,
      connectionInterrupted: false,
    })
  }, [stopSubscription])

  return {
    ...state,
    upload,
    reset,
  }
}

export function applyTerminalEvent(
  prev: UploadState,
  event: ProgressEvent,
  detailError?: string
): UploadState {
  return {
    ...prev,
    isUploading: false,
    progress: 100,
    stage: event.stage,
    message: event.message,
    status: event.type === 'partial' ? 'partial' : 'completed',
    isComplete: true,
    fileCount: event.fileCount ?? prev.fileCount,
    downloadUrl: event.downloadUrl ?? prev.downloadUrl,
    reportUrl: event.reportUrl ?? prev.reportUrl,
    diagnosticsUrl: event.diagnosticsUrl ?? prev.diagnosticsUrl,
    diagnosticsCount: event.diagnosticsCount ?? prev.diagnosticsCount,
    warning: detailError ?? prev.warning,
    connectionInterrupted: false,
  }
}

export function applyTaskDetail(prev: UploadState, detail: TaskResponse): UploadState {
  const detailStatus = mapTaskStatusToUI(detail.status)
  const preserveResultState =
    (prev.status === 'completed' || prev.status === 'partial') &&
    (detailStatus === 'processing' ||
      detailStatus === 'idle' ||
      detailStatus === 'failed' ||
      (prev.status === 'completed' && detailStatus === 'partial'))
  const status = preserveResultState ? prev.status : detailStatus
  const mergeDetail = <T>(current: T | undefined, incoming: T | undefined) =>
    preserveResultState ? (current ?? incoming) : (incoming ?? current)

  return {
    ...prev,
    taskId: detail.id,
    progress: preserveResultState ? prev.progress : (detail.progress ?? prev.progress),
    stage: preserveResultState ? prev.stage : (detail.currentStage ?? detail.status ?? prev.stage),
    message: preserveResultState ? prev.message : (detail.currentMessage ?? prev.message),
    fileCount: mergeDetail(prev.fileCount, detail.artifacts?.fileCount),
    archiveSize: mergeDetail(prev.archiveSize, detail.artifacts?.archiveSize),
    downloadUrl: mergeDetail(prev.downloadUrl, detail.artifacts?.downloadUrl),
    reportUrl: mergeDetail(prev.reportUrl, detail.artifacts?.reportUrl),
    diagnosticsUrl: mergeDetail(prev.diagnosticsUrl, detail.artifacts?.diagnosticsUrl),
    diagnosticsCount: mergeDetail(prev.diagnosticsCount, detail.diagnosticsCount),
    recoveryScore: mergeDetail(prev.recoveryScore, detail.score),
    packageProfile: mergeDetail(prev.packageProfile, detail.profile),
    stages: mergeDetail(prev.stages, detail.stages),
    error: preserveResultState ? prev.error : (detail.errorMessage ?? prev.error),
    isComplete: preserveResultState
      ? prev.isComplete
      : detail.status === 'completed' || detail.status === 'partial',
    isUploading: status === 'processing',
    status,
    connectionInterrupted: false,
  }
}

export function mapTaskStatusToUI(status: string): UploadState['status'] {
  if (status === 'completed') {
    return 'completed'
  }
  if (status === 'partial') {
    return 'partial'
  }
  if (status === 'failed') {
    return 'failed'
  }
  if (status) {
    return 'processing'
  }
  return 'idle'
}
