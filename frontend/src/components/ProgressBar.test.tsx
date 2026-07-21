import { render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { ProgressBar } from './ProgressBar'

describe('ProgressBar', () => {
  it('keeps file context and exposes the current processing step', () => {
    render(
      <ProgressBar
        progress={42}
        stage="recovering_js"
        message="正在整理页面逻辑"
        fileName="__APP__.wxapkg"
      />
    )

    expect(screen.getByText('__APP__.wxapkg')).toBeInTheDocument()
    const currentStep = screen.getByRole('listitem', { name: '反编译与整理：正在处理' })
    expect(currentStep).toHaveAttribute('aria-current', 'step')
    expect(within(currentStep).getByText('反编译')).toBeInTheDocument()
    expect(within(currentStep).getByText('反编译与整理')).toBeInTheDocument()
    expect(screen.getByRole('list', { name: '处理步骤' }).children).toHaveLength(5)
  })

  it('scales a full-width progress layer and clamps values to the valid range', () => {
    const { rerender } = render(
      <ProgressBar progress={42} stage="unpacking" message="正在提取文件" />
    )

    const progressbar = screen.getByRole('progressbar', { name: '任务处理进度' })
    const fill = screen.getByTestId('progress-fill')
    expect(progressbar).toHaveAttribute('aria-valuenow', '42')
    expect(fill).toHaveClass('w-full', 'progress-fill-smooth')
    expect(fill).toHaveStyle({ transform: 'scaleX(0.42)' })

    rerender(<ProgressBar progress={140} stage="packaging" message="正在打包" />)
    expect(progressbar).toHaveAttribute('aria-valuenow', '100')
    expect(fill).toHaveStyle({ transform: 'scaleX(1)' })
  })
})
