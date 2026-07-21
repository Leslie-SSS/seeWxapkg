import { act, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '../api/client'
import { GithubStarLink } from './GithubStarLink'

describe('GithubStarLink', () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('shows the latest star count without replacing the fixed-width count slot', async () => {
    const getGithubStars = vi.spyOn(api, 'getGithubStars').mockResolvedValue({
      stars: 1_284,
      stale: false,
    })

    const { container } = render(<GithubStarLink />)
    const link = screen.getByRole('link', { name: /在 GitHub 查看 See Wxapkg/ })
    const countSlot = container.querySelector('.github-star-count')

    expect(link).toHaveAttribute('href', 'https://github.com/Leslie-SSS/seeWxapkg')
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noopener noreferrer')
    expect(countSlot).toHaveAttribute('data-state', 'loading')
    expect(countSlot).toHaveTextContent('—')

    await act(async () => Promise.resolve())

    expect(container.querySelector('.github-star-count')).toBe(countSlot)
    expect(countSlot).toHaveAttribute('data-state', 'ready')
    expect(countSlot).toHaveAttribute('data-stale', 'false')
    expect(countSlot).toHaveTextContent('1.3k')
    expect(link).toHaveAccessibleName('在 GitHub 查看 See Wxapkg，1284 个 Star（在新窗口打开）')
    expect(getGithubStars).toHaveBeenCalledOnce()
  })

  it('discloses when the displayed number is the most recent cached value', async () => {
    vi.spyOn(api, 'getGithubStars').mockResolvedValue({ stars: 86, stale: true })

    const { container } = render(<GithubStarLink />)
    await act(async () => Promise.resolve())

    const link = screen.getByRole('link', { name: /GitHub 暂时不可用/ })
    expect(link).toHaveAccessibleName(
      '在 GitHub 查看 See Wxapkg，86 个 Star；GitHub 暂时不可用，当前显示最近一次 Star 数据（在新窗口打开）'
    )
    expect(link).toHaveAttribute('title', 'GitHub 暂时不可用，当前显示最近一次 Star 数据')
    expect(container.querySelector('.github-star-count')).toHaveAttribute('data-stale', 'true')
    expect(container.querySelector('.github-star-count')?.textContent).toBe('~86')
  })

  it('silently keeps the GitHub destination available when the count cannot load', async () => {
    vi.spyOn(api, 'getGithubStars').mockRejectedValue(new Error('service unavailable'))

    const { container } = render(<GithubStarLink />)

    await act(async () => Promise.resolve())

    expect(container.querySelector('.github-star-count')).toHaveAttribute(
      'data-state',
      'unavailable'
    )
    expect(screen.getByText('GitHub')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /在 GitHub 查看 See Wxapkg/ })).toHaveAttribute(
      'href',
      'https://github.com/Leslie-SSS/seeWxapkg'
    )
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('refreshes every five minutes and preserves the last count on a later failure', async () => {
    vi.useFakeTimers()
    const getGithubStars = vi
      .spyOn(api, 'getGithubStars')
      .mockResolvedValueOnce({ stars: 42, stale: false })
      .mockRejectedValueOnce(new Error('temporary failure'))

    const { container } = render(<GithubStarLink />)
    const countSlot = container.querySelector('.github-star-count')
    await act(async () => Promise.resolve())
    expect(countSlot).toHaveTextContent('42')

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5 * 60 * 1_000)
    })

    expect(getGithubStars).toHaveBeenCalledTimes(2)
    expect(countSlot).toHaveAttribute('data-state', 'ready')
    expect(countSlot).toHaveAttribute('data-stale', 'true')
    expect(countSlot?.textContent).toBe('~42')
    expect(screen.getByRole('link', { name: /GitHub 暂时不可用/ })).toHaveAttribute(
      'title',
      'GitHub 暂时不可用，当前显示最近一次 Star 数据'
    )
  })

  it('cancels the active request and refresh timer when unmounted', async () => {
    vi.useFakeTimers()
    let requestSignal: AbortSignal | undefined
    const getGithubStars = vi.spyOn(api, 'getGithubStars').mockImplementation((signal) => {
      requestSignal = signal
      return new Promise(() => {})
    })

    const { unmount } = render(<GithubStarLink />)
    expect(requestSignal?.aborted).toBe(false)

    unmount()

    expect(requestSignal?.aborted).toBe(true)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5 * 60 * 1_000)
    })
    expect(getGithubStars).toHaveBeenCalledOnce()
  })
})
