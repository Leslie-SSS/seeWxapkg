import { useState } from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { ConfigPanel } from './ConfigPanel'

function ConfigPanelHarness({ requiresAppId = false }: { requiresAppId?: boolean }) {
  const [appId, setAppId] = useState('')
  const [beautify, setBeautify] = useState(true)
  const [decompile, setDecompile] = useState(true)

  return (
    <ConfigPanel
      appId={appId}
      setAppId={setAppId}
      beautify={beautify}
      setBeautify={setBeautify}
      decompile={decompile}
      setDecompile={setDecompile}
      requiresAppId={requiresAppId}
    />
  )
}

describe('ConfigPanel', () => {
  it('prompts for the AppID when the selected file is detected as encrypted', () => {
    render(<ConfigPanelHarness requiresAppId />)

    const notice = screen.getByRole('status')
    expect(notice).toHaveTextContent('检测到加密包')
    expect(notice).toHaveTextContent('需要对应的小程序 AppID 才能解密')
  })

  it('stays quiet about AppID for plain packages', () => {
    render(<ConfigPanelHarness />)
    expect(screen.queryByText('检测到加密包')).not.toBeInTheDocument()
  })
  it('provides full-size touch targets for switches and AppID clearing', () => {
    render(<ConfigPanelHarness />)

    expect(screen.queryByText('推荐设置')).not.toBeInTheDocument()
    expect(screen.getByText('仅加密包需要')).toBeInTheDocument()

    const switches = screen.getAllByRole('switch')
    expect(switches).toHaveLength(2)
    for (const control of switches) {
      expect(control).toHaveClass('h-12', 'w-16')
    }

    const appIdHelp = screen.getByText('AppID 在哪里找？').closest('details')
    expect(appIdHelp).toHaveAttribute('open')
    fireEvent.click(appIdHelp!.querySelector('summary')!)
    expect(appIdHelp).not.toHaveAttribute('open')

    const appId = screen.getByRole('textbox', { name: '小程序 AppID' })
    fireEvent.change(appId, { target: { value: 'wx0123456789abcdef' } })
    expect(appIdHelp).not.toHaveAttribute('open')

    const clear = screen.getByRole('button', { name: '清除 AppID' })
    expect(clear).toHaveClass('min-h-12')
    fireEvent.click(clear)
    expect(appId).toHaveValue('')
  })

  it('describes recommended decompilation in plain language and associates switch help', () => {
    render(<ConfigPanelHarness />)

    const deepDecompile = screen.getByRole('switch', { name: /深度反编译（推荐）：已开启/ })
    const description = screen.getByText('尽量还原页面、逻辑和样式，并标出不确定内容')

    expect(deepDecompile).toHaveAttribute('aria-describedby', description.id)
    expect(screen.getByText('普通包可留空。', { exact: false })).toBeInTheDocument()
  })
})
