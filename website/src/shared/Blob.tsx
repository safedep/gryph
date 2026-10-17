import type { CSSProperties } from 'react'

// Blobatars from the Cartridge design system. The seed alone picks the tone, the shape and the face,
// so one name always gives the same blob.

const INK = 'var(--ink-900)'
const WHITE = 'var(--white)'
const TONES = ['coin', 'cherry', 'mint', 'sky', 'grape', 'peach'] as const
const SHAPES = ['round', 'boxy', 'wobble', 'cloud', 'sun', 'droplet'] as const

type Tone = (typeof TONES)[number]
type Shape = (typeof SHAPES)[number]
type EyeKind = 'oval' | 'round' | 'arc' | 'line'
export type Expression = 'idle' | 'happy' | 'sad' | 'surprised' | 'sleepy' | 'wink'

function hash(s: string) {
  let h = 2166136261
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i)
    h = Math.imul(h, 16777619)
  }
  return h >>> 0
}

function rng(seed: number) {
  let a = seed
  return () => {
    a |= 0
    a = (a + 0x6d2b79f5) | 0
    let t = Math.imul(a ^ (a >>> 15), 1 | a)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

function radius(shape: Shape, th: number, p1: number, p2: number) {
  const c = Math.cos(th)
  const s = Math.sin(th)
  switch (shape) {
    case 'boxy':
      return 33 / Math.pow(Math.pow(Math.abs(c), 4) + Math.pow(Math.abs(s), 4), 1 / 4)
    case 'wobble':
      return 33 * (1 + 0.07 * Math.sin(3 * th + p1) + 0.045 * Math.sin(5 * th + p2))
    case 'cloud':
      return 31 + 4 * Math.abs(Math.sin(3 * th + p1))
    case 'sun':
      return 32 + 2.6 * Math.cos(10 * th + p1)
    case 'droplet':
      return 33 * (0.84 + 0.16 * s)
    default:
      return 34
  }
}

function bodyPath(shape: Shape, p1: number, p2: number) {
  let d = ''
  for (let i = 0; i < 72; i++) {
    const th = (i / 72) * Math.PI * 2
    const r = radius(shape, th, p1, p2)
    d += (i ? 'L' : 'M') + (50 + r * Math.cos(th)).toFixed(2) + ' ' + (53 + r * Math.sin(th)).toFixed(2)
  }
  return d + 'Z'
}

const LINE: CSSProperties = { fill: 'none', stroke: INK, strokeWidth: 3, strokeLinecap: 'square' }

interface EyeProps {
  x: number
  y: number
  kind: EyeKind
  w: number
  h: number
}

function Eye({ x, y, kind, w, h }: EyeProps) {
  if (kind === 'arc') return <path d={`M${x - 6} ${y + 2}Q${x} ${y - 7} ${x + 6} ${y + 2}`} style={LINE} />
  if (kind === 'line') return <path d={`M${x - 5} ${y}H${x + 5}`} style={LINE} />
  const r = kind === 'round' ? Math.max(w, h) * 0.9 : 0
  return (
    <g>
      {r ? (
        <circle cx={x} cy={y} r={r} style={{ fill: INK }} />
      ) : (
        <ellipse cx={x} cy={y} rx={w} ry={h} style={{ fill: INK }} />
      )}
      <rect x={x - (r || w) * 0.5} y={y - (r || h) * 0.6} width={3} height={3} style={{ fill: WHITE }} />
    </g>
  )
}

interface Face {
  eye: EyeKind | 'wink'
  mouth: (y: number) => string
  fillMouth: boolean
  cheeks?: boolean
  droop?: number
}

const FACES: Record<Expression, Face> = {
  idle: { eye: 'oval', mouth: (y) => `M-5 ${y}Q0 ${y + 4} 5 ${y}`, fillMouth: false, cheeks: true },
  happy: { eye: 'arc', mouth: (y) => `M-8 ${y}Q0 ${y + 11} 8 ${y}Z`, fillMouth: true, cheeks: true },
  sad: { eye: 'oval', mouth: (y) => `M-5 ${y + 3}Q0 ${y - 2} 5 ${y + 3}`, fillMouth: false, droop: 2 },
  surprised: { eye: 'round', mouth: (y) => `M-3 ${y + 2}a3 4 0 1 0 6 0a3 4 0 1 0 -6 0`, fillMouth: true },
  sleepy: { eye: 'line', mouth: (y) => `M-3 ${y + 1}H3`, fillMouth: false },
  wink: { eye: 'wink', mouth: (y) => `M-6 ${y}Q0 ${y + 6} 6 ${y}`, fillMouth: false, cheeks: true },
}

interface BlobProps {
  seed: string
  size?: number
  expression?: Expression
  title?: string
}

export function Blob({ seed, size = 48, expression = 'idle', title }: BlobProps) {
  const r = rng(hash(seed.trim().toLowerCase() + '\u00b7cart'))
  r()
  r()
  r()
  const tone: Tone = TONES[Math.floor(r() * TONES.length)]
  const shape: Shape = SHAPES[Math.floor(r() * SHAPES.length)]
  const p1 = r() * 6.28
  const p2 = r() * 6.28
  const gap = 10 + r() * 5
  const ey = 47 + r() * 6
  const ew = 5 + r() * 1.4
  const eh = 6.8 + r() * 2
  const f = FACES[expression]
  const eyeY = ey + (f.droop ?? 0)
  const my = ey + 12 + r() * 3
  const left = f.eye === 'wink' ? 'oval' : f.eye
  const right = f.eye === 'wink' ? 'arc' : f.eye
  const body = bodyPath(shape, p1, p2)

  return (
    <svg
      className="blob"
      viewBox="0 0 100 100"
      width={size}
      height={size}
      role={title ? 'img' : undefined}
      aria-label={title}
      aria-hidden={title ? undefined : true}
    >
      <path d={body} style={{ fill: `var(--${tone})`, stroke: INK, strokeWidth: 3.5, strokeLinejoin: 'round' }} />
      <path
        d={body}
        transform="translate(50 53) scale(0.78) translate(-50 -53) translate(-3 -5)"
        style={{ fill: WHITE, opacity: 0.18 }}
      />
      {f.cheeks && (
        <g style={{ fill: 'var(--cherry)', opacity: 0.35 }}>
          <ellipse cx={50 - gap - 5} cy={eyeY + 8} rx={4} ry={2.6} />
          <ellipse cx={50 + gap + 5} cy={eyeY + 8} rx={4} ry={2.6} />
        </g>
      )}
      <Eye x={50 - gap} y={eyeY} kind={left} w={ew} h={eh} />
      <Eye x={50 + gap} y={eyeY} kind={right} w={ew} h={eh} />
      <g transform="translate(50 0)">
        <path
          d={f.mouth(my)}
          style={
            f.fillMouth
              ? { fill: INK, stroke: INK, strokeWidth: 2, strokeLinejoin: 'round' }
              : { ...LINE, strokeLinecap: 'round', strokeLinejoin: 'round' }
          }
        />
      </g>
    </svg>
  )
}
