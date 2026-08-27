import { AGENTS, PLANES } from '../../data/site'
import { MIDDOT } from '../../data/glyphs'

export function Orientation() {
  return (
    <div className="orientation">
      <div className="kicker">TRACK 0 {MIDDOT} 3 MIN</div>
      <h2 className="track-title">What Gryph solves</h2>
      <p className="orientation-goal">
        An AI agent can read any file, write any file, and run any command. One session can
        include many actions. Gryph adds two functions. It records the actions, and it controls
        them.
      </p>
      <div className="orientation-planes">
        {PLANES.map((p) => (
          <div key={p.name} className="orientation-plane">
            <div className="orientation-plane-name">{p.name}</div>
            <div className="orientation-plane-desc">{p.desc}</div>
            <div className="orientation-plane-entry">{p.entry}</div>
          </div>
        ))}
      </div>
      <div className="orientation-agents-label">Supported agents</div>
      <div className="orientation-agents">
        {AGENTS.map((name) => (
          <span key={name} className="orientation-agent">
            {name}
          </span>
        ))}
      </div>
    </div>
  )
}
