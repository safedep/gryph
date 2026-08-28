import { AGENTS } from '../../data/site'

export function AgentStrip() {
  return (
    <section className="agents">
      <div className="agents-label">Gryph works with the agents you already use</div>
      <div className="agents-list">
        {AGENTS.map((name) => (
          <span key={name} className="agent-chip">
            {name}
          </span>
        ))}
      </div>
    </section>
  )
}
