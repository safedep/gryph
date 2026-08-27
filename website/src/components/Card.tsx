import type { ReactNode } from 'react'
import './components.css'

interface CardProps {
  title: string
  children: ReactNode
}

export function Card({ title, children }: CardProps) {
  return (
    <div className="card">
      <div className="card-header">
        <span className="card-dot card-dot-red" />
        <span className="card-dot" />
        <span className="card-title">{title}</span>
      </div>
      <div className="card-body">{children}</div>
    </div>
  )
}
