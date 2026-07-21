import { useEffect, useRef, useState } from 'react'
import { ConfigPanel } from './components/ConfigPanel'
import { FileUploader } from './components/FileUploader'
import { GithubStarLink } from './components/GithubStarLink'
import { HomeHero, UploadConsole } from './components/HomeHero'
import { PathCopy } from './components/PathCopy'
import { ProgressBar } from './components/ProgressBar'
import { ResultPanel } from './components/ResultPanel'
import { useSeeWxapkgUpload } from './hooks/useSeeWxapkgUpload'

type AppScreen = 'landing' | 'configure' | 'processing' | 'result' | 'failed'

function App() {
  const [appId, setAppId] = useState('')
  const [beautify, setBeautify] = useState(true)
  const [decompile, setDecompile] = useState(true)
  const [selectedFile, setSelectedFile] = useState<File | null>(null)
  const errorTitleRef = useRef<HTMLHeadingElement>(null)

  const {
    isUploading,
    progress,
    stage,
    message,
    status,
    fileCount,
    archiveSize,
    downloadUrl,
    reportUrl,
    diagnosticsUrl,
    diagnosticsCount,
    diagnostics,
    recoveryScore,
    packageProfile,
    stages,
    error,
    warning,
    connectionInterrupted,
    isComplete,
    taskId,
    upload,
    reset,
  } = useSeeWxapkgUpload()

  const showResult = isComplete || status === 'partial'
  const screen: AppScreen = showResult
    ? 'result'
    : isUploading
      ? 'processing'
      : status === 'failed'
        ? 'failed'
        : selectedFile
          ? 'configure'
          : 'landing'

  const screenWidth =
    screen === 'landing' ? 'max-w-6xl' : screen === 'result' ? 'max-w-4xl' : 'max-w-2xl'

  useEffect(() => {
    if (screen === 'failed') {
      errorTitleRef.current?.focus({ preventScroll: true })
    }
  }, [screen])

  useEffect(() => {
    if (taskId) setAppId('')
  }, [taskId])

  const handleReset = () => {
    setSelectedFile(null)
    setAppId('')
    reset()
  }

  const handleStart = () => {
    if (!selectedFile) return
    void upload(selectedFile, appId.trim() || undefined, beautify, decompile)
  }

  const handleFileSelect = (file: File | null) => {
    setSelectedFile(file)
    if (file) settleViewportAtTop()
  }

  return (
    <div className="app-background min-h-[100dvh] overflow-x-clip px-4 py-5 sm:px-6 sm:py-8">
      <div className="ambient-layer" aria-hidden="true">
        <span className="ambient-orb ambient-orb-left" />
        <span className="ambient-orb ambient-orb-right" />
        <span className="ambient-scan" />
      </div>

      <div className="relative mx-auto w-full max-w-6xl">
        <header className="site-header mb-8 sm:mb-10">
          <div className="flex min-w-0 items-center gap-3">
            <div className="brand-mark flex h-11 w-11 shrink-0 items-center justify-center rounded-xl bg-gradient-to-br from-emerald-300 via-emerald-400 to-cyan-400 text-slate-950 shadow-lg shadow-emerald-500/20">
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
                  d="M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4"
                />
              </svg>
            </div>
            <div className="min-w-0">
              <h1 className="whitespace-nowrap font-mono text-xl font-bold tracking-tight text-slate-50">
                See Wxapkg
              </h1>
              <p className="brand-subtitle mt-0.5 truncate text-xs tracking-wide text-slate-400 sm:text-sm">
                微信小程序反编译工具
              </p>
            </div>
          </div>
          <GithubStarLink />
        </header>

        <main>
          <div
            key={screen}
            data-screen={screen}
            className={`screen-shell screen-enter mx-auto w-full ${screenWidth} ${screen === 'configure' || screen === 'failed' ? 'configure-stage' : ''}`}
          >
            {screen === 'landing' && (
              <>
                <HomeHero>
                  <FileUploader
                    file={selectedFile}
                    onFileSelect={handleFileSelect}
                    disabled={false}
                  />
                </HomeHero>
                <FileLocationHelp />
              </>
            )}

            {(screen === 'configure' || screen === 'failed') && (
              <div className="space-y-4">
                {screen === 'failed' && (
                  <section
                    className="failure-card rounded-2xl border border-red-400/40 bg-red-400/10 p-4 sm:p-5"
                    role="alert"
                    aria-labelledby="failure-title"
                  >
                    <div className="flex items-start gap-3">
                      <span className="failure-icon" aria-hidden="true">
                        <svg fill="none" stroke="currentColor" viewBox="0 0 24 24">
                          <path
                            strokeLinecap="round"
                            strokeLinejoin="round"
                            strokeWidth={2}
                            d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
                          />
                        </svg>
                      </span>
                      <div className="min-w-0 flex-1">
                        <h2
                          id="failure-title"
                          ref={errorTitleRef}
                          tabIndex={-1}
                          className="font-semibold text-red-100"
                        >
                          反编译未完成
                        </h2>
                        <p className="mt-1 break-words text-sm leading-6 text-slate-300">
                          {getRecoveryErrorMessage(error)}
                        </p>
                        <p className="mt-1 text-sm leading-6 text-red-100/80">
                          {selectedFile
                            ? '文件已保留；如为加密包，请重新确认 AppID 后重试。'
                            : '请选择文件，确认设置后再试一次。'}
                        </p>
                        <button
                          type="button"
                          onClick={handleReset}
                          className="mt-2 inline-flex min-h-11 items-center text-sm font-medium text-red-200 underline-offset-4 transition-colors hover:text-white hover:underline"
                        >
                          重新选择文件
                        </button>
                      </div>
                    </div>
                  </section>
                )}

                <UploadConsole compact>
                  <FileUploader
                    file={selectedFile}
                    onFileSelect={handleFileSelect}
                    disabled={isUploading}
                  />
                </UploadConsole>

                <ConfigPanel
                  appId={appId}
                  setAppId={setAppId}
                  beautify={beautify}
                  setBeautify={setBeautify}
                  decompile={decompile}
                  setDecompile={setDecompile}
                />

                <ActionDock
                  retry={screen === 'failed'}
                  disabled={!selectedFile}
                  onStart={handleStart}
                />
              </div>
            )}

            {screen === 'processing' && (
              <div className="space-y-4" aria-live="polite">
                <ProgressBar
                  progress={progress}
                  stage={stage}
                  message={message}
                  fileName={selectedFile?.name}
                />
                {connectionInterrupted && (
                  <div
                    className="rounded-xl border border-amber-400/30 bg-amber-400/10 p-4 text-sm text-amber-100"
                    role="status"
                  >
                    <p className="font-semibold">连接中断，正在重试</p>
                    <p className="mt-1 leading-6 text-slate-300">
                      无需重新上传，连接恢复后会自动更新进度。
                    </p>
                  </div>
                )}
              </div>
            )}

            {screen === 'result' && (
              <div className="space-y-4">
                {warning && (
                  <div
                    className="rounded-xl border border-amber-400/30 bg-amber-400/10 p-4 text-sm text-amber-100"
                    role="status"
                  >
                    结果已生成，部分详情加载失败：{warning}
                  </div>
                )}
                <ResultPanel
                  status={status === 'partial' ? 'partial' : 'completed'}
                  fileName={selectedFile?.name}
                  fileCount={fileCount}
                  archiveSize={archiveSize}
                  downloadUrl={downloadUrl}
                  reportUrl={reportUrl}
                  diagnosticsUrl={diagnosticsUrl}
                  recoveryScore={recoveryScore}
                  diagnosticsCount={diagnosticsCount}
                  diagnostics={diagnostics}
                  packageProfile={packageProfile}
                  stages={stages}
                  onReset={handleReset}
                />
              </div>
            )}
          </div>
        </main>
      </div>
    </div>
  )
}

