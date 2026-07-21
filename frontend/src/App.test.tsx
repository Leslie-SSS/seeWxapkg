import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App'

const { mockUseUpload, mockGetGithubStars } = vi.hoisted(() => ({
  mockUseUpload: vi.fn(),
  mockGetGithubStars: vi.fn(),
}))

vi.mock('./hooks/useSeeWxapkgUpload', () => ({
  useSeeWxapkgUpload: () => mockUseUpload(),
}))

vi.mock('./api/client', () => ({
  api: { getGithubStars: mockGetGithubStars },
}))

describe('App connection recovery notice', () => {
  beforeEach(() => {
    mockGetGithubStars.mockReset().mockReturnValue(new Promise(() => {}))
    mockUseUpload.mockReturnValue({
      isUploading: true,
      progress: 38,
      stage: 'recovering_js',
      message: '正在整理页面逻辑',
      status: 'processing',
      error: undefined,
      warning: undefined,
      connectionInterrupted: true,
      isComplete: false,
      upload: vi.fn(),
      reset: vi.fn(),
    })
  })

  it('explains that an interrupted connection is retrying without treating it as failure', () => {
    render(<App />)

    const notice = screen.getByRole('status')
    expect(notice).toHaveTextContent('连接中断，正在重试')
    expect(notice).toHaveTextContent('无需重新上传')
    expect(screen.queryByText('反编译未完成')).not.toBeInTheDocument()
  })

  it('uses deep decompilation and code formatting as the recommended default', () => {
    const upload = vi.fn()
    mockUseUpload.mockReturnValue({
      isUploading: false,
      progress: 0,
      stage: '',
      message: '',
      status: 'idle',
      error: undefined,
      warning: undefined,
      connectionInterrupted: false,
      isComplete: false,
      upload,
      reset: vi.fn(),
    })

    const { container } = render(<App />)
    expect(container.querySelectorAll('[data-screen]')).toHaveLength(1)
    expect(container.querySelector('[data-screen]')).toHaveAttribute('data-screen', 'landing')
    expect(screen.getByRole('heading', { name: /在线反编译\s*wxapkg/ })).toBeInTheDocument()
    expect(screen.getByText(/自动解包、反编译并整理为/)).toHaveTextContent('src/')
    const githubLink = screen.getByRole('link', { name: /在 GitHub 查看 See Wxapkg/ })
    expect(githubLink).toHaveAttribute('href', 'https://github.com/Leslie-SSS/seeWxapkg')
    expect(githubLink).toHaveAttribute('target', '_blank')
    expect(githubLink.closest('header')).not.toBeNull()
    expect(container.querySelector('footer')).toBeNull()
    expect(screen.queryByText('请仅处理你有权分析的文件')).not.toBeInTheDocument()
    expect(screen.queryByText('支持 .wxapkg')).not.toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'wxapkg 上传工作台' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '反编译流程' })).toBeInTheDocument()
    expect(screen.getAllByRole('listitem')).toHaveLength(7)
    const fileHelp = screen.getByText('wxapkg 文件在哪里？').closest('details')
    expect(fileHelp).toHaveAttribute('open')
    fireEvent.click(fileHelp!.querySelector('summary')!)
    expect(fileHelp).not.toHaveAttribute('open')

    const input = container.querySelector<HTMLInputElement>('input[type="file"]')
    const file = new File(['package'], '__APP__.wxapkg')
    expect(input).not.toBeNull()
    fireEvent.change(input!, { target: { files: [file] } })

    expect(container.querySelectorAll('[data-screen]')).toHaveLength(1)
    expect(container.querySelector('[data-screen]')).toHaveAttribute('data-screen', 'configure')
    expect(screen.getByText('准备开始')).toBeInTheDocument()

    const appIdHelp = screen.getByText('AppID 在哪里找？').closest('details')
    expect(appIdHelp).toHaveAttribute('open')

    const deepDecompile = screen.getByRole('switch', { name: /深度反编译（推荐）/ })
    expect(deepDecompile).toHaveAttribute('aria-checked', 'true')
    fireEvent.click(screen.getByRole('button', { name: '开始反编译' }))
    expect(upload).toHaveBeenCalledWith(file, undefined, true, true)
  })

  it('puts a failed task explanation before the retained file and focuses it for recovery', () => {
    const idleState = {
      isUploading: false,
      progress: 0,
      stage: '',
      message: '',
      status: 'idle',
      error: undefined,
      warning: undefined,
      connectionInterrupted: false,
      isComplete: false,
      upload: vi.fn(),
      reset: vi.fn(),
    }
    mockUseUpload.mockReturnValue(idleState)

    const { container, rerender } = render(<App />)
    const input = container.querySelector<HTMLInputElement>('input[type="file"]')
    const file = new File(['package'], '__APP__.wxapkg')
    fireEvent.change(input!, { target: { files: [file] } })

    mockUseUpload.mockReturnValue({
      ...idleState,
      stage: 'failed',
      status: 'failed',
      error: '这是加密包，需要提供正确的小程序 AppID 才能解密: encrypted package requires appID',
    })
    rerender(<App />)

    const failureTitle = screen.getByRole('heading', { name: '反编译未完成' })
    const uploadConsole = screen.getByRole('region', { name: 'wxapkg 上传工作台' })
    expect(failureTitle).toHaveFocus()
    expect(failureTitle.compareDocumentPosition(uploadConsole)).toBe(
      Node.DOCUMENT_POSITION_FOLLOWING
    )
    expect(
      screen.getByText('文件已保留；如为加密包，请重新确认 AppID 后重试。')
    ).toBeInTheDocument()
    expect(screen.getByText('这是加密包，需要填写正确的小程序 AppID。')).toBeInTheDocument()
    expect(screen.queryByText(/encrypted package requires appID/)).not.toBeInTheDocument()
    expect(screen.getByText('可直接重试')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '重新反编译' })).toBeEnabled()
    expect(container.querySelector('[data-screen]')).toHaveAttribute('data-screen', 'failed')
  })

  it('does not offer an active retry when a restored failed task has no local file', () => {
    mockUseUpload.mockReturnValue({
      isUploading: false,
      progress: 0,
      stage: 'failed',
      message: '',
      status: 'failed',
      error: '任务未完成',
      warning: undefined,
      connectionInterrupted: false,
      isComplete: false,
      upload: vi.fn(),
      reset: vi.fn(),
    })

    render(<App />)

    expect(screen.getByText('请选择文件，确认设置后再试一次。')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '重新反编译' })).toBeDisabled()
  })
})
