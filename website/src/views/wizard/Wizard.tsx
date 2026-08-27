import { useState } from 'react'
import { Logo } from '../../components/Logo'
import { StarButton } from '../../components/StarButton'
import { Rail } from './Rail'
import { Orientation } from './Orientation'
import { StepTrack } from './StepTrack'
import { TRACKS } from '../../data/tracks'
import { DOCS_FILE_URL } from '../../data/site'
import { useCopy } from '../../hooks/useCopy'
import { ARROW_BACK, ARROW_EXTERNAL, ARROW_NEXT, MIDDOT } from '../../data/glyphs'
import './wizard.css'

const TOTAL = TRACKS.length

interface WizardProps {
  stars: string
  ti: number
  onHome: () => void
  onSelectTrack: (i: number) => void
  onBack: () => void
  onNext: () => void
}

export function Wizard({ stars, ti, onHome, onSelectTrack, onBack, onNext }: WizardProps) {
  const { copied, copy } = useCopy()
  const [confirmed, setConfirmed] = useState<Record<number, boolean>>({})
  const [quizPicks, setQuizPicks] = useState<Record<number, number>>({})
  const [installMethod, setInstallMethod] = useState('curl')

  const track = ti > 0 ? TRACKS[ti - 1] : undefined
  const headerLabel =
    ti === 0 ? `orientation ${MIDDOT} 3 min` : `track ${ti} / ${TOTAL} ${MIDDOT} ${track?.title}`
  const docsLabel = track?.docs ?? 'security-policy.md'
  const nextLabel =
    ti === 0
      ? `begin track 1 ${ARROW_NEXT}`
      : ti >= TOTAL
        ? `finish ${ARROW_NEXT}`
        : `next ${ARROW_NEXT}`

  return (
    <div className="wizard">
      <header className="wizard-header">
        <div className="wizard-header-row">
          <Logo size="sm" onClick={onHome} />
          <span className="wizard-header-label">{headerLabel}</span>
          <div className="spacer" />
          <StarButton stars={stars} size="sm" />
        </div>
        <div className="progress">
          <div className="progress-fill" style={{ width: `${Math.round((ti / TOTAL) * 100)}%` }} />
        </div>
      </header>

      <div className="wizard-body">
        <Rail current={ti} onSelect={onSelectTrack} />
        <main className="wizard-main">
          {track ? (
            <StepTrack
              num={ti}
              track={track}
              copied={copied}
              onCopy={copy}
              installMethod={installMethod}
              onInstallMethod={setInstallMethod}
              confirmed={!!confirmed[ti]}
              onToggleConfirm={() => setConfirmed((s) => ({ ...s, [ti]: !s[ti] }))}
              quizPick={quizPicks[ti]}
              onQuizPick={(i) => setQuizPicks((s) => ({ ...s, [ti]: i }))}
            />
          ) : (
            <Orientation />
          )}

          <div className="wizard-nav">
            <a
              className="wizard-nav-docs"
              href={`${DOCS_FILE_URL}${docsLabel}`}
              target="_blank"
              rel="noreferrer"
            >
              {ARROW_EXTERNAL} {docsLabel}
            </a>
            <div className="wizard-nav-buttons">
              <button type="button" className="nav-back" onClick={onBack}>
                {ARROW_BACK} {ti === 0 ? 'home' : 'back'}
              </button>
              <button type="button" className="nav-next" onClick={onNext}>
                {nextLabel}
              </button>
            </div>
          </div>
        </main>
      </div>
    </div>
  )
}
