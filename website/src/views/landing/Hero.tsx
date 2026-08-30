import { Card } from '../../components/Card'
import { BlinkCursor } from '../../components/BlinkCursor'
import { ARROW_NEXT, MIDDOT } from '../../data/glyphs'

export function Hero({ onStart }: { onStart: () => void }) {
  return (
    <section className="hero">
      <div className="hero-kicker">OBSERVE AND CONTROL AI CODING AGENTS</div>
      <h1 className="hero-title">
        Your agents.
        <br />
        your controls.
        <BlinkCursor />
      </h1>
      <p className="hero-desc">
        An AI coding agent reads, writes, and runs commands with no record and no limit. Gryph
        records every action and enforces your policy before the action runs.
      </p>
      <div className="hero-term">
        <Card title="gryph logs --live">
          <div>
            <span className="badge badge-allow">ALLOW</span> write readme.txt
          </div>
          <div>
            <span className="badge badge-block">BLOCK</span> exec rm -rf /
          </div>
          <div className="term-note">policy: no-destructive {MIDDOT} claude-code</div>
          <div>
            <span className="badge badge-allow">ALLOW</span> read src/main.go
            <BlinkCursor />
          </div>
        </Card>
      </div>
      <button type="button" className="hero-cta" onClick={onStart}>
        protect your agents {ARROW_NEXT}
      </button>
      <div className="hero-meta">
        30 minutes {MIDDOT} 8 tracks {MIDDOT} runs on your machine
      </div>
    </section>
  )
}
