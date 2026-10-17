import type { ReactNode } from 'react'
import { REPO_URL, SAFEDEP_URL, cx } from './site'
import { useStars } from './github'
import { GitHubMark } from './Icons'

const MARK = [
  'M7.86802 33.7899L0.439392 31.1018L0.400024 10.6899L7.82866 13.3781L7.8342 16.5347L7.83311 16.5467L7.86802 33.7899Z',
  'M7.82865 13.3779L0.400024 10.6897L28.6941 0.400024L36.1227 3.08813L12.3153 11.7474L12.2897 11.7536L7.82865 13.3779Z',
  'M7.86778 33.7894L7.8328 16.5462L7.83389 16.5343L7.82837 13.3776L12.2894 11.7534L12.315 11.7471L36.1225 3.08789L36.1269 6.18464L12.7706 15.0955L10.9777 15.7458L10.9815 15.7711L10.9825 17.686L11.0126 32.6462L7.86778 33.7894Z',
  'M28.659 13.4635L36.0876 16.1516L36.127 36.5635L28.6983 33.8754L28.6928 30.7187L28.6939 30.7068L28.659 13.4635Z',
  'M28.6983 33.8756L36.1269 36.5637L7.83284 46.8534L0.404209 44.1653L24.2117 35.5061L24.2372 35.4998L28.6983 33.8756Z',
  'M28.6592 13.464L28.6942 30.7072L28.6931 30.7192L28.6986 33.8758L24.2376 35.5L24.212 35.5063L0.40451 44.1655L0.400074 41.0688L23.7564 32.1579L25.5493 31.5076L25.5455 31.4823L25.5444 29.5674L25.5143 14.6072L28.6592 13.464Z',
]

interface CrumbsProps {
  brandHref: string
  brandLabel?: string
}

export function Crumbs({ brandHref, brandLabel }: CrumbsProps) {
  return (
    <div className="crumbs">
      <a className="org" href={SAFEDEP_URL} target="_blank" rel="noopener" aria-label="SafeDep">
        <svg viewBox="0 0 37 48" aria-hidden="true">
          <g fill="currentColor">
            {MARK.map((d) => (
              <path key={d} d={d} />
            ))}
          </g>
        </svg>
      </a>
      <svg className="sep" viewBox="0 0 24 24" aria-hidden="true">
        <path d="M16.9 3.5 7.1 20.5" stroke="currentColor" strokeWidth="2" fill="none" />
      </svg>
      <a className="wordmark" href={brandHref} aria-label={brandLabel}>
        Gryph
      </a>
    </div>
  )
}

export function StarsLink() {
  const stars = useStars()
  return (
    <a className="btn btn-sm stars" href={REPO_URL} aria-label={`Gryph on GitHub, ${stars} stars`}>
      <GitHubMark />
      <span>&#9733; {stars}</span>
    </a>
  )
}

interface TopbarProps {
  brandHref: string
  wide?: boolean
  nav?: ReactNode
  children?: ReactNode
}

export function Topbar({ brandHref, wide, nav, children }: TopbarProps) {
  return (
    <header className={cx('topbar', wide && 'topbar-wide')}>
      <div className="topbar-in">
        <Crumbs brandHref={brandHref} />
        {nav}
        <div className="topbar-end">{children}</div>
      </div>
    </header>
  )
}

export function Footer({ children, note }: { children: ReactNode; note?: ReactNode }) {
  return (
    <footer className="foot">
      <div className="foot-in">
        <Crumbs brandHref="#top" brandLabel="Back to top" />
        <nav aria-label="Footer">{children}</nav>
        <span className="spacer" />
        <Copyright />
        {note && <p className="foot-note">{note}</p>}
      </div>
    </footer>
  )
}

export function Copyright() {
  return (
    <span>
      &copy; 2026 <a href={SAFEDEP_URL}>SafeDep, Inc.</a> Gryph is open source under Apache 2.0.
    </span>
  )
}
