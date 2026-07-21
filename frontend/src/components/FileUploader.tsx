import { useCallback, useEffect, useId, useRef, useState } from 'react'

interface FileUploaderProps {
  file?: File | null
  onFileSelect: (file: File | null) => void
  disabled?: boolean
  maxSize?: number
}

export const FileUploader: React.FC<FileUploaderProps> = ({
  file = null,
  onFileSelect,
  disabled = false,
  maxSize = 50 * 1024 * 1024,
}) => {
  const [isDragging, setIsDragging] = useState(false)
  const [error, setError] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)
  const dragDepthRef = useRef(0)
  const descriptionId = useId()
  const feedbackId = useId()

  useEffect(() => {
    if (!file && inputRef.current) {
      inputRef.current.value = ''
    }
  }, [file])

  const validate = useCallback(
    (nextFile: File): string => {
      if (!nextFile.name.toLowerCase().endsWith('.wxapkg')) {
        return '文件格式不正确，请选择 .wxapkg 文件'
      }
      if (nextFile.size > maxSize) {
        return `文件过大，最大支持 ${formatSize(maxSize)}`
      }
      return ''
    },
    [maxSize]
  )

  const selectFile = useCallback(
    (nextFile?: File) => {
      if (!nextFile) return
      const validationError = validate(nextFile)
      if (validationError) {
        setError(validationError)
        if (inputRef.current) inputRef.current.value = ''
        return
      }
      setError('')
      onFileSelect(nextFile)
    },
    [onFileSelect, validate]
  )

  const handleDragOver = useCallback(
    (event: React.DragEvent) => {
      event.preventDefault()
      if (!disabled) setIsDragging(true)
    },
    [disabled]
  )

  const handleDragEnter = useCallback(
    (event: React.DragEvent) => {
      event.preventDefault()
      if (disabled) return
      dragDepthRef.current += 1
      setIsDragging(true)
    },
    [disabled]
  )

  const handleDrop = useCallback(
    (event: React.DragEvent) => {
      event.preventDefault()
      dragDepthRef.current = 0
      setIsDragging(false)
      if (!disabled) selectFile(event.dataTransfer.files[0])
    },
    [disabled, selectFile]
  )

  const handleDragLeave = useCallback(() => {
    dragDepthRef.current = Math.max(0, dragDepthRef.current - 1)
    if (dragDepthRef.current === 0) setIsDragging(false)
  }, [])

  const visualState = error ? 'error' : isDragging ? 'dragging' : file ? 'selected' : 'idle'

  return (
    <div
      data-state={visualState}
      className={`upload-surface relative overflow-hidden rounded-2xl border transition ${
        error
          ? 'border-red-400/70 bg-red-400/5'
          : isDragging
            ? 'border-emerald-300 bg-emerald-400/10'
            : file
              ? 'border-emerald-400/50 bg-emerald-400/5'
              : 'border-slate-500/80 bg-slate-950/55 hover:border-emerald-400/60 hover:bg-slate-900/75'
      }`}
      onDragEnter={handleDragEnter}
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
    >
      <input
        ref={inputRef}
        type="file"
        accept=".wxapkg"
        disabled={disabled}
        className="hidden"
        onChange={(event) => selectFile(event.target.files?.[0])}
      />
      <button
        type="button"
        disabled={disabled}
        aria-describedby={`${descriptionId} ${feedbackId}`}
        onClick={() => inputRef.current?.click()}
        className="upload-trigger group/upload relative z-10 flex min-h-56 w-full flex-col items-center justify-center gap-4 px-5 py-8 text-center disabled:cursor-not-allowed disabled:opacity-50 sm:min-h-64"
      >
        <span
          className={`upload-icon flex h-16 w-16 items-center justify-center rounded-2xl transition ${
            error
              ? 'bg-red-400/10 text-red-300'
              : file
                ? 'bg-emerald-400/20 text-emerald-300'
                : 'bg-slate-800/90 text-slate-200'
          }`}
        >
          {file ? (
            <svg
              aria-hidden="true"
              className="h-7 w-7"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M5 13l4 4L19 7"
              />
            </svg>
          ) : (
            <svg
              aria-hidden="true"
              className="h-7 w-7"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M7 16a4 4 0 01-.88-7.903A5 5 0 1115.9 6L16 6a5 5 0 011 9.9M15 13l-3-3m0 0l-3 3m3-3v12"
              />
            </svg>
          )}
        </span>

        <span className="upload-copy min-w-0 max-w-full">
          {file ? (
            <>
              <span
                className="block max-w-full truncate font-mono text-sm font-medium text-emerald-300"
                title={file.name}
              >
                {file.name}
              </span>
              <span id={descriptionId} className="mt-1 block text-sm text-slate-400">
                {formatSize(file.size)} · 点击可更换文件
              </span>
            </>
          ) : (
            <>
              <span className="inline-flex items-center text-base font-semibold text-slate-50 sm:text-lg">
                选择 wxapkg 文件
              </span>
              <span id={descriptionId} className="mt-1 block text-sm text-slate-400">
                点击选择，或将文件拖到这里
              </span>
            </>
          )}
        </span>

        <span
          id={feedbackId}
          aria-live="polite"
          className={`upload-feedback text-sm ${error ? 'text-red-300' : file ? 'text-emerald-300' : 'text-slate-400'}`}
        >
          {error || (file ? '文件已就绪' : `最大支持 ${formatSize(maxSize)}`)}
        </span>
      </button>
    </div>
  )
}

function formatSize(bytes: number): string {
  if (bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  return `${(bytes / Math.pow(1024, index)).toFixed(1)} ${units[index]}`
}
