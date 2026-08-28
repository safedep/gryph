import { useCallback, useEffect, useRef, useState } from 'react'

export function useCopy(): { copied: string | null; copy: (text: string) => void } {
  const [copied, setCopied] = useState<string | null>(null)
  const timer = useRef<number | undefined>(undefined)

  useEffect(() => () => window.clearTimeout(timer.current), [])

  const copy = useCallback((text: string) => {
    if (!navigator.clipboard) return
    navigator.clipboard
      .writeText(text)
      .then(() => {
        setCopied(text)
        window.clearTimeout(timer.current)
        timer.current = window.setTimeout(() => {
          setCopied((current) => (current === text ? null : current))
        }, 1200)
      })
      .catch(() => {})
  }, [])

  return { copied, copy }
}
