// Block-character art, generated from a seed, so the server and the browser draw the same picture.
// A field is a <pre> of <span>s. Each span gets one of the accent classes in components.css.

type Cell = [ch: string, cls: string] | null

function rng(seed: number) {
  let a = seed | 0
  return () => {
    a = (a + 0x6d2b79f5) | 0
    let t = Math.imul(a ^ (a >>> 15), 1 | a)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

const SHADES = ' ░▒▓█'

// Waves of shade that rise from the bottom. Green on the left, purple in the middle, red at the right.
function shade(cols: number, rows: number, R: () => number): Cell[][] {
  const out: Cell[][] = []
  for (let y = 0; y < rows; y++) {
    const row: Cell[] = []
    for (let x = 0; x < cols; x++) {
      const w = Math.sin(x / 11 + y / 2.6) * 0.5 + 0.5
      const v = w * 0.75 + R() * 0.25
      const ch = SHADES[Math.min(4, Math.floor(v * 5))]
      const t = x / cols + (y / rows) * 0.4 + Math.sin(x / 30) * 0.1
      const cls = v < 0.45 ? 'gx' : t < 0.4 ? 'gm' : t < 0.75 ? 'gp' : 'gc'
      row.push(ch === ' ' ? null : [ch, cls])
    }
    out.push(row)
  }
  return out
}

// A plume that rises out of the hero's shade field on the right edge. Dense shade at the base breaks
// into quadrant blocks, then into dots as it climbs. Colour runs from red and purple at the base, as in
// the field, to green near the top.
function plume(cols: number, rows: number, R: () => number): Cell[][] {
  const QUADS = '▖▗▘▝▚▞▙▟▛▜'
  const out: Cell[][] = []
  for (let y = 0; y < rows; y++) {
    const row: Cell[] = []
    const up = 1 - y / (rows - 1) // 0 at the base, 1 at the top
    for (let x = 0; x < cols; x++) {
      const right = x / (cols - 1)
      const wave = Math.sin(x / 7 + y / 5) * 0.08 + Math.sin(y / 3.3 - x / 11) * 0.06
      const d = (1 - up) ** 1.6 * (0.3 + 0.7 * right) + right ** 3 * 0.35 + wave + (R() - 0.5) * 0.18
      if (d < 0.16) {
        row.push(R() < 0.012 + right * 0.02 ? ['·', 'gx'] : null)
        continue
      }
      const ch =
        d > 0.62 ? SHADES[Math.min(4, 2 + Math.floor(R() * 3))] : d > 0.36 ? QUADS[Math.floor(R() * QUADS.length)] : R() < 0.5 ? '▪' : '·'
      const cls = d < 0.3 && R() < 0.4 ? 'gx' : up > 0.62 ? 'gm' : up > 0.3 ? 'gp' : R() < 0.45 ? 'gc' : 'gp'
      row.push([ch, cls])
    }
    out.push(row)
  }
  return out
}

// A faint drift of shade that gathers in the bottom-right corner of a section. It takes the section's
// own colour through the gk class.
function dust(cols: number, rows: number, R: () => number): Cell[][] {
  const out: Cell[][] = []
  for (let y = 0; y < rows; y++) {
    const row: Cell[] = []
    for (let x = 0; x < cols; x++) {
      const d = (x / cols) ** 1.4 * (y / rows) ** 1.1 + (R() - 0.5) * 0.25
      row.push(d < 0.18 ? (R() < 0.01 ? ['·', 'gk'] : null) : [d > 0.62 ? '▓' : d > 0.42 ? '▒' : '░', 'gk'])
    }
    out.push(row)
  }
  return out
}

// A tall, faint column of texture for a sidebar. It gathers toward the bottom and the right edge.
function column(cols: number, rows: number, R: () => number): Cell[][] {
  const QUADS = '▖▗▘▝▚▞'
  const out: Cell[][] = []
  for (let y = 0; y < rows; y++) {
    const row: Cell[] = []
    for (let x = 0; x < cols; x++) {
      const d = (x / cols) ** 2 * 0.55 + (y / rows) ** 2.2 * 0.6 + (R() - 0.5) * 0.3
      if (d < 0.42) {
        row.push(R() < 0.006 ? ['·', 'gx'] : null)
        continue
      }
      const ch = d > 0.8 ? '▒' : d > 0.6 ? '░' : QUADS[Math.floor(R() * QUADS.length)]
      const r = R()
      row.push([ch, r < 0.07 ? 'gm' : r < 0.11 ? 'gp' : 'gx'])
    }
    out.push(row)
  }
  return out
}

const GEN = { shade, plume, dust, column }

// Merges neighbouring cells of one colour into a single element. The art looks the same, with a small
// fraction of the markup, which keeps the prerendered pages light.
function runs(grid: Cell[][]) {
  const out: (string | ReturnType<typeof run>)[] = []
  let text = ''
  let cls: string | null = null
  const flush = () => {
    if (text) out.push(cls ? run(out.length, cls, text) : text)
    text = ''
  }
  for (const row of grid) {
    for (const c of [...row, null]) {
      const next = c ? c[1] : null
      if (next !== cls) {
        flush()
        cls = next
      }
      text += c ? c[0] : ' '
    }
    text = text.slice(0, -1) + '\n'
  }
  flush()
  return out
}

const run = (key: number, cls: string, text: string) => (
  <i key={key} className={cls}>
    {text}
  </i>
)

interface GlyphProps {
  kind: keyof typeof GEN
  cols: number
  rows: number
  seed?: number
  className?: string
}

export function Glyph({ kind, cols, rows, seed = 1, className }: GlyphProps) {
  const grid = GEN[kind](cols, rows, rng(seed * 7919))
  return (
    <pre className={className ? `glyph ${className}` : 'glyph'} aria-hidden="true">
      {runs(grid)}
    </pre>
  )
}

// Small hand-drawn marks for feature cards. Letters pick the tone: m mint, p purple, c cherry, x muted.
const ICONS: Record<string, [art: string, tones: string]> = {
  lock: ['  ▗▄▄▖  \n  ▌  ▐  \n▗█▀▀▀▀█▖\n▐█ ▓▓ █▌\n▝▀▀▀▀▀▀▘', 'c'],
  home: ['   ▗▄▖   \n ▗▟▓▓▓▙▖ \n▐▓▓▓▓▓▓▓▌\n▐▓▓ ▌▐▓▓▌\n▝▀▀ ▀▀▀▀▘', 'm'],
  shield: ['▗▄▄▄▄▄▖\n▐▓▓▓▓▓▌\n▐▓▓▓▓▓▌\n ▜▓▓▓▛ \n  ▝▀▘  ', 'p'],
}

const TONE: Record<string, string> = { m: 'gm', p: 'gp', c: 'gc' }

export function Icon({ name }: { name: keyof typeof ICONS }) {
  const [art, tone] = ICONS[name]
  return (
    <pre className="glyph glyph-icon" aria-hidden="true">
      {[...art].map((ch, i) => {
        if (ch === ' ' || ch === '\n') return ch
        const cls = ch === '▌' || ch === '▐' ? 'gp' : ch === '✕' ? 'gc' : ch === '─' || ch === '│' || ch === '┼' ? 'gx' : TONE[tone]
        return (
          <i key={i} className={cls}>
            {ch}
          </i>
        )
      })}
    </pre>
  )
}

// The setup as a metro line over a skyline. One stop per step, with its name on a row below.
// Finished stretches are solid green, the rest dashed grey. The next stop is ringed in purple.
// The skyline is tinted green under finished steps, purple at the next one, grey ahead.
export function Trail({ done, names, cols = 82 }: { done: boolean[]; names: string[]; cols?: number }) {
  const n = done.length
  const next = done.indexOf(false)
  const R = rng(9001)
  const pad = 4
  const xs = done.map((_, i) => Math.round(pad + (i * (cols - 1 - 2 * pad)) / (n - 1)))
  const ys = done.map((_, i) => 4 + Math.round(Math.sin(i * 1.25 + 0.4) * 2.6))
  const nameRow = 9
  const rows = 16
  const grid: Cell[][] = Array.from({ length: rows }, () => Array<Cell>(cols).fill(null))
  const put = (x: number, y: number, ch: string, cls: string) => {
    if (x >= 0 && x < cols && y >= 0 && y < rows) grid[y][x] = [ch, cls]
  }

  // The line between stops.
  for (let i = 0; i < n - 1; i++) {
    const solid = done[i] && done[i + 1]
    const cls = solid ? 'gm' : i === next - 1 ? 'gp' : 'gx'
    const h = solid ? '━' : '┅'
    const v = solid ? '┃' : '┇'
    const [x0, y0, x1, y1] = [xs[i], ys[i], xs[i + 1], ys[i + 1]]
    const xm = Math.round((x0 + x1) / 2)
    if (y0 === y1) {
      for (let x = x0 + 1; x < x1; x++) put(x, y0, h, cls)
      continue
    }
    const down = y1 > y0
    for (let x = x0 + 1; x < xm; x++) put(x, y0, h, cls)
    put(xm, y0, solid ? (down ? '┓' : '┛') : down ? '╮' : '╯', cls)
    for (let y = Math.min(y0, y1) + 1; y < Math.max(y0, y1); y++) put(xm, y, v, cls)
    put(xm, y1, solid ? (down ? '┗' : '┏') : down ? '╰' : '╭', cls)
    for (let x = xm + 1; x < x1; x++) put(x, y1, h, cls)
  }

  // Stops, their numbers above and their names on the name row.
  xs.forEach((x, i) => {
    const cls = done[i] ? 'gm' : i === next ? 'gp' : 'gx'
    put(x, ys[i], done[i] ? '●' : i === next ? '◉' : '○', cls)
    put(x, ys[i] - 1, String(i + 1), cls)
    const name = names[i] ?? ''
    const start = Math.max(0, Math.min(cols - name.length, x - Math.floor(name.length / 2)))
    ;[...name].forEach((ch, k) => put(start + k, nameRow, ch, i === next ? 'gp' : done[i] ? 'gm' : 'gx'))
  })

  // The skyline: blocks of shade, two to six rows tall, one column of air between them.
  const tone = (x: number) => {
    const before = xs.filter((sx) => sx <= x).length - 1
    if (next === -1 || before < next - 1) return 'gmd'
    if (before === next - 1) return 'gpd'
    return 'gx'
  }
  for (let x = 0; x < cols; ) {
    const w = 2 + Math.floor(R() * 4)
    const h = 2 + Math.floor(R() * 5)
    for (let bx = x; bx < Math.min(cols, x + w); bx++) {
      const cls = tone(bx)
      for (let y = rows - h; y < rows; y++) {
        const depth = y - (rows - h)
        const ch = depth === 0 ? '▄' : SHADES[Math.min(4, 1 + Math.floor((depth / h) * 3) + (R() < 0.15 ? 1 : 0))]
        // Windows: a few lit cells in the taller blocks.
        const lit = h > 3 && depth > 1 && R() < 0.08
        put(bx, y, lit ? '▪' : ch, lit ? cls.replace('d', '') : cls)
      }
    }
    x += w + (R() < 0.6 ? 1 : 0)
  }

  return (
    <pre className="glyph trail" aria-hidden="true">
      {runs(grid)}
    </pre>
  )
}

// Larger hand-drawn scenes for the feature cells. Box-drawing characters are muted. A few symbols carry a
// fixed colour. Other characters take the line's tone, or the plain text colour. A mask line, when given,
// sets a character's class by letter: m mint, p purple, c cherry, x muted, t text, d dim.
const BOX = new Set([...'╭╮╰╯│─├┤┬┴┼'])
const SYMBOL: Record<string, string> = { '+': 'gm', '-': 'gc', '✓': 'gm', '✕': 'gc', '◆': 'gm', '$': 'gm', '▌': 'gm' }
const MASK: Record<string, string> = { m: 'gm', p: 'gp', c: 'gc', x: 'gx', t: 'gt', d: 'gd' }

type SceneLine = string | [text: string, tone?: string, mask?: string]

const SCENES: Record<string, SceneLine[]> = {
  files: [
    '╭─────────────╮',
    ['│ ▀▀▀▀▀ ▀▀▀   │─╮', 'gd'],
    ['│ ▀▀▀ ▀▀▀▀▀▀  │ │─╮', 'gd'],
    ['│+▀▀▀▀▀▀▀ ▀▀  │ │ │', 'gm'],
    ['│+▀▀▀▀ ▀▀▀▀   │ │ │', 'gm'],
    ['│-▀▀▀▀▀▀      │ │ │', 'gc'],
    ['│ ▀▀ ▀▀▀▀▀    │ │ │', 'gd'],
    '╰─────────────╯ │ │',
    '  ╰─────────────╯ │',
    '    ╰─────────────╯',
  ],
  commands: [
    '╭──────────────────────╮',
    ['│ ● ● ●   gryph        │', 'gd', ' c m p'],
    '├──────────────────────┤',
    '│ $ npm test        ✓  │',
    '│ $ cat src/app.ts  ✓  │',
    ['│ $ git push -f     ✕  │', 'gcd'],
    ['│ $ curl x.sh | sh  ✕  │', 'gcd'],
    '│ $ ▌                  │',
    '╰──────────────────────╯',
  ],
  web: [
    ['             ╭─◆ github.com', 'gt'],
    '  ▗▄▄▄▄▄▖    │',
    ['  ▐▓▓▓▓▓▌    ├─◆ npmjs.org', 'gt', '   ppppp'],
    ['  ▐▓▓▓▓▓▌────┤', '', '   ppppp'],
    ['  ▝▀▀▀▀▀▘    ├─◆ pypi.org', 'gt'],
    ' ▀▀▀▀▀▀▀▀▀   │',
    ['             ╰─✕ x.example', 'gcd', '             cc'],
  ],
}

export function Scene({ name }: { name: keyof typeof SCENES }) {
  return (
    <pre className="glyph scene" aria-hidden="true">
      {SCENES[name].map((line, y) => {
        const [text, tone, mask = ''] = typeof line === 'string' ? [line] : line
        return (
          <span key={y} className="glyph-row">
            {[...text].map((ch, x) => {
              if (ch === ' ') return ' '
              const cls =
                MASK[mask[x]] ?? (BOX.has(ch) ? 'gx' : (SYMBOL[ch] ?? (tone || 'gt')))
              return (
                <i key={x} className={cls}>
                  {ch}
                </i>
              )
            })}
            {'\n'}
          </span>
        )
      })}
    </pre>
  )
}
