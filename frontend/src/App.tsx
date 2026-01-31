import { useState } from 'react';
import { FileUploader } from './components/FileUploader';
import { ConfigPanel } from './components/ConfigPanel';
import { ProgressBar } from './components/ProgressBar';
import { ResultPanel } from './components/ResultPanel';
import { PathCopy } from './components/PathCopy';
import { useSeeWxapkgUpload } from './hooks/useSeeWxapkgUpload';

function App() {
  const [appId, setAppId] = useState('');
  const [beautify, setBeautify] = useState(true);
  const [selectedFile, setSelectedFile] = useState<File | null>(null);

  const {
    isUploading,
    progress,
    stage,
    message,
    fileCount,
    downloadUrl,
    error,
    isComplete,
    upload,
    reset,
  } = useSeeWxapkgUpload();

  return (
    <div className="min-h-screen flex items-center justify-center p-4" style={{
      backgroundImage: 'linear-gradient(to right, rgba(255,255,255,0.03) 1px, transparent 1px), linear-gradient(to bottom, rgba(255,255,255,0.03) 1px, transparent 1px)',
      backgroundSize: '32px 32px'
    }}>
      <div className="w-full max-w-2xl">
        {/* Header */}
        <header className="text-center mb-6">
          <div className="inline-flex items-center gap-3 mb-3">
            <div
              className="w-10 h-10 rounded-lg flex items-center justify-center"
              style={{
                background: 'linear-gradient(to bottom right, #10b981, #34d399)',
                boxShadow: '0 0 12px rgba(16, 185, 129, 0.15)'
              }}
            >
              <svg className="w-5 h-5 text-slate-950" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4" />
              </svg>
            </div>
            <h1 className="text-xl font-mono font-bold text-slate-100">See Wxapkg</h1>
          </div>
          <p className="text-sm text-slate-400">微信小程序反编译工具</p>
        </header>

        {/* Main */}
        <main className="space-y-4">
          {/* 使用指引 */}
          {!selectedFile && !isUploading && !isComplete && (
            <div className="bg-slate-900/50 rounded-xl border border-slate-800/50 p-4 space-y-3">
              <div className="flex items-center gap-2 text-sm font-medium text-slate-300">
                <svg className="w-4 h-4 text-emerald-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                </svg>
                使用说明
              </div>
              <ul className="space-y-2 text-sm text-slate-400">
                <li className="flex items-start gap-2">
                  <span className="text-emerald-500 mt-0.5">1.</span>
                  <div className="flex-1 space-y-2">
                    <p>导出 <span className="font-mono text-slate-300">.wxapkg</span> 文件：</p>
                    <div className="space-y-1.5">
                      <PathCopy platform="macOS" path="~/Library/Containers/com.tencent.xinWeChat/Data/Documents/app_data/radium/Applet/packages" />
                      <PathCopy platform="Windows" path="C:\Users\{用户名}\Documents\WeChat Files\Applet\{AppID}" />
                      <p className="text-xs text-slate-400">注：目录可能随微信版本变动，请自行确认最新路径</p>
                    </div>
                  </div>
                </li>
                <li className="flex items-start gap-2">
                  <span className="text-emerald-500 mt-0.5">2.</span>
                  <span>加密包需填写 AppID（小程序页面路径中可找到）</span>
                </li>
                <li className="flex items-start gap-2">
                  <span className="text-emerald-500 mt-0.5">3.</span>
                  <span>点击"开始反编译"完成后，下载 ZIP 包即可获得源码</span>
                </li>
              </ul>
            </div>
          )}

          {/* Upload */}
          <FileUploader
            onFileSelect={setSelectedFile}
            disabled={isUploading || isComplete}
          />

          {/* Config */}
          {!isComplete && !isUploading && selectedFile && (
            <div className="animate-fade-in">
              <ConfigPanel
                appId={appId}
                setAppId={setAppId}
                beautify={beautify}
                setBeautify={setBeautify}
              />
            </div>
          )}

          {/* Start Button */}
          {!isComplete && !isUploading && selectedFile && (
            <button
              onClick={() => upload(selectedFile, appId || undefined, beautify)}
              className="w-full inline-flex items-center justify-center gap-2 px-5 py-2.5 bg-emerald-500 text-slate-950 rounded-lg font-medium transition-all duration-200 hover:bg-emerald-600 hover:shadow-lg hover:shadow-emerald-500/25 active:scale-[0.97] focus-visible:ring-2 focus-visible:ring-emerald-500 focus-visible:ring-offset-2 focus-visible:ring-offset-slate-950 animate-fade-in"
            >
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z" />
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
              开始反编译
            </button>
          )}

          {/* Progress */}
          {isUploading && (
            <div className="animate-fade-in">
              <ProgressBar progress={progress} stage={stage} message={message} />
            </div>
          )}

          {/* Error */}
          {error && (
            <div className="bg-slate-900 rounded-xl border border-red-500/50 bg-red-500/5 p-4 animate-fade-in" role="alert">
              <div className="flex items-start gap-3">
                <svg className="w-5 h-5 text-red-400 flex-shrink-0 mt-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                </svg>
                <div className="flex-1">
                  <p className="text-sm font-medium text-red-400">处理失败</p>
                  <p className="text-xs text-slate-500 mt-1">{error}</p>
                </div>
                <button
                  onClick={reset}
                  className="inline-flex items-center justify-center gap-2 px-4 py-2 bg-transparent text-slate-500 rounded-lg font-medium transition-all duration-200 hover:bg-slate-800 hover:text-slate-300 active:scale-[0.97]"
                >
                  关闭
                </button>
              </div>
            </div>
          )}

          {/* Result */}
          {isComplete && fileCount && downloadUrl && (
            <div className="animate-scale-in">
              <ResultPanel
                fileCount={fileCount}
                downloadUrl={downloadUrl}
                onReset={() => {
                  setSelectedFile(null);
                  reset();
                }}
              />
            </div>
          )}
        </main>

        {/* Footer */}
        <footer className="text-center mt-6 space-y-2">
          <p className="text-sm text-slate-400">
            文件在服务器本地处理，下载后自动删除
          </p>
          <div className="flex items-center justify-center gap-4 text-sm text-slate-500">
            <a
              href="https://github.com/Leslie-SSS/seeWxapkg"
              target="_blank"
              rel="noopener noreferrer"
              className="flex items-center gap-1 hover:text-slate-300 transition-colors"
            >
              <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 24 24">
                <path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z"/>
              </svg>
              GitHub
            </a>
            <span className="flex items-center gap-1">
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
              支持 .wxapkg 格式
            </span>
            <span className="flex items-center gap-1">
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" />
              </svg>
              数据不留存
            </span>
          </div>
        </footer>
      </div>
    </div>
  );
}

export default App;
