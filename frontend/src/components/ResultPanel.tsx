import React, { useEffect, useRef } from 'react'
import type { Diagnostic, PackageProfile, RecoveryScore, StageResult } from '../api/client'
import {
  formatBytes,
  formatDuration,
  getDiagnosticGroups,
  getEngineCopy,
  getPackagePresentation,
  getRecoveryMetrics,
  getResultStatusCopy,
  getStageCopy,
  getStageStatusCopy,
  SCORE_TRUTH_NOTE,
  type ReportTone,
  type ResultStatus,
} from './reportPresentation'

interface ResultPanelProps {
  status: ResultStatus
  fileName?: string
  fileCount?: number
  archiveSize?: number
  downloadUrl?: string
  reportUrl?: string
  diagnosticsUrl?: string
  recoveryScore?: RecoveryScore
  diagnosticsCount?: number
  diagnostics?: Diagnostic[]
  packageProfile?: PackageProfile
  stages?: StageResult[]
  onReset: () => void
}

export const ResultPanel: React.FC<ResultPanelProps> = ({
  status,
  fileName,
  fileCount,
  archiveSize,
  downloadUrl,
  reportUrl,
  diagnosticsUrl,
  recoveryScore,
  diagnosticsCount,
  diagnostics = [],
  packageProfile,
  stages = [],
  onReset,
}) => {
  const titleRef = useRef<HTMLHeadingElement>(null)
  const didFocusTitleRef = useRef(false)
  const statusCopy = getResultStatusCopy(status)
  const attentionStages = stages.filter(
    (stage) =>
      stage.partial || stage.status === 'partial' || (!stage.success && stage.status !== 'pending')
  )
  const packagePresentation = packageProfile ? getPackagePresentation(packageProfile) : undefined
  const recoveryMetrics = recoveryScore ? getRecoveryMetrics(recoveryScore) : []
  const diagnosticGroups = getDiagnosticGroups(diagnostics)
  const attentionCount = diagnostics.filter((diagnostic) => diagnostic.severity !== 'info').length

  useEffect(() => {
    if (didFocusTitleRef.current) return
    didFocusTitleRef.current = true
    titleRef.current?.focus({ preventScroll: true })
  }, [])

  return (
    <section
      aria-labelledby="result-title"
      className="task-surface task-surface-result overflow-hidden rounded-2xl border border-slate-700/70 bg-slate-900/95 shadow-2xl shadow-slate-950/40"
    >
      <div className="border-b border-slate-800 bg-gradient-to-br from-slate-900 via-slate-900 to-slate-950 p-4 sm:p-6">
        <div className="flex min-w-0 flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
          <div className="flex min-w-0 items-start gap-3 sm:gap-4">
            <StatusIcon tone={statusCopy.tone} />
            <div className="min-w-0">
              <h2
                id="result-title"
                ref={titleRef}
                tabIndex={-1}
                className="text-lg font-semibold leading-7 text-slate-50 sm:text-xl"
              >
                {statusCopy.title}
              </h2>
              <p className="mt-1 max-w-2xl text-sm leading-6 text-slate-300">
                {statusCopy.description}
              </p>
              {fileName && (
                <p className="mt-2 truncate text-sm text-slate-400" title={fileName}>
                  本次文件：<span className="font-mono text-slate-300">{fileName}</span>
                </p>
              )}
            </div>
          </div>
          <span
            className={`w-fit shrink-0 rounded-full border px-3 py-1.5 text-sm font-medium ${toneBadgeClass(statusCopy.tone)}`}
          >
            {statusCopy.badge}
          </span>
        </div>

        <div className="mt-5 grid gap-3 sm:grid-cols-[minmax(0,1fr)_auto]">
          <DownloadAction url={downloadUrl} label="下载 src/" meta={formatBytes(archiveSize)} />
          <button
            type="button"
            onClick={onReset}
            className="inline-flex min-h-12 items-center justify-center gap-2 rounded-xl border border-slate-600 bg-slate-800 px-5 py-3 text-sm font-semibold text-slate-100 transition hover:border-slate-500 hover:bg-slate-700 active:scale-[0.98]"
          >
            <svg
              aria-hidden="true"
              className="h-5 w-5"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
              />
            </svg>
            继续反编译
          </button>
        </div>

        {(reportUrl || diagnosticsUrl) && (
          <nav
            aria-label="相关文件"
            className="mt-3 grid gap-2 rounded-xl border border-slate-700/70 bg-slate-950/35 p-2 text-sm sm:grid-cols-2"
          >
            {reportUrl && (
              <a
                href={reportUrl}
                download={`seewxapkg-report-${Date.now()}.json`}
                className="group/resource inline-flex min-h-12 items-center gap-3 rounded-lg px-3 py-2 text-slate-300 transition hover:bg-slate-800/80 hover:text-emerald-200"
              >
                <ResourceIcon type="report" />
                <span className="min-w-0 flex-1 font-medium">导出报告</span>
                <span className="rounded-full border border-slate-700 px-2 py-0.5 font-mono text-xs text-slate-400 group-hover/resource:border-emerald-400/30 group-hover/resource:text-emerald-200">
                  JSON
                </span>
              </a>
            )}
            {diagnosticsUrl && diagnosticsCount !== undefined && diagnosticsCount > 0 && (
              <a
                href={diagnosticsUrl}
                target="_blank"
                rel="noreferrer"
                className="group/resource inline-flex min-h-12 items-center gap-3 rounded-lg px-3 py-2 text-slate-300 transition hover:bg-slate-800/80 hover:text-amber-200"
              >
                <ResourceIcon type="diagnostics" />
                <span className="min-w-0 flex-1 font-medium">查看原始提示</span>
                <span className="rounded-full border border-slate-700 px-2 py-0.5 font-mono text-xs text-slate-400 group-hover/resource:border-amber-400/30 group-hover/resource:text-amber-200">
                  JSON
                </span>
              </a>
            )}
          </nav>
        )}

        <aside
          aria-label="下载包目录说明"
          className="mt-3 rounded-lg border border-slate-700/70 bg-slate-900/70 px-3 py-2.5 text-sm leading-6 text-slate-300"
        >
          ZIP 仅含 <code className="font-mono text-emerald-300">src/</code>
          ；报告可单独导出。
        </aside>
      </div>

      <div className="space-y-4 p-4 sm:p-6">
        <section aria-labelledby="summary-title">
          <div className="mb-3 flex items-center justify-between gap-3">
            <h3 id="summary-title" className="text-base font-semibold text-slate-100">
              结果概览
            </h3>
            <span className="text-sm text-slate-400">建议先下载保存</span>
          </div>
          <dl className="grid gap-3 sm:grid-cols-3">
            <SummaryMetric
              label="结果文件"
              value={fileCount !== undefined ? String(fileCount) : '—'}
              suffix={fileCount !== undefined ? '个' : undefined}
              help="src/ 中的文件数"
            />
            <SummaryMetric
              label="静态质量分"
              value={recoveryScore ? String(recoveryScore.overall) : '—'}
              suffix={recoveryScore ? '/100' : undefined}
              help="基于文件结构与语法检查，不是源码还原率"
              tone={recoveryScore && recoveryScore.overall < 80 ? 'warning' : 'success'}
            />
            <SummaryMetric
              label={diagnostics.length ? '待检查' : '处理提示'}
              value={
                diagnostics.length
                  ? String(attentionCount)
                  : diagnosticsCount !== undefined
                    ? String(diagnosticsCount)
                    : '—'
              }
              suffix={diagnostics.length || diagnosticsCount !== undefined ? '条' : undefined}
              help={
                diagnostics.length
                  ? `${diagnosticGroups.length} 类；报告共 ${diagnosticsCount ?? diagnostics.length} 条`
                  : '建议检查项和处理记录'
              }
              tone={
                (diagnostics.length ? attentionCount : diagnosticsCount) ? 'warning' : 'success'
              }
            />
          </dl>
        </section>

        {(status === 'partial' || attentionStages.length > 0 || recoveryScore?.fallbackUsed) && (
          <section
            aria-labelledby="attention-title"
            className="rounded-xl border border-amber-400/30 bg-amber-400/10 p-4"
          >
            <div className="flex items-start gap-3">
              <svg
                aria-hidden="true"
                className="mt-0.5 h-5 w-5 shrink-0 text-amber-300"
                fill="none"
                stroke="currentColor"
                viewBox="0 0 24 24"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M12 9v3.75m9-.75a9 9 0 11-18 0 9 9 0 0118 0zm-9 3.75h.008v.008H12v-.008z"
                />
              </svg>
              <div className="min-w-0">
                <h3 id="attention-title" className="font-semibold text-amber-100">
                  建议检查
                </h3>
                <p className="mt-1 text-sm leading-6 text-amber-100/80">
                  {recoveryScore?.fallbackUsed
                    ? '部分内容使用了辅助反编译，建议在微信开发者工具中检查关键页面。'
                    : '部分步骤未完整完成，请检查以下内容。'}
                </p>
                {diagnosticGroups.length > 0 ? (
                  <ul className="mt-3 space-y-3 text-sm text-amber-50/90">
                    {diagnosticGroups.slice(0, 6).map((group) => (
                      <li key={group.key} className="flex items-start gap-2">
                        <span
                          aria-hidden="true"
                          className={`mt-2 h-1.5 w-1.5 shrink-0 rounded-full ${group.tone === 'danger' ? 'bg-red-300' : 'bg-amber-300'}`}
                        />
                        <span className="min-w-0 break-words">
                          <strong className="font-medium">
                            {group.label} · {group.count} 条
                          </strong>
                          <span className="mt-0.5 block text-amber-100/80">
                            {group.description}
                          </span>
                        </span>
                      </li>
                    ))}
                  </ul>
                ) : attentionStages.length > 0 ? (
                  <ul className="mt-3 space-y-2 text-sm text-amber-50/90">
                    {attentionStages.slice(0, 4).map((stage, index) => {
                      const copy = getStageCopy(stage.stage)
                      return (
                        <li key={`${stage.stage}-${index}`} className="flex items-start gap-2">
                          <span
                            aria-hidden="true"
                            className="mt-2 h-1.5 w-1.5 shrink-0 rounded-full bg-amber-300"
                          />
                          <span className="min-w-0 break-words">
                            <strong className="font-medium">{copy.label}：</strong>
                            {copy.description}
                          </span>
                        </li>
                      )
                    })}
                  </ul>
                ) : null}
              </div>
            </div>
          </section>
        )}

        {recoveryScore && (
          <details open className="group rounded-xl border border-slate-700/70 bg-slate-950/40">
            <summary className="flex min-h-14 cursor-pointer list-none items-center justify-between gap-3 px-4 py-3 text-sm font-semibold text-slate-100 marker:hidden">
              <span>
                静态检查
                <span className="ml-2 font-normal text-slate-400">{recoveryScore.overall}/100</span>
              </span>
              <Chevron />
            </summary>
            <div className="border-t border-slate-800 px-4 pb-4 pt-3">
              <p className="rounded-lg border border-sky-400/20 bg-sky-400/10 px-3 py-2.5 text-sm leading-6 text-sky-100/90">
                {SCORE_TRUTH_NOTE}
              </p>
              <dl className="mt-3 grid gap-3 md:grid-cols-2">
                {recoveryMetrics.map((metric) => (
                  <div
                    key={metric.key}
                    className="rounded-lg border border-slate-800 bg-slate-900/70 p-3"
                  >
                    <div className="flex items-start justify-between gap-3">
                      <dt className="text-sm font-medium text-slate-200">{metric.label}</dt>
                      <dd className={`shrink-0 font-mono text-sm ${toneTextClass(metric.tone)}`}>
                        {metric.value}
                      </dd>
                    </div>
                    <p className="mt-1.5 text-sm leading-5 text-slate-400">{metric.help}</p>
                  </div>
                ))}
              </dl>
            </div>
          </details>
        )}

        {packagePresentation && (
          <details open className="group rounded-xl border border-slate-700/70 bg-slate-950/40">
            <summary className="flex min-h-14 cursor-pointer list-none items-center justify-between gap-3 px-4 py-3 text-sm font-semibold text-slate-100 marker:hidden">
              <span>
                文件信息
                <span className="ml-2 font-normal text-slate-400">
                  {packagePresentation.variant}
                </span>
              </span>
              <Chevron />
            </summary>
            <div className="border-t border-slate-800 px-4 pb-4 pt-3">
              <p className="text-sm leading-6 text-slate-400">
                根据包头和文件结构识别，仅用于选择处理方式。
              </p>
              <div className="mt-3 flex flex-wrap gap-2">
                {packagePresentation.features.map((feature) => (
                  <span
                    key={feature}
                    className="rounded-full border border-slate-700 bg-slate-900 px-3 py-1.5 text-sm text-slate-300"
                  >
                    {feature}
                  </span>
                ))}
              </div>
            </div>
          </details>
        )}

        {stages.length > 0 && (
          <details open className="group rounded-xl border border-slate-700/70 bg-slate-950/40">
            <summary className="flex min-h-14 cursor-pointer list-none items-center justify-between gap-3 px-4 py-3 text-sm font-semibold text-slate-100 marker:hidden">
              <span>
                反编译过程
                <span className="ml-2 font-normal text-slate-400">
                  {stages.length} 个步骤
                  {attentionStages.length ? ` · ${attentionStages.length} 个需检查` : ''}
                </span>
              </span>
              <Chevron />
            </summary>
            <ol className="space-y-2 border-t border-slate-800 px-4 pb-4 pt-3">
              {stages.map((stage, index) => {
                const copy = getStageCopy(stage.stage)
                const stageStatus = getStageStatusCopy(stage)
                const meta = [
                  getEngineCopy(stage.engine),
                  formatDuration(stage.durationMs),
                  stage.attempt && stage.attempt > 1 ? `第 ${stage.attempt} 次处理` : '',
                ].filter(Boolean)

                return (
                  <li
                    key={`${stage.stage}-${index}`}
                    className="flex min-w-0 items-start gap-3 rounded-lg border border-slate-800 bg-slate-900/70 p-3"
                  >
                    <span
                      aria-hidden="true"
                      className={`mt-2 h-2.5 w-2.5 shrink-0 rounded-full ${toneDotClass(stageStatus.tone)}`}
                    />
                    <div className="min-w-0 flex-1">
                      <div className="flex min-w-0 items-start justify-between gap-3">
                        <div className="min-w-0">
                          <p className="font-medium text-slate-200">{copy.label}</p>
                          <p className="mt-0.5 break-words text-sm leading-5 text-slate-400">
                            {copy.description}
                          </p>
                        </div>
                        <span
                          className={`shrink-0 rounded-full px-2.5 py-1 text-xs font-medium ${toneBadgeClass(stageStatus.tone)}`}
                        >
                          {stageStatus.label}
                        </span>
                      </div>
                      {meta.length > 0 && (
                        <p className="mt-2 text-xs text-slate-400">{meta.join(' · ')}</p>
                      )}
                    </div>
                  </li>
                )
              })}
            </ol>
          </details>
        )}
      </div>
    </section>
  )
}

