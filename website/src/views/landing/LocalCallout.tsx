import { ARROW_NEXT } from '../../data/glyphs'

export function LocalCallout({ onStart }: { onStart: () => void }) {
  return (
    <section className="local-callout">
      <div>
        <div className="local-title">All data stays local.</div>
        <div className="local-sub">No cloud. No telemetry. SQLite on your machine.</div>
      </div>
      <button type="button" className="local-cta" onClick={onStart}>
        start the lessons {ARROW_NEXT}
      </button>
    </section>
  )
}
