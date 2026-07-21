import { fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { ResultPanel } from './ResultPanel'

describe('ResultPanel', () => {
  it('puts primary actions before the plain-language report and avoids raw terms', () => {
    const onReset = vi.fn()
    const { container } = render(
      <ResultPanel
        status="partial"
        fileName="__APP__.wxapkg"
        fileCount={12}
        archiveSize={790716}
        downloadUrl="/api/download/task-1"
        reportUrl="/api/tasks/task-1/report"
        diagnosticsUrl="/api/tasks/task-1/diagnostics"
        diagnosticsCount={7}
        diagnostics={[
          {
            code: 'recover.js.page.missing_runtime',
            severity: 'warn',
            message: '页面 JS 缺失',
            file: 'pages/a.js',
          },
          {
            code: 'recover.js.page.missing_runtime',
            severity: 'warn',
            message: '页面 JS 缺失',
            file: 'pages/b.js',
          },
          {
            code: 'recover.wxml.page.missing_runtime',
            severity: 'warn',
            message: '页面 WXML 缺失',
            file: 'pages/a.wxml',
          },
          {
            code: 'classifier.variant',
            severity: 'info',
            message: '已识别包类型',
          },
        ]}
        recoveryScore={{
          overall: 68,
          manifest: 100,
          js: 61,
          wxml: 54,
          wxss: 52,
          decompileHit: true,
          fallbackUsed: true,
          generatedRatio: 37,
          fallbackPenalty: 10,
          verifierPassed: false,
        }}
        packageProfile={{
          isEncrypted: true,
          isStandardWxapkg: true,
          isWeChat4xLike: true,
          isSubPackage: false,
          isGamePackage: false,
          hasAppConfigJSON: true,
          hasAppServiceJS: true,
          hasWorkersJS: false,
          hasPageFrameHTML: true,
          hasPageFrameJS: false,
          hasAppWxssJS: false,
          indexFileCount: 51,
          suspectedVariant: 'wechat4x',
        }}
        stages={[
          {
            stage: 'recovering_wxml',
            success: false,
            partial: true,
            status: 'partial',
            message: '部分页面结构需要人工检查',
            durationMs: 120,
            attempt: 1,
            engine: 'native',
          },
          {
            stage: 'classifying',
            success: true,
            partial: false,
            status: 'success',
          },
          {
            stage: 'classifying',
            success: true,
            partial: false,
            status: 'success',
          },
        ]}
        onReset={onReset}
      />
    )

    expect(
      screen.getByRole('heading', { name: '反编译结果已生成，部分内容需检查' })
    ).toBeInTheDocument()
    expect(screen.getByText('可下载 · 需检查')).toBeInTheDocument()

    const download = screen.getByRole('link', { name: /下载 src\// })
    const continueButton = screen.getByRole('button', { name: '继续反编译' })
    const summary = screen.getByRole('heading', { name: '结果概览' })
    expect(
      download.compareDocumentPosition(summary) & Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
    expect(
      continueButton.compareDocumentPosition(summary) & Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
    expect(download).toHaveAttribute('href', '/api/download/task-1')

    const technicalResources = screen.getByRole('navigation', { name: '相关文件' })
    expect(
      within(technicalResources).getByRole('link', { name: /导出报告.*JSON/ })
    ).toHaveAttribute('href', '/api/tasks/task-1/report')
    expect(
      within(technicalResources).getByRole('link', { name: /查看原始提示.*JSON/ })
    ).toHaveAttribute('href', '/api/tasks/task-1/diagnostics')

    const detailCards = Array.from(container.querySelectorAll('details'))
    expect(detailCards).toHaveLength(3)
    for (const card of detailCards) {
      expect(card).toHaveAttribute('open')
    }
    fireEvent.click(detailCards[0].querySelector('summary')!)
    expect(detailCards[0]).not.toHaveAttribute('open')

    fireEvent.click(continueButton)
    expect(onReset).toHaveBeenCalledTimes(1)

    expect(screen.getByText('静态质量分')).toBeInTheDocument()
    expect(
      screen.getByText('不是源码还原率，也不代表项目可直接运行。', { exact: false })
    ).toBeInTheDocument()
    expect(screen.getByText('页面文件不齐率')).toBeInTheDocument()
    expect(screen.getByText('37%')).toBeInTheDocument()
    expect(screen.getByText('页面逻辑需要检查 · 2 条')).toBeInTheDocument()
    expect(screen.getByText('页面结构需要检查 · 1 条')).toBeInTheDocument()
    expect(screen.getByText('2 类；报告共 7 条')).toBeInTheDocument()
    expect(screen.queryByText('生成占比')).not.toBeInTheDocument()
    expect(screen.queryByText('used')).not.toBeInTheDocument()
    expect(screen.queryByText('PARTIAL')).not.toBeInTheDocument()
    expect(screen.queryByText('attempt #1')).not.toBeInTheDocument()
    expect(screen.getByLabelText('下载包目录说明')).toHaveTextContent(
      'ZIP 仅含 src/；报告可单独导出。'
    )
  })

  it('describes a completed result without promising semantic equivalence', () => {
    const { container } = render(
      <ResultPanel
        status="completed"
        fileCount={0}
        downloadUrl="/api/download/task-2"
        recoveryScore={{
          overall: 100,
          manifest: 100,
          js: 100,
          wxml: 100,
          wxss: 100,
          decompileHit: false,
          fallbackUsed: false,
          generatedRatio: 0,
          fallbackPenalty: 0,
          verifierPassed: true,
        }}
        onReset={vi.fn()}
      />
    )

    expect(screen.getByRole('heading', { name: '反编译结果已生成' })).toBeInTheDocument()
    expect(screen.getByText('建议在微信开发者工具中检查关键页面。')).toBeInTheDocument()
    expect(screen.getByText('结果文件')).toBeInTheDocument()
    expect(screen.getByText('src/ 中的文件数')).toBeInTheDocument()
    expect(container.querySelector('section[aria-labelledby="result-title"]')).toHaveClass(
      'task-surface'
    )
    expect(
      screen.getByText('不是源码还原率，也不代表项目可直接运行。', { exact: false })
    ).toBeInTheDocument()
  })

  it('focuses the result heading only once without moving the viewport', () => {
    const focus = vi.spyOn(HTMLElement.prototype, 'focus')

    const { rerender } = render(
      <ResultPanel status="completed" fileCount={2} downloadUrl="/download" onReset={vi.fn()} />
    )

    expect(focus).toHaveBeenCalledTimes(1)
    expect(focus).toHaveBeenCalledWith({ preventScroll: true })

    rerender(
      <ResultPanel status="partial" fileCount={2} downloadUrl="/download" onReset={vi.fn()} />
    )
    expect(focus).toHaveBeenCalledTimes(1)

    focus.mockRestore()
  })
})
