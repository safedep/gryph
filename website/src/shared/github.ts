import { useEffect, useState } from 'react'

// The static fallback stays until GitHub answers.
function useGitHub(path: string, read: (d: any) => string | undefined, fallback: string): string {
  const [value, setValue] = useState(fallback)

  useEffect(() => {
    let live = true
    fetch(`https://api.github.com/repos/safedep/gryph${path}`)
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => {
        const v = d && read(d)
        if (live && v != null) setValue(v)
      })
      .catch(() => {})
    return () => {
      live = false
    }
  }, [path, read])

  return value
}

const readStars = (d: any) => d.stargazers_count?.toLocaleString()
const readVersion = (d: any) => d.tag_name

export const useStars = () => useGitHub('', readStars, '161')

export const useVersion = () => useGitHub('/releases/latest', readVersion, 'v0.9.0')
