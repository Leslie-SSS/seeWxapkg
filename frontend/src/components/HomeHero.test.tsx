import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { HomeHero } from './HomeHero'

describe('HomeHero', () => {
  it('keeps the upload workbench before supporting assurances in reading order', () => {
    render(
      <HomeHero>
        <button type="button">选择测试文件</button>
      </HomeHero>
    )

    const uploadWorkbench = screen.getByRole('region', { name: 'wxapkg 上传工作台' })
    const assurances = screen.getByRole('list', { name: '处理保障' })

    expect(
      uploadWorkbench.compareDocumentPosition(assurances) & Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
    expect(screen.getByText('不执行包内代码')).toBeInTheDocument()
    expect(screen.getByText('不确定内容会标出')).toBeInTheDocument()
    expect(screen.getByText('文件定时清理')).toBeInTheDocument()
  })

  it('uses accurate plain-language workflow copy without repeating version or size labels', () => {
    render(
      <HomeHero>
        <span>上传入口</span>
      </HomeHero>
    )

    expect(screen.getByText('解包')).toBeInTheDocument()
    expect(screen.getByText(/自动解包、反编译并整理为/)).toHaveTextContent('src/')
    expect(screen.getByText('静态反编译')).toBeInTheDocument()
    expect(screen.getByText(/STATIC ONLY/)).toBeInTheDocument()
    expect(screen.getByText('OUTPUT: SRC/')).toBeInTheDocument()
    expect(screen.queryByText(/V2/)).not.toBeInTheDocument()
    expect(screen.queryByText(/MAX 50 MB/)).not.toBeInTheDocument()
    expect(screen.queryByText('安全解包')).not.toBeInTheDocument()
  })
})