function ResourceIcon({ type }: { type: 'report' | 'diagnostics' }) {
  const path =
    type === 'report'
      ? 'M7 3.75h7.25L18 7.5v12.75H7V3.75Zm7 0V8h4M9.75 12h5.5M9.75 15.5h5.5'
      : 'M12 3.75a8.25 8.25 0 1 0 0 16.5 8.25 8.25 0 0 0 0-16.5ZM12 8v4.25m0 3.25h.01'

  return (
    <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-slate-700 bg-slate-900 text-slate-400 transition-colors group-hover/resource:text-current">
      <svg
        aria-hidden="true"
        className="h-[18px] w-[18px]"
        fill="none"
        stroke="currentColor"
        viewBox="0 0 24 24"
      >
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.8} d={path} />
      </svg>
    </span>
  )
}

function DownloadAction({ url, label, meta }: { url?: string; label: string; meta?: string }) {
  const className =
    'inline-flex min-h-12 w-full items-center justify-center gap-2 rounded-xl bg-emerald-400 px-5 py-3 text-sm font-bold text-slate-950 shadow-lg shadow-emerald-500/20 transition hover:bg-emerald-300 active:scale-[0.99]'

  if (!url) {
    return (
      <span aria-disabled="true" className={`${className} cursor-not-allowed opacity-45`}>
        下载文件暂未就绪
      </span>
    )
  }

  return (
    <a href={url} download={`wxapkg-result-${Date.now()}.zip`} className={className}>
      <svg
        aria-hidden="true"
        className="h-5 w-5"
        fill="none"
        stroke="currentColor"
        viewBox="0 0 24 24"
      >
        <path
          strokeLinecap="round"
          strokeLinejoin="round"
          strokeWidth={2}
          d="M12 3v12m0 0l-4-4m4 4l4-4M5 17v2a2 2 0 002 2h10a2 2 0 002-2v-2"
        />
      </svg>
      <span>{label}</span>
      {meta && <span className="font-normal text-slate-800/75">· {meta}</span>}
    </a>
  )
}

