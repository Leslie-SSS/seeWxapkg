import { fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { FileUploader } from './FileUploader'

describe('FileUploader', () => {
  it('ignores an empty drop without throwing', () => {
    const onFileSelect = vi.fn()
    render(<FileUploader onFileSelect={onFileSelect} />)

    expect(() => {
      fireEvent.drop(screen.getByRole('button'), { dataTransfer: { files: [] } })
    }).not.toThrow()
    expect(onFileSelect).not.toHaveBeenCalled()
  })

  it('keeps the previous valid selection and error visible when a replacement is invalid', () => {
    const onSelection = vi.fn()

    function Harness() {
      const [file, setFile] = useState<File | null>(null)
      return (
        <>
          <FileUploader
            file={file}
            onFileSelect={(nextFile) => {
              setFile(nextFile)
              onSelection(nextFile)
            }}
          />
          <output data-testid="selected-file">{file?.name}</output>
        </>
      )
    }

    const { container } = render(<Harness />)
    const input = container.querySelector('input[type="file"]') as HTMLInputElement

    fireEvent.change(input, {
      target: { files: [new File(['ok'], 'valid.wxapkg')] },
    })
    expect(onSelection).toHaveBeenLastCalledWith(expect.objectContaining({ name: 'valid.wxapkg' }))

    fireEvent.change(input, {
      target: { files: [new File(['bad'], 'invalid.txt')] },
    })

    expect(onSelection).toHaveBeenCalledTimes(1)
    expect(screen.getByTestId('selected-file')).toHaveTextContent('valid.wxapkg')
    expect(screen.getByTitle('valid.wxapkg')).toBeInTheDocument()
    expect(screen.getByText('文件格式不正确，请选择 .wxapkg 文件')).toBeInTheDocument()
    expect(input.value).toBe('')
  })

  it('clears the native input so the same file can be selected again', () => {
    const onSelection = vi.fn()

    function Harness() {
      const [file, setFile] = useState<File | null>(null)
      return (
        <>
          <FileUploader
            file={file}
            onFileSelect={(nextFile) => {
              setFile(nextFile)
              onSelection(nextFile)
            }}
          />
          <button type="button" onClick={() => setFile(null)}>
            重置
          </button>
        </>
      )
    }

    const { container } = render(<Harness />)
    const input = container.querySelector('input[type="file"]') as HTMLInputElement
    const sameFile = new File(['ok'], 'same.wxapkg')

    fireEvent.change(input, { target: { files: [sameFile] } })
    expect(screen.getByText('same.wxapkg')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '重置' }))
    expect(screen.queryByText('same.wxapkg')).not.toBeInTheDocument()
    expect(input.value).toBe('')

    fireEvent.change(input, { target: { files: [sameFile] } })
    expect(onSelection).toHaveBeenCalledTimes(2)
  })

  it('exposes stable visual states for idle, dragging and selected feedback', () => {
    const onFileSelect = vi.fn()
    const { rerender } = render(<FileUploader onFileSelect={onFileSelect} />)
    const button = screen.getByRole('button')
    const surface = button.closest('[data-state]')

    expect(surface).toHaveAttribute('data-state', 'idle')
    fireEvent.dragEnter(surface!)
    expect(surface).toHaveAttribute('data-state', 'dragging')

    fireEvent.dragEnter(button)
    fireEvent.dragLeave(button)
    expect(surface).toHaveAttribute('data-state', 'dragging')

    fireEvent.dragLeave(surface!)
    expect(surface).toHaveAttribute('data-state', 'idle')

    rerender(
      <FileUploader file={new File(['package'], 'ready.wxapkg')} onFileSelect={onFileSelect} />
    )
    expect(surface).toHaveAttribute('data-state', 'selected')
    expect(screen.getByText('文件已就绪')).toBeInTheDocument()
  })
})
