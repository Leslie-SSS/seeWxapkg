import { useEffect, useState } from 'react'
import { api } from '../api/client'

const REPOSITORY_URL = 'https://github.com/Leslie-SSS/seeWxapkg'
const STAR_REFRESH_INTERVAL_MS = 5 * 60 * 1_000

type StarState =
  | { status: 'loading'; count: null }
  | { status: 'ready'; count: number; stale: boolean }
  | { status: 'unavailable'; count: null }

export function GithubStarLink() {
  const [stars, setStars] = useState<StarState>({ status: 'loading', count: null })

  useEffect(() => {
    let activeController: AbortController | null = null
    let disposed = false

    const refresh = () => {
      activeController?.abort()
      const controller = new AbortController()
      activeController = controller

      void api
        .getGithubStars(controller.signal)
        .then(({ stars: count, stale }) => {
          if (!disposed && !controller.signal.aborted) setStars({ status: 'ready', count, stale })
        })
        .catch(() => {
          if (disposed || controller.signal.aborted) return
          setStars((current) =>
            current.status === 'ready'
              ? { ...current, stale: true }
              : { status: 'unavailable', count: null }
          )
        })
        .finally(() => {
          if (activeController === controller) activeController = null
        })
    }

    refresh()
    const refreshTimer = window.setInterval(refresh, STAR_REFRESH_INTERVAL_MS)

    return () => {
      disposed = true
      window.clearInterval(refreshTimer)
      activeController?.abort()
    }
  }, [])

  const isStale = stars.status === 'ready' && stars.stale
  const staleNotice = 'GitHub 暂时不可用，当前显示最近一次 Star 数据'
  const accessibleLabel = isStale
    ? `在 GitHub 查看 See Wxapkg，${stars.count} 个 Star；${staleNotice}（在新窗口打开）`
    : stars.status === 'ready'
      ? `在 GitHub 查看 See Wxapkg，${stars.count} 个 Star（在新窗口打开）`
      : '在 GitHub 查看 See Wxapkg（在新窗口打开）'

  return (
    <a
      className="github-star-link"
      href={REPOSITORY_URL}
      target="_blank"
      rel="noopener noreferrer"
      aria-label={accessibleLabel}
      title={isStale ? staleNotice : undefined}
    >
      <span className="github-star-brand">
        <svg aria-hidden="true" className="github-mark" fill="currentColor" viewBox="0 0 24 24">
          <path d="M12 0C5.37 0 0 5.37 0 12c0 5.3 3.44 9.8 8.21 11.39.6.11.79-.26.79-.58v-2.23c-3.34.72-4.03-1.42-4.03-1.42-.55-1.39-1.33-1.76-1.33-1.76-1.09-.74.08-.73.08-.73 1.21.09 1.84 1.24 1.84 1.24 1.07 1.83 2.81 1.3 3.49 1 .11-.78.42-1.31.76-1.61-2.66-.3-5.47-1.33-5.47-5.93 0-1.31.47-2.38 1.24-3.22-.13-.3-.54-1.52.11-3.18 0 0 1.01-.32 3.3 1.23A11.5 11.5 0 0 1 12 6.8c1.02.01 2.05.14 3.01.4 2.29-1.55 3.3-1.23 3.3-1.23.65 1.66.24 2.88.12 3.18.77.84 1.23 1.91 1.23 3.22 0 4.61-2.81 5.62-5.48 5.92.43.37.82 1.1.82 2.22v3.3c0 .32.19.69.8.57A12 12 0 0 0 24 12c0-6.63-5.37-12-12-12Z" />
        </svg>
        <span>GitHub</span>
      </span>

      <span className="github-star-divider" aria-hidden="true" />

      <span className="github-star-meta" aria-hidden="true">
        <svg className="github-star-icon" fill="currentColor" viewBox="0 0 24 24">
          <path d="m12 2.25 2.93 5.94 6.55.95-4.74 4.62 1.12 6.52L12 17.2l-5.86 3.08 1.12-6.52-4.74-4.62 6.55-.95L12 2.25Z" />
        </svg>
        <span className="github-star-action-label">Star</span>
        <span
          className="github-star-count"
          data-state={stars.status}
          data-stale={stars.status === 'ready' ? stars.stale.toString() : undefined}
        >
          {stars.status === 'ready'
            ? `${stars.stale ? '~' : ''}${formatStarCount(stars.count)}`
            : '—'}
        </span>
      </span>
    </a>
  )
}

function formatStarCount(count: number) {
  if (count < 1_000) return count.toString()

  return new Intl.NumberFormat('en-US', {
    notation: 'compact',
    maximumFractionDigits: 1,
  })
    .format(count)
    .replace('K', 'k')
}