function SummaryMetric({
  label,
  value,
  suffix,
  help,
  tone = 'neutral',
}: {
  label: string
  value: string
  suffix?: string
  help: string
  tone?: ReportTone
}) {
  return (
    <div className="rounded-xl border border-slate-800 bg-slate-950/50 p-4">
      <dt className="text-sm font-medium text-slate-400">{label}</dt>
      <dd className={`mt-1 font-mono text-2xl font-semibold ${toneTextClass(tone)}`}>
        {value}
        {suffix && <span className="ml-1 text-sm font-normal text-slate-400">{suffix}</span>}
      </dd>
      <p className="mt-2 text-sm leading-5 text-slate-400">{help}</p>
    </div>
  )
}

function StatusIcon({ tone }: { tone: ReportTone }) {
  const path =
    tone === 'success'
      ? 'M5 13l4 4L19 7'
      : tone === 'danger'
        ? 'M6 18L18 6M6 6l12 12'
        : 'M12 9v3m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z'

  return (
    <span
      className={`flex h-11 w-11 shrink-0 items-center justify-center rounded-xl ${toneIconClass(tone)}`}
    >
      <svg
        aria-hidden="true"
        className="h-6 w-6"
        fill="none"
        stroke="currentColor"
        viewBox="0 0 24 24"
      >
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d={path} />
      </svg>
    </span>
  )
}

