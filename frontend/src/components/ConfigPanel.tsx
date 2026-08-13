import React from 'react'

interface ConfigPanelProps {
  appId: string
  setAppId: (value: string) => void
  beautify: boolean
  setBeautify: (value: boolean) => void
  decompile: boolean
  setDecompile: (value: boolean) => void
  requiresAppId?: boolean
  disabled?: boolean
}

export const ConfigPanel: React.FC<ConfigPanelProps> = ({
  appId,
  setAppId,
  beautify,
  setBeautify,
  decompile,
  setDecompile,
  requiresAppId = false,
  disabled = false,
}) => {
  return (
    <section
      aria-labelledby="options-title"
      className="task-surface tech-card space-y-4 rounded-2xl border border-slate-700/70 bg-slate-900/85 p-4 shadow-2xl shadow-slate-950/20 sm:p-5"
    >
      <div>
        <div className="min-w-0">
          <h2 id="options-title" className="text-base font-semibold text-slate-100">
            反编译选项
          </h2>
          <p className="mt-1 text-sm leading-6 text-slate-400">默认设置适合大多数文件。</p>
        </div>
      </div>

      <div className="space-y-2">
        <div className="flex items-center justify-between gap-3">
          <label htmlFor="appId" className="text-sm font-medium text-slate-200">
            小程序 AppID
          </label>
          <span className="rounded-full border border-slate-700 bg-slate-950/45 px-2.5 py-1 text-xs text-slate-400">
            仅加密包需要
          </span>
        </div>
        <div className="relative">
          <input
            id="appId"
            type="text"
            value={appId}
            onChange={(event) => setAppId(event.target.value)}
            placeholder="例如 wx1234567890abcdef"
            disabled={disabled}
            autoCapitalize="off"
            autoComplete="off"
            autoCorrect="off"
            spellCheck={false}
            className="min-h-12 w-full rounded-xl border border-slate-600 bg-slate-950/70 px-4 py-3 pr-16 font-mono text-sm text-slate-50 placeholder-slate-400 transition-colors focus:border-emerald-400 focus:outline-none focus:ring-2 focus:ring-emerald-400/20 disabled:cursor-not-allowed disabled:opacity-50"
          />
          {appId && (
            <button
              type="button"
              aria-label="清除 AppID"
              onClick={() => setAppId('')}
              disabled={disabled}
              className="absolute inset-y-0 right-0 min-h-12 rounded-r-xl px-4 text-sm text-slate-400 transition hover:bg-slate-800 hover:text-slate-200 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-emerald-400/60"
            >
              清除
            </button>
          )}
        </div>
        {requiresAppId && (
          <div
            className="rounded-xl border border-amber-400/30 bg-amber-400/10 p-4 text-sm text-amber-100"
            role="status"
          >
            <p className="font-semibold">检测到加密包</p>
            <p className="mt-1 leading-6 text-slate-300">
              该文件需要对应的小程序 AppID 才能解密，请在上方填写后重试。
            </p>
          </div>
        )}
        <details open className="group">
          <summary className="inline-flex min-h-11 cursor-pointer list-none items-center gap-1.5 py-2 text-sm text-slate-400 underline-offset-4 hover:text-emerald-300 hover:underline marker:hidden">
            AppID 在哪里找？
            <svg
              aria-hidden="true"
              className="h-4 w-4 transition group-open:rotate-180"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M19 9l-7 7-7-7"
              />
            </svg>
          </summary>
          <p className="rounded-lg border border-slate-700/70 bg-slate-950/45 p-3 text-sm leading-6 text-slate-400">
            普通包可留空。加密包请填写对应 AppID，可在小程序“更多资料”中查看。
          </p>
        </details>
      </div>

      <div className="divide-y divide-slate-800 overflow-hidden rounded-xl border border-slate-700/70 bg-slate-950/35">
        <OptionSwitch
          id="beautify"
          checked={beautify}
          onChange={setBeautify}
          disabled={disabled}
          label="整理代码排版"
          description="调整缩进和换行，让输出更容易阅读"
        />
        <OptionSwitch
          id="decompile"
          checked={decompile}
          onChange={setDecompile}
          disabled={disabled}
          label="深度反编译"
          description="尽量还原页面、逻辑和样式，并标出不确定内容"
          recommended
        />
      </div>
    </section>
  )
}

function OptionSwitch({
  id,
  checked,
  onChange,
  disabled,
  label,
  description,
  recommended = false,
}: {
  id: string
  checked: boolean
  onChange: (value: boolean) => void
  disabled: boolean
  label: string
  description: string
  recommended?: boolean
}) {
  const accessibleLabel = `${label}${recommended ? '（推荐）' : ''}`

  return (
    <div className="group/option flex min-w-0 items-center justify-between gap-4 px-4 py-3.5 transition-colors hover:bg-slate-900/40">
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <label htmlFor={id} className="text-sm font-medium text-slate-200">
            {label}
          </label>
          {recommended && (
            <span className="rounded-full bg-cyan-400/10 px-2 py-0.5 text-xs font-medium text-cyan-200">
              推荐
            </span>
          )}
        </div>
        <p id={`${id}-description`} className="mt-0.5 text-sm leading-5 text-slate-400">
          {description}
        </p>
      </div>
      <button
        id={id}
        type="button"
        role="switch"
        aria-checked={checked}
        aria-label={`${accessibleLabel}：${checked ? '已开启' : '已关闭'}`}
        aria-describedby={`${id}-description`}
        onClick={() => onChange(!checked)}
        disabled={disabled}
        className="relative h-12 w-16 shrink-0 rounded-full focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-emerald-400/60 focus-visible:ring-offset-2 focus-visible:ring-offset-slate-950 disabled:cursor-not-allowed disabled:opacity-50"
      >
        <span
          aria-hidden="true"
          className={`absolute inset-y-2 left-1 right-1 rounded-full border transition-colors ${
            checked ? 'border-emerald-300 bg-emerald-400' : 'border-slate-500 bg-slate-700'
          }`}
        >
          <span
            className={`absolute left-1 top-1 h-6 w-6 rounded-full bg-white shadow-sm transition-transform ${checked ? 'translate-x-6' : ''}`}
          />
        </span>
      </button>
    </div>
  )
}
