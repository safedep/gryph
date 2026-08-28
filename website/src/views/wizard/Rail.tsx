import { RAIL_TITLES } from '../../data/site'
import { CHECK } from '../../data/glyphs'

interface RailProps {
  current: number
  onSelect: (i: number) => void
}

export function Rail({ current, onSelect }: RailProps) {
  return (
    <aside className="rail">
      <div className="rail-label">MODULES</div>
      <div className="rail-items">
        {RAIL_TITLES.map((title, i) => {
          const state = i === current ? ' rail-item-current' : i < current ? ' rail-item-done' : ''
          return (
            <button
              type="button"
              key={title}
              className={`rail-item${state}`}
              onClick={() => onSelect(i)}
            >
              <span className="rail-badge">{i < current ? CHECK : i}</span>
              <span className="rail-title">{title}</span>
            </button>
          )
        })}
      </div>
    </aside>
  )
}
