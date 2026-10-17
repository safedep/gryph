import type { ReactNode } from 'react'
import { cx } from './site'

interface WindowProps {
  title: string
  dark?: boolean
  status?: string
  label?: string
  children: ReactNode
}

export function Window({ title, dark, status, label, children }: WindowProps) {
  return (
    <div className={cx('window', dark && 'window-dark')} role={label ? 'figure' : undefined} aria-label={label}>
      <div className="window-bar">
        <span className="lights" aria-hidden="true">
          <i />
          <i />
          <i />
        </span>
        <span className="window-title">{title}</span>
        {status && <span className="window-live">{status}</span>}
      </div>
      <div className="window-body">{children}</div>
    </div>
  )
}

export const Cursor = () => <span className="cursor" aria-hidden="true" />
