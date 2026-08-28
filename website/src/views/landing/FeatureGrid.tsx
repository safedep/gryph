import { FEATURES } from '../../data/site'

export function FeatureGrid() {
  return (
    <div className="feature-grid">
      {FEATURES.map((f) => (
        <div key={f.name} className="feature-cell">
          <div className="feature-name">{f.name}</div>
          <div className="feature-desc">{f.desc}</div>
        </div>
      ))}
    </div>
  )
}