function Chevron() {
  return (
    <svg
      aria-hidden="true"
      className="h-5 w-5 shrink-0 text-slate-400 transition group-open:rotate-180"
      fill="none"
      stroke="currentColor"
      viewBox="0 0 24 24"
    >
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
    </svg>
  )
}

function toneBadgeClass(tone: ReportTone) {
  switch (tone) {
    case 'success':
      return 'border-emerald-400/40 bg-emerald-400/10 text-emerald-200'
    case 'warning':
      return 'border-amber-400/40 bg-amber-400/10 text-amber-200'
    case 'danger':
      return 'border-red-400/40 bg-red-400/10 text-red-200'
    default:
      return 'border-slate-600 bg-slate-800 text-slate-200'
  }
}

function toneTextClass(tone?: ReportTone) {
  switch (tone) {
    case 'success':
      return 'text-emerald-300'
    case 'warning':
      return 'text-amber-300'
    case 'danger':
      return 'text-red-300'
    default:
      return 'text-slate-100'
  }
}

function toneDotClass(tone: ReportTone) {
  switch (tone) {
    case 'success':
      return 'bg-emerald-400'
    case 'warning':
      return 'bg-amber-400'
    case 'danger':
      return 'bg-red-400'
    default:
      return 'bg-slate-500'
  }
}

function toneIconClass(tone: ReportTone) {
  switch (tone) {
    case 'success':
      return 'bg-emerald-400/20 text-emerald-300'
    case 'warning':
      return 'bg-amber-400/20 text-amber-300'
    case 'danger':
      return 'bg-red-400/20 text-red-300'
    default:
      return 'bg-slate-800 text-slate-300'
  }
}
