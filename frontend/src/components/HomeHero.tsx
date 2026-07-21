import type { ReactNode } from 'react'

const workflow = [
  {
    number: '01',
    title: '解包',
    description: '识别并提取文件',
  },
  {
    number: '02',
    title: '反编译',
    description: '还原页面、逻辑、样式',
  },
  {
    number: '03',
    title: '整理',
    description: '统一代码格式',
  },
  {
    number: '04',
    title: '检查',
    description: '标出待确认内容',
  },
]

interface HomeHeroProps {
  children: ReactNode
}

export function HomeHero({ children }: HomeHeroProps) {
  return (
    <>
      <section aria-labelledby="home-hero-title" className="hero-layout">
        <div className="hero-copy">
          <div className="hero-eyebrow hero-reveal">
            <span className="hero-eyebrow-dot" aria-hidden="true" />
            <span>静态反编译</span>
          </div>

          <h2 id="home-hero-title" className="hero-title hero-reveal hero-reveal-delay-1">
            在线反编译
            <span>wxapkg</span>
          </h2>

          <p className="hero-description hero-reveal hero-reveal-delay-2">
            上传 wxapkg，自动解包、反编译并整理为 <code>src/</code>，完成后直接下载。
          </p>
        </div>

        <div className="hero-console-wrap hero-reveal hero-reveal-delay-2">
          <div className="hero-orbit hero-orbit-one" aria-hidden="true" />
          <div className="hero-orbit hero-orbit-two" aria-hidden="true" />
          <UploadConsole>{children}</UploadConsole>
        </div>

        <ul className="hero-assurances hero-reveal hero-reveal-delay-3" aria-label="处理保障">
          <li>
            <CheckIcon />
            不执行包内代码
          </li>
          <li>
            <CheckIcon />
            不确定内容会标出
          </li>
          <li>
            <CheckIcon />
            文件定时清理
          </li>
        </ul>
      </section>

      <section
        aria-labelledby="workflow-title"
        className="workflow-panel hero-reveal hero-reveal-delay-3"
      >
        <div className="workflow-heading">
          <span aria-hidden="true">PIPELINE</span>
          <h2 id="workflow-title">反编译流程</h2>
        </div>
        <ol className="workflow-grid">
          {workflow.map((step) => (
            <li key={step.number} className="workflow-step">
              <span className="workflow-number" aria-hidden="true">
                {step.number}
              </span>
              <span className="workflow-copy">
                <strong>{step.title}</strong>
                <small>{step.description}</small>
              </span>
            </li>
          ))}
        </ol>
      </section>
    </>
  )
}

interface UploadConsoleProps {
  children: ReactNode
  compact?: boolean
}

export function UploadConsole({ children, compact = false }: UploadConsoleProps) {
  return (
    <section
      aria-label="wxapkg 上传工作台"
      className={`upload-console ${compact ? 'upload-console-compact' : ''}`}
    >
      <div className="console-toolbar" aria-hidden="true">
        <span className="console-dots">
          <i />
          <i />
          <i />
        </span>
        <span className="console-title">PACKAGE INPUT</span>
        <span className="console-mode">
          <i /> STATIC ONLY
        </span>
      </div>
      <div className="console-body">
        <span className="console-corner console-corner-tl" aria-hidden="true" />
        <span className="console-corner console-corner-tr" aria-hidden="true" />
        <span className="console-corner console-corner-bl" aria-hidden="true" />
        <span className="console-corner console-corner-br" aria-hidden="true" />
        {children}
      </div>
      <div className="console-footer">
        <span>WXAPKG INPUT</span>
        <span className="console-secure">
          <LockIcon />
          OUTPUT: SRC/
        </span>
      </div>
    </section>
  )
}

function CheckIcon() {
  return (
    <svg aria-hidden="true" viewBox="0 0 20 20" fill="none">
      <path d="m5.5 10 3 3 6-6" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
    </svg>
  )
}

function LockIcon() {
  return (
    <svg aria-hidden="true" viewBox="0 0 20 20" fill="none">
      <rect x="4.5" y="8.5" width="11" height="8" rx="2" stroke="currentColor" strokeWidth="1.4" />
      <path d="M7 8.5V6.75a3 3 0 0 1 6 0V8.5" stroke="currentColor" strokeWidth="1.4" />
    </svg>
  )
}
