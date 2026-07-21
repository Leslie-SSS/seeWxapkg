import React from 'react'

interface ProgressBarProps {
  progress: number
  stage: string
  message: string
  fileName?: string
}

const userSteps = [
  {
    key: 'upload',
    shortLabel: '上传',
    label: '上传文件',
    stages: ['uploading', 'queued', 'processing'],
  },
  {
    key: 'identify',
    shortLabel: '识别',
    label: '识别与解密',
    stages: ['classifying', 'decrypting'],
  },
  {
    key: 'extract',
    shortLabel: '解包',
    label: '解包文件',
    stages: ['unpacking', 'normalizing'],
  },
  {
    key: 'recover',
    shortLabel: '反编译',
    label: '反编译与整理',
    stages: [
      'recovering_manifest',
      'recovering_js',
      'recovering_wxml',
      'recovering_wxss',
      'fallback_recovering',
      'formatting',
    ],
  },
  {
    key: 'finish',
    shortLabel: '下载',
    label: '检查并打包',
    stages: ['verifying', 'packaging'],
  },
]

export const ProgressBar: React.FC<ProgressBarProps> = ({ progress, stage, message, fileName }) => {
  const safeProgress = Math.min(Math.max(progress, 0), 100)
  const isError = stage === 'failed' || stage === 'error'
  const currentStepIndex = userSteps.findIndex((step) => step.stages.includes(stage))
  const currentLabel = currentStepIndex >= 0 ? userSteps[currentStepIndex].label : '处理中'

  return (
    <section
      aria-labelledby="progress-title"
      className="task-surface rounded-2xl border border-slate-700/70 bg-slate-900/90 p-4 shadow-2xl shadow-slate-950/30 sm:p-5"
    >
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <h2 id="progress-title" className="text-base font-semibold text-slate-100">
            {isError ? '处理遇到问题' : currentLabel}
          </h2>
          <p
            className={`mt-1 break-words text-sm leading-6 ${isError ? 'text-red-300' : 'text-slate-400'}`}
          >
            {message || '正在反编译，请不要关闭页面…'}
          </p>
          {fileName && (
            <p className="mt-2 truncate text-sm text-slate-400" title={fileName}>
              本次文件：<span className="font-mono text-slate-300">{fileName}</span>
            </p>
          )}
        </div>
        <span
          className={`shrink-0 font-mono text-lg font-semibold ${isError ? 'text-red-300' : 'text-emerald-300'}`}
        >
          {Math.round(safeProgress)}%
        </span>
      </div>

      <div
        className="progress-track mt-4 h-2.5 w-full overflow-hidden rounded-full bg-slate-800"
        role="progressbar"
        aria-label="任务处理进度"
        aria-valuenow={safeProgress}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuetext={`${currentLabel}，已完成 ${Math.round(safeProgress)}%`}
      >
        <div
          data-testid="progress-fill"
          className={`progress-fill-smooth h-full w-full rounded-full ${isError ? 'bg-red-400' : 'bg-gradient-to-r from-emerald-400 to-teal-400'}`}
          style={{ transform: `scaleX(${safeProgress / 100})` }}
        />
      </div>

      <ol className="progress-steps mt-5 grid grid-cols-5 gap-1 sm:gap-3" aria-label="处理步骤">
        {userSteps.map((step, index) => {
          const state = isError
            ? 'error'
            : index < currentStepIndex
              ? 'done'
              : index === currentStepIndex
                ? 'active'
                : 'pending'
          return (
            <li
              key={step.key}
              aria-current={state === 'active' ? 'step' : undefined}
              aria-label={`${step.label}：${stepStateLabel(state)}`}
              className="flex min-w-0 flex-col items-center gap-1.5 text-center text-[11px] sm:flex-row sm:gap-2 sm:text-left sm:text-sm"
            >
              <span
                aria-hidden="true"
                className={`progress-step-dot h-2.5 w-2.5 shrink-0 rounded-full ${
                  state === 'error'
                    ? 'bg-red-400'
                    : state === 'done'
                      ? 'bg-emerald-400'
                      : state === 'active'
                        ? 'bg-emerald-300 motion-safe:animate-pulse'
                        : 'bg-slate-600'
                }`}
              />
              <span
                aria-hidden="true"
                className={`min-w-0 ${state === 'active' ? 'font-medium text-slate-200' : state === 'done' ? 'text-slate-300' : 'text-slate-400'}`}
              >
                <span className="sm:hidden">{step.shortLabel}</span>
                <span className="hidden truncate sm:inline">{step.label}</span>
              </span>
            </li>
          )
        })}
      </ol>
    </section>
  )
}

function stepStateLabel(state: 'error' | 'done' | 'active' | 'pending') {
  switch (state) {
    case 'error':
      return '处理遇到问题'
    case 'done':
      return '已完成'
    case 'active':
      return '正在处理'
    default:
      return '等待处理'
  }
}
