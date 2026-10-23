import { useEffect, useState } from 'react'

// The static fallback stays until GitHub answers. GitHub allows 60 calls an hour per address without a
// login, so the last good value is kept in the browser and shown when a call is refused.
function useGitHub(path: string, read: (d: any) => string | undefined, fallback: string): string {
  const [value, setValue] = useState(fallback)

  useEffect(() => {
    let live = true
    const key = `gryph-gh${path}`
    const store = (v?: string) => {
      try {
        if (v == null) return localStorage.getItem(key) ?? undefined
        localStorage.setItem(key, v)
      } catch {
        // Blocked storage.
      }
      return v
    }
    fetch(`https://api.github.com/repos/safedep/gryph${path}`)
      .then((r) => (r.ok ? r.json() : null))
      .catch(() => null)
      .then((d) => {
        const v = d ? read(d) : undefined
        const shown = v != null ? store(v) : store()
        if (live && shown != null) setValue(shown)
      })
    return () => {
      live = false
    }
  }, [path, read])

  return value
}

const readStars = (d: any) => d.stargazers_count?.toLocaleString()

export const useStars = () => useGitHub('', readStars, '179')
