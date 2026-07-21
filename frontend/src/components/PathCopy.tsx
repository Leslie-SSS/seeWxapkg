import { useState } from 'react'

interface PathCopyProps {
  platform: string
  path: string
}

export function PathCopy({ platform, path }: PathCopyProps) {
  const [copied, setCopied] = useState(false)

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(path)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch (err) {
      // Fallback for older browsers or non-secure contexts
      const textArea = document.createElement('textarea')
      textArea.value = path
      textArea.style.position = 'fixed'
      textArea.style.opacity = '0'
      document.body.appendChild(textArea)
      textArea.focus()
      textArea.select()
      try {
        document.execCommand('copy')
        setCopied(true)
        setTimeout(() => setCopied(false), 2000)
      } catch (e) {
        console.error('Copy failed', e)
      }
      document.body.removeChild(textArea)
    }
  }

  return (
    <div className="group flex w-full min-w-0 items-center gap-2" title={path}>
      <span className="shrink-0 text-sm text-slate-400">{platform}:</span>
      <code className="block min-w-0 flex-1 cursor-help truncate font-mono text-sm text-slate-300">
        {path}
      </code>
      <button
        onClick={handleCopy}
        type="button"
        aria-label={copied ? `${platform} 路径已复制` : `复制 ${platform} 路径`}
        className="inline-flex min-h-11 min-w-11 shrink-0 items-center justify-center rounded-lg text-slate-400 transition-colors hover:bg-slate-800 hover:text-emerald-300"
        title={copied ? '已复制' : '复制路径'}
      >
        {copied ? (
          <svg
            aria-hidden="true"
            className="h-4 w-4"
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
          >
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
          </svg>
        ) : (
          <svg
            aria-hidden="true"
            className="h-4 w-4"
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"
            />
          </svg>
        )}
      </button>
    </div>
  )
}
