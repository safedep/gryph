import { REPO_URL, SAFEDEP_URL } from './site'
import { useStars } from './github'

const MARK = [
  'M7.86802 33.7899L0.439392 31.1018L0.400024 10.6899L7.82866 13.3781L7.8342 16.5347L7.83311 16.5467L7.86802 33.7899Z',
  'M7.82865 13.3779L0.400024 10.6897L28.6941 0.400024L36.1227 3.08813L12.3153 11.7474L12.2897 11.7536L7.82865 13.3779Z',
  'M7.86778 33.7894L7.8328 16.5462L7.83389 16.5343L7.82837 13.3776L12.2894 11.7534L12.315 11.7471L36.1225 3.08789L36.1269 6.18464L12.7706 15.0955L10.9777 15.7458L10.9815 15.7711L10.9825 17.686L11.0126 32.6462L7.86778 33.7894Z',
  'M28.659 13.4635L36.0876 16.1516L36.127 36.5635L28.6983 33.8754L28.6928 30.7187L28.6939 30.7068L28.659 13.4635Z',
  'M28.6983 33.8756L36.1269 36.5637L7.83284 46.8534L0.404209 44.1653L24.2117 35.5061L24.2372 35.4998L28.6983 33.8756Z',
  'M28.6592 13.464L28.6942 30.7072L28.6931 30.7192L28.6986 33.8758L24.2376 35.5L24.212 35.5063L0.40451 44.1655L0.400074 41.0688L23.7564 32.1579L25.5493 31.5076L25.5455 31.4823L25.5444 29.5674L25.5143 14.6072L28.6592 13.464Z',
]

const GITHUB_MARK =
  'M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27s1.36.09 2 .27c1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8z'

const ORG = (
  <>
    <svg className="sd" viewBox="0 0 37 48" aria-hidden="true">
      <g fill="currentColor">
        {MARK.map((d) => (
          <path key={d} d={d} />
        ))}
      </g>
    </svg>
    <span>SafeDep</span>
  </>
)

const SEP = (
  <svg className="sep" viewBox="0 0 24 24" aria-hidden="true">
    <path d="M16.9 3.5 7.1 20.5" stroke="currentColor" strokeWidth="1.2" fill="none" />
  </svg>
)

interface CrumbsProps {
  brandHref: string
  brandLabel?: string
}

export function Crumbs({ brandHref, brandLabel }: CrumbsProps) {
  return (
    <div className="crumbs">
      <a className="org" href={SAFEDEP_URL} target="_blank" rel="noopener" aria-label="SafeDep">
        {ORG}
      </a>
      {SEP}
      <a className="brand" href={brandHref} aria-label={brandLabel}>
        <span>Gryph</span>
      </a>
    </div>
  )
}

export function StarsLink() {
  const stars = useStars()
  return (
    <a className="stars" href={REPO_URL} aria-label="Gryph on GitHub">
      <svg className="gh" viewBox="0 0 16 16" aria-hidden="true">
        <path fill="currentColor" d={GITHUB_MARK} />
      </svg>
      <b>{stars}</b>
    </a>
  )
}

export function Copyright() {
  return (
    <span>
      &copy; 2026 <a href={SAFEDEP_URL}>SafeDep, Inc.</a> Gryph is open source under Apache 2.0.
    </span>
  )
}
