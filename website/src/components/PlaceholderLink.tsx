import type { ReactNode } from 'react'

interface PlaceholderLinkProps {
  children: ReactNode
  className?: string
}

export function PlaceholderLink({ children, className }: PlaceholderLinkProps) {
  return (
    <a
      href="#"
      className={className}
      onClick={(e) => {
        e.preventDefault()
      }}
    >
      {children}
    </a>
  )
}
