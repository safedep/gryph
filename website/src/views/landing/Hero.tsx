import { ARROW_NEXT, MIDDOT } from '../../data/glyphs'

export function Hero({ onStart }: { onStart: () => void }) {
  return (
    <section className="hero">
      <div className="hero-kicker">SEE AND CONTROL AI CODING AGENTS</div>
      <h1 className="hero-title">
        Your agents.
        <br />
        your controls.
      </h1>
      <p className="hero-desc">
        An AI coding agent reads files, writes files, and runs commands with no common record and
        no limit. Gryph adds the two functions it does not have. It records every action in a
        local database. It provides a powerful policy layer to block, guide or influence agent
        actions.
      </p>
      <div className="hero-planes">
        <div className="hero-plane">
          <div className="hero-plane-name">Observe</div>
          <div className="hero-plane-desc">
            Gryph records which files and commands each agent uses. It keeps every action in a
            local file.
          </div>
        </div>
        <div className="hero-plane">
          <div className="hero-plane-name">Enforce</div>
          <div className="hero-plane-desc">
            Gryph can block, warn, or guide an action. It makes the decision before the action
            runs.
          </div>
        </div>
      </div>
      <button type="button" className="hero-cta" onClick={onStart}>
        protect your agents {ARROW_NEXT}
      </button>
      <div className="hero-meta">
        90 minutes {MIDDOT} 8 tracks {MIDDOT} runs on your machine
      </div>
    </section>
  )
}
