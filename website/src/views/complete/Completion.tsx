import { MILESTONES, RECEIPT_LINES, REPO_URL } from '../../data/site'
import { CHECK, EMDASH, MIDDOT, STAR } from '../../data/glyphs'
import { PlaceholderLink } from '../../components/PlaceholderLink'
import './completion.css'

interface CompletionProps {
  stars: string
  onHome: () => void
}

export function Completion({ stars, onHome }: CompletionProps) {
  return (
    <div className="completion">
      <div className="completion-card">
        <div className="completion-receipt">
          <div className="completion-receipt-label">// gryph {MIDDOT} completion receipt</div>
          <div className="completion-receipt-lines">
            {RECEIPT_LINES.map((line) => (
              <div key={line.text} className={`receipt-${line.tone}`}>
                {line.text}
              </div>
            ))}
          </div>
        </div>
        <div className="completion-body">
          <div className="completion-kicker">PATH COMPLETE</div>
          <div className="completion-title">Gryph now protects your agents.</div>
          <div className="completion-sub">
            Hooks active {MIDDOT} policy on {MIDDOT} trail signed.
          </div>
          <div className="milestones">
            {MILESTONES.map((m) => (
              <div key={m.name} className="milestone">
                <span className="milestone-check">{CHECK}</span>
                <span className="milestone-name">{m.name}</span>
                <span className="milestone-desc">
                  {EMDASH} {m.desc}
                </span>
              </div>
            ))}
          </div>
          <a className="completion-star" href={REPO_URL} target="_blank" rel="noreferrer">
            {STAR} star gryph on github
          </a>
          <div className="completion-hint">
            it helps other developers find it {MIDDOT} {stars} stars
          </div>
          <div className="completion-links">
            <PlaceholderLink className="completion-docs">read the documentation</PlaceholderLink>
            <button type="button" className="completion-home" onClick={onHome}>
              back to start
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
