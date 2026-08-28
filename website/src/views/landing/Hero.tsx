import { ARROW_NEXT, MIDDOT } from '../../data/glyphs'

export function Hero({ onStart }: { onStart: () => void }) {
  return (
    <section className="hero">
      <div className="hero-kicker">OBSERVE AND CONTROL AI CODING AGENTS</div>
      <h1 className="hero-title">
        Your agents.
        <br />
        your controls.
      </h1>
      <p className="hero-desc">
        An AI coding agent reads files, writes files, searches the web, calls tools, and runs
        commands with no common record and no limit. Gryph adds the two primitives it does not
        have. It records every action in a local database. It provides a powerful policy layer to
        block, guide, or influence agent actions.
      </p>
      <div className="hero-planes">
        <div className="hero-plane">
          <div className="hero-plane-name">Observe</div>
          <div className="hero-plane-desc">
            Gryph maintains a record of every action your agents take. You can see what they did,
            when, and why.
          </div>
        </div>
        <div className="hero-plane">
          <div className="hero-plane-name">Control</div>
          <div className="hero-plane-desc">
            Gryph can block, warn, or guide an action. It makes the decision before the action
            runs based on your policies.
          </div>
        </div>
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