function settleViewportAtTop() {
  if (
    typeof window === 'undefined' ||
    typeof window.requestAnimationFrame !== 'function' ||
    !window.CSS?.supports?.('scroll-behavior', 'smooth')
  ) {
    return
  }

  window.requestAnimationFrame(() => {
    const reduceMotion = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false
    window.scrollTo({ top: 0, left: 0, behavior: reduceMotion ? 'auto' : 'smooth' })
  })
}

function getRecoveryErrorMessage(error?: string) {
  if (!error) return '反编译遇到问题，请检查设置后重试。'

  if (/encrypted package requires appid/i.test(error) || /加密包.*AppID/i.test(error)) {
    return '这是加密包，需要填写正确的小程序 AppID。'
  }

  return error
}

function FileLocationHelp() {
  return (
    <details open className="help-card group">
      <summary className="flex min-h-12 cursor-pointer list-none items-center justify-between gap-3 px-4 py-3 text-sm font-medium text-slate-300 marker:hidden">
        <span className="flex items-center gap-2">
          <svg
            aria-hidden="true"
            className="h-4 w-4 text-emerald-400"
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
            />
          </svg>
          wxapkg 文件在哪里？
        </span>
        <svg
          aria-hidden="true"
          className="h-5 w-5 shrink-0 text-slate-400 transition-transform duration-300 group-open:rotate-180"
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
        >
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
        </svg>
      </summary>
      <div className="space-y-3 border-t border-slate-800 px-4 pb-4 pt-3 text-sm leading-6 text-slate-300">
        <p>常见微信缓存目录：</p>
        <div className="min-w-0 space-y-2">
          <PathCopy
            platform="macOS"
            path="~/Library/Containers/com.tencent.xinWeChat/Data/Documents/app_data/radium/users/{一串值}/applet/packages"
          />
          <PathCopy
            platform="Windows"
            path={'C:\\Users\\{用户名}\\Documents\\WeChat Files\\Applet\\{AppID}'}
          />
        </div>
        <p className="text-sm text-slate-400">目录可能随微信版本变化；加密包还需填写 AppID。</p>
      </div>
    </details>
  )
}

function ActionDock({
  retry,
  disabled,
  onStart,
}: {
  retry: boolean
  disabled: boolean
  onStart: () => void
}) {
  return (
    <div className="configure-action-dock">
      <div className="action-dock-copy">
        <strong>{retry ? '可直接重试' : '准备开始'}</strong>
        <span>
          下载包只包含 <code>src/</code>
        </span>
      </div>
      <button type="button" onClick={onStart} disabled={disabled} className="tech-cta">
        <span className="tech-cta-content">
          <svg aria-hidden="true" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z"
            />
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
            />
          </svg>
          {retry ? '重新反编译' : '开始反编译'}
          <svg aria-hidden="true" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M5 12h14m-6-6 6 6-6 6"
            />
          </svg>
        </span>
      </button>
    </div>
  )
}

export default App
