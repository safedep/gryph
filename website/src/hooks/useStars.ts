import { useEffect, useState } from 'react'
import { FALLBACK_STARS, REPO_URL } from '../data/site'

function formatStars(n: number): string {
  return n >= 1000 ? `${(n / 1000).toFixed(n < 10000 ? 1 : 0)}k` : String(n)
}

export function useStars(): string {
  const [stars, setStars] = useState(FALLBACK_STARS)

  useEffect(() => {
    const url = REPO_URL.replace('github.com/', 'api.github.com/repos/')
    let cancelled = false
    fetch(url)
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => {
        if (cancelled) return
        const n = d?.stargazers_count
        if (typeof n === 'number') setStars(formatStars(n))
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [])

  return stars
}
