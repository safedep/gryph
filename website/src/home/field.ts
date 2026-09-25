import {
  FS_POINTS,
  GL_OPTIONS,
  QUAD,
  REDUCED,
  VS_QUAD,
  attr,
  listeners,
  newPointer,
  program,
  stepPointer,
  type Program,
} from '../shared/gl'

// One swarm of pixels sits behind the page. Each section is a shape it settles
// into. The ground colour travels through a spectrum as you scroll.

const N = 7000

type RGB = [number, number, number]
// x, y, z, r, g, b, a. y runs -1..1 and x runs about -1.35..1.35.
type Point = number[]

interface Shape {
  pts: Point[]
  cell: number
  spin?: number
  pitch?: number
  burst?: number
}

interface Target {
  pos: Float32Array
  col: Float32Array
  cell: number
  spin: number
  pitch: number
  burst: number
}

const WHITE: RGB = [1, 1, 1]
const INK: RGB = [0.047, 0.047, 0.078]
const AMBER: RGB = [1, 0.7, 0]

// g: ground colour. t: text colour on it (w: white, k: ink).
const SCENES = [
  { id: 'dust', g: '#2700ff', t: 'w' },
  { id: 'hero', g: '#2700ff', t: 'w' },
  { id: 'overview', g: '#f5f6fb', t: 'k' },
  { id: 'live', g: '#14151c', t: 'w' },
  { id: 'problem', g: '#5b1fe0', t: 'w' },
  { id: 'monitor', g: '#ffb300', t: 'k' },
  { id: 'policy', g: '#f5f6fb', t: 'k' },
  { id: 'agents', g: '#0b7a69', t: 'w' },
  { id: 'install', g: '#f5f6fb', t: 'k' },
  { id: 'footer', g: '#000000', t: 'w', place: 'wide' },
]
const hex = (h: string): RGB => [1, 3, 5].map((i) => parseInt(h.slice(i, i + 2), 16) / 255) as RGB
const GROUNDS = SCENES.map((s) => hex(s.g))

const lerp = (a: number, b: number, t: number) => a + (b - a) * t
const clamp = (x: number, a: number, b: number) => Math.min(b, Math.max(a, x))
const easeOut = (x: number) => 1 - Math.pow(1 - x, 3)

function gauss() {
  let u = 0
  let v = 0
  while (u === 0) u = Math.random()
  while (v === 0) v = Math.random()
  return Math.sqrt(-2 * Math.log(u)) * Math.cos(2 * Math.PI * v)
}

function shuffle<T>(a: T[]): T[] {
  for (let i = a.length - 1; i > 0; i--) {
    const j = (Math.random() * (i + 1)) | 0
    const t = a[i]
    a[i] = a[j]
    a[j] = t
  }
  return a
}

const VS_POINTS = `
precision highp float;
attribute vec3 aFrom; attribute vec3 aTo;
attribute vec4 aCf;   attribute vec4 aCt;
attribute vec3 aSeed;
uniform float uP, uT, uScaleF, uScaleT, uBreath, uAlpha;
uniform vec2  uRes, uCenterF, uCenterT, uMouse, uTilt;
uniform float uSpinF, uSpinT, uPitchF, uPitchT, uCellF, uCellT;
uniform float uForce, uRadius, uBurst;
varying vec4 vC; varying float vPx;
mat3 rotY(float a){ float c=cos(a), s=sin(a); return mat3(c,0.,-s, 0.,1.,0., s,0.,c); }
mat3 rotX(float a){ float c=cos(a), s=sin(a); return mat3(1.,0.,0., 0.,c,s, 0.,-s,c); }
float ease(float x){ return x<0.5 ? 4.*x*x*x : 1.-pow(-2.*x+2.,3.)/2.; }
void main(){
  float d = aSeed.x*0.55;
  float p = clamp((uP - d)/0.45, 0., 1.);
  float e = ease(p);
  vec3 a = rotX(uPitchF)*rotY(uSpinF)*aFrom;
  vec3 b = rotX(uPitchT)*rotY(uSpinT)*aTo;
  vec3 pos = mix(a, b, e);
  vec3 dir = b - a; float len = length(dir);
  vec3 perp = normalize(vec3(-dir.y, dir.x, (aSeed.z-0.5)*len) + vec3(1e-4));
  pos += perp * sin(p*3.14159) * len * (aSeed.y-0.5) * 0.7;
  pos += normalize(pos + vec3(1e-4)) * sin(p*3.14159) * uBurst * (0.4 + 0.9*aSeed.x);
  pos.xy += uBreath * vec2(sin(uT*1.3 + aSeed.y*40.), cos(uT*1.1 + aSeed.z*40.));
  pos = rotX(uTilt.x)*rotY(uTilt.y)*pos;
  float persp = 1.0/(1.0 + pos.z*0.45);
  vec2 xy = pos.xy*persp;
  float asp = uRes.x/uRes.y;
  float uScale = mix(uScaleF, uScaleT, e);
  vec2 uCenter = mix(uCenterF, uCenterT, e);
  vec2 clip = uCenter + vec2(xy.x/asp, xy.y)*uScale;
  vec2 dm = vec2((clip.x-uMouse.x)*asp, clip.y-uMouse.y);
  float md = length(dm);
  if (md < uRadius) {
    float f = 1.0 - md/uRadius; f = f*f;
    vec2 n = dm/max(md, 1e-4);
    clip += vec2(n.x/asp, n.y) * f * uForce * (0.6 + 0.8*aSeed.z);
  }
  gl_Position = vec4(clip, 0., 1.);
  float cell = mix(uCellF, uCellT, e);
  float px = cell * uScale * uRes.y * 0.5 * persp * (0.84 + 0.22*aSeed.y);
  gl_PointSize = clamp(px, 1.0, 60.0);
  vPx = px;
  vC = mix(aCf, aCt, e);
  vC.a *= clamp(1.0 + pos.z*0.5, 0.3, 1.0) * uAlpha;
}`

const FS_FADE = `precision mediump float; uniform vec4 uC; void main(){ gl_FragColor = uC; }`

const FS_VEIL = `
precision mediump float; varying vec2 vUv;
uniform vec3 uC; uniform float uAxis, uA, uB, uStrength;
void main(){
  float t = uAxis < 0.5 ? (1.0 - vUv.x) : (1.0 - vUv.y);
  float a = smoothstep(uA, uB, t);
  gl_FragColor = vec4(uC, a*uStrength);
}`

function fit(shape: Shape): Target {
  const pts = shuffle(shape.pts.slice())
  const out: Point[] = []
  if (pts.length >= N) out.push(...pts.slice(0, N))
  else {
    out.push(...pts)
    const j = shape.cell * 0.45
    while (out.length < N) {
      const p = pts[(Math.random() * pts.length) | 0]
      out.push([
        p[0] + (Math.random() * 2 - 1) * j,
        p[1] + (Math.random() * 2 - 1) * j,
        p[2],
        p[3],
        p[4],
        p[5],
        p[6],
      ])
    }
    shuffle(out)
  }
  const pos = new Float32Array(N * 3)
  const col = new Float32Array(N * 4)
  for (let i = 0; i < N; i++) {
    const p = out[i]
    pos.set(p.slice(0, 3), i * 3)
    col.set(p.slice(3, 7), i * 4)
  }
  return {
    pos,
    col,
    cell: shape.cell,
    spin: shape.spin || 0,
    pitch: shape.pitch || 0,
    burst: shape.burst == null ? 0.18 : shape.burst,
  }
}

function dust(): Shape {
  const pts: Point[] = []
  for (let i = 0; i < N; i++)
    pts.push([
      (Math.random() * 2 - 1) * 1.5,
      (Math.random() * 2 - 1) * 1.05,
      (Math.random() * 2 - 1) * 0.8,
      1,
      1,
      1,
      0.25 + 0.6 * Math.random(),
    ])
  return { pts, cell: 0.011 }
}

function galaxy(): Shape {
  const pts: Point[] = []
  for (let i = 0; i < N; i++) {
    const core = i < N * 0.28
    const r = core ? Math.abs(gauss()) * 0.13 : 0.16 + 0.84 * Math.pow(Math.random(), 0.65)
    const th = (i % 2) * Math.PI + r * 4.2 + gauss() * (core ? 3 : 0.3) + Math.random() * 0.15
    const x = r * Math.cos(th)
    const z = r * Math.sin(th)
    const y = gauss() * 0.045 * (1.2 - r * 0.6)
    const t = clamp((r - 0.1) / 0.32, 0, 1)
    pts.push([
      x,
      y,
      z,
      lerp(AMBER[0], 1, t),
      lerp(AMBER[1], 1, t),
      lerp(AMBER[2], 1, t),
      core ? 1 : 0.55 + 0.45 * Math.random(),
    ])
  }
  return { pts, cell: 0.014, spin: 0.16, pitch: 0.62 }
}

// Your machine: clusters of files and processes. One of them is the agent's session, lit.
function network(): Shape {
  const BLUE = hex('#1e2be6')
  const comms = [
    { x: -0.82, y: 0.42, r: 0.26, n: 14 },
    { x: 0.05, y: 0.62, r: 0.3, n: 16 },
    { x: 0.85, y: 0.35, r: 0.3, n: 15, hot: true },
    { x: -0.55, y: -0.35, r: 0.3, n: 15 },
    { x: 0.35, y: -0.15, r: 0.28, n: 12 },
    { x: 0.95, y: -0.6, r: 0.24, n: 10 },
    { x: -0.1, y: -0.75, r: 0.22, n: 9 },
  ]
  const nodes: { x: number; y: number; z: number; s: number; c: number; hot: boolean }[] = []
  comms.forEach((c, ci) => {
    for (let k = 0; k < c.n; k++) {
      const a = Math.random() * 6.283
      const rr = c.r * Math.sqrt(Math.random())
      nodes.push({
        x: c.x + Math.cos(a) * rr,
        y: c.y + Math.sin(a) * rr * 0.8,
        z: (Math.random() - 0.5) * 0.3,
        s: 0.018 + Math.random() * 0.028,
        c: ci,
        hot: !!c.hot,
      })
    }
  })
  const edges: [number, number, boolean?][] = []
  const has = (a: number, b: number) =>
    edges.some((e) => (e[0] === a && e[1] === b) || (e[0] === b && e[1] === a))
  nodes.forEach((n, i) => {
    nodes
      .map((m, j) => ({ j, d: Math.hypot(m.x - n.x, m.y - n.y) }))
      .filter((o) => o.j !== i && nodes[o.j].c === n.c)
      .sort((a, b) => a.d - b.d)
      .slice(0, 2)
      .forEach((o) => {
        if (!has(i, o.j)) edges.push([i, o.j])
      })
  })
  let cross = 0
  while (cross < 4) {
    const a = (Math.random() * nodes.length) | 0
    const b = (Math.random() * nodes.length) | 0
    if (nodes[a].c !== nodes[b].c && !has(a, b)) {
      edges.push([a, b, true])
      cross++
    }
  }
  const cell = 0.014
  const step = cell * 0.72
  const pts: Point[] = []
  nodes.forEach((n) => {
    const col = n.hot ? BLUE : INK
    for (let gy = -n.s; gy <= n.s; gy += step)
      for (let gx = -n.s; gx <= n.s; gx += step)
        if (gx * gx + gy * gy <= n.s * n.s)
          pts.push([n.x + gx, n.y + gy, n.z, col[0], col[1], col[2], 1])
  })
  edges.forEach(([a, b, far]) => {
    const A = nodes[a]
    const B = nodes[b]
    const hot = A.hot && B.hot
    const col = hot ? BLUE : INK
    const steps = Math.max(2, Math.floor(Math.hypot(B.x - A.x, B.y - A.y) / 0.03))
    for (let k = 1; k < steps; k++) {
      const u = k / steps
      pts.push([
        A.x + (B.x - A.x) * u,
        A.y + (B.y - A.y) * u,
        A.z + (B.z - A.z) * u,
        col[0],
        col[1],
        col[2],
        hot ? 0.9 : far ? 0.28 : 0.5,
      ])
    }
  })
  return { pts, cell, burst: 0.9 }
}

// A stop sign in Claude's terracotta, to echo the terminal bar below it.
function stopSign(): Shape {
  const R = 0.64
  const step = 0.013
  const pts: Point[] = []
  const FILL: RGB = [0.847, 0.467, 0.337]
  for (let y = -R; y <= R; y += step)
    for (let x = -R; x <= R; x += step) {
      let d = -Infinity
      for (let k = 0; k < 8; k++) {
        const a = (k * Math.PI) / 4
        d = Math.max(d, x * Math.cos(a) + y * Math.sin(a))
      }
      if (d > R) continue
      const border = d > R - 0.055
      const bar = Math.abs(y) < 0.07 && Math.abs(x) < R - 0.16
      const c = border || bar ? WHITE : FILL
      pts.push([
        x,
        y,
        (Math.random() - 0.5) * 0.06,
        c[0],
        c[1],
        c[2],
        border ? 1 : 0.88 + 0.12 * Math.random(),
      ])
    }
  return { pts, cell: step, burst: 0.45 }
}

function squares(): Shape {
  const pts: Point[] = []
  const side = 0.62
  const gap = 0.16
  const cols = 3
  const rows = 2
  const per = Math.floor(Math.sqrt(N / 6))
  const tw = cols * side + (cols - 1) * gap
  const th = rows * side + (rows - 1) * gap
  for (let r = 0; r < rows; r++)
    for (let c = 0; c < cols; c++) {
      const x0 = -tw / 2 + c * (side + gap)
      const y0 = th / 2 - r * (side + gap) - side
      const z = (r + c) % 2 ? 0.14 : -0.14
      for (let j = 0; j < per; j++)
        for (let i = 0; i < per; i++)
          pts.push([x0 + ((i + 0.5) / per) * side, y0 + ((j + 0.5) / per) * side, z, 1, 1, 1, 1])
    }
  return { pts, cell: side / per }
}

function glyph(str: string, color: RGB): Shape {
  const H = 560
  const c = document.createElement('canvas')
  const ctx = c.getContext('2d')!
  const font = `900 ${Math.round(H * 0.95)}px "Archivo", "Arial Black", Impact, sans-serif`
  ctx.font = font
  let m = ctx.measureText(str)
  const W = Math.ceil(m.width + H * 0.12)
  c.width = W
  c.height = H
  ctx.font = font
  ctx.fillStyle = '#fff'
  ctx.textAlign = 'center'
  ctx.textBaseline = 'alphabetic'
  m = ctx.measureText(str)
  const asc = m.actualBoundingBoxAscent || H * 0.5
  const desc = m.actualBoundingBoxDescent || 0
  ctx.fillText(str, W / 2, H / 2 + (asc - desc) / 2)
  const d = ctx.getImageData(0, 0, W, H).data
  let area = 0
  for (let i = 3; i < d.length; i += 4) if (d[i] > 128) area++
  const step = Math.max(2, Math.sqrt(area / N))
  const sc = Math.min(1, (1.35 * H) / W)
  const pts: Point[] = []
  for (let y = step / 2; y < H; y += step)
    for (let x = step / 2; x < W; x += step) {
      const i = ((y | 0) * W + (x | 0)) * 4
      if (d[i + 3] > 128)
        pts.push([
          ((2 * x) / W - 1) * (W / H) * sc,
          (1 - (2 * y) / H) * sc,
          0,
          color[0],
          color[1],
          color[2],
          1,
        ])
    }
  return { pts, cell: ((2 * step) / H) * sc }
}

// The last shape: the SafeDep mark, sampled from its own SVG so each pixel keeps the logo's teal.
const SAFEDEP_SVG =
  '<svg width="220" height="280" viewBox="40 10 220 280" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M90.026 208.634L47.1685 193.126L46.9414 75.3646L89.7989 90.8729L89.8309 109.084L89.8246 109.153L90.026 208.634Z" fill="#0D9488"/><path d="M89.7989 90.8721L46.9414 75.3638L210.176 16L253.034 31.5083L115.683 81.4654L115.536 81.5016L89.7989 90.8721Z" fill="url(#paint0_linear_9227_368)"/><path d="M90.0247 208.631L89.823 109.151L89.8292 109.082L89.7974 90.8705L115.534 81.5001L115.682 81.4638L253.033 31.5068L253.058 49.3727L118.31 100.781L107.966 104.533L107.989 104.679L107.995 115.727L108.168 202.035L90.0247 208.631Z" fill="url(#paint1_linear_9227_368)"/><path d="M209.974 91.3661L252.831 106.874L253.058 224.635L210.201 209.127L210.169 190.916L210.175 190.847L209.974 91.3661Z" fill="#0D9488"/><path d="M210.2 209.128L253.058 224.636L89.8227 284L46.9652 268.492L184.316 218.535L184.464 218.498L210.2 209.128Z" fill="url(#paint2_linear_9227_368)"/><path d="M209.975 91.3688L210.177 190.849L210.17 190.918L210.202 209.129L184.465 218.5L184.318 218.536L46.967 268.493L46.9414 250.627L181.689 199.219L192.033 195.467L192.011 195.321L192.005 184.273L191.831 97.9646L209.975 91.3688Z" fill="url(#paint3_linear_9227_368)"/><defs><linearGradient id="paint0_linear_9227_368" x1="252.703" y1="31.5518" x2="46.941" y2="75.2165" gradientUnits="userSpaceOnUse"><stop stop-color="#00FFE3"/><stop offset="1" stop-color="#09524A"/></linearGradient><linearGradient id="paint1_linear_9227_368" x1="252.704" y1="31.5518" x2="74.6708" y2="48.6198" gradientUnits="userSpaceOnUse"><stop stop-color="#00FFE3"/><stop offset="1" stop-color="#09524A"/></linearGradient><linearGradient id="paint2_linear_9227_368" x1="47.2956" y1="268.448" x2="253.058" y2="224.783" gradientUnits="userSpaceOnUse"><stop stop-color="#00FFE3"/><stop offset="1" stop-color="#09524A"/></linearGradient><linearGradient id="paint3_linear_9227_368" x1="47.2953" y1="268.448" x2="225.329" y2="251.38" gradientUnits="userSpaceOnUse"><stop stop-color="#00FFE3"/><stop offset="1" stop-color="#09524A"/></linearGradient></defs></svg>'

function picture(svg: string, size: number): Promise<Shape> {
  const img = new Image()
  img.src = 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(svg)
  return img.decode().then(() => {
    const H = 600
    const W = Math.round((H * img.naturalWidth) / img.naturalHeight)
    const c = document.createElement('canvas')
    c.width = W
    c.height = H
    const ctx = c.getContext('2d')!
    ctx.drawImage(img, 0, 0, W, H)
    const d = ctx.getImageData(0, 0, W, H).data
    let area = 0
    for (let k = 3; k < d.length; k += 4) if (d[k] > 128) area++
    const step = Math.max(2, Math.sqrt(area / N))
    const pts: Point[] = []
    for (let y = step / 2; y < H; y += step)
      for (let x = step / 2; x < W; x += step) {
        const k = ((y | 0) * W + (x | 0)) * 4
        if (d[k + 3] > 128)
          pts.push([
            ((2 * x) / W - 1) * (W / H) * size,
            (1 - (2 * y) / H) * size,
            (Math.random() - 0.5) * 0.06,
            d[k] / 255,
            d[k + 1] / 255,
            d[k + 2] / 255,
            1,
          ])
      }
    return { pts, cell: ((2 * step) / H) * size, burst: 0.5 }
  })
}

interface Gfx {
  gl: WebGLRenderingContext
  pt: Program
  fade: Program
  veil: Program
  buf: Record<string, WebGLBuffer | null>
}

function setupGL(canvas: HTMLCanvasElement): Gfx | null {
  let gl: WebGLRenderingContext | null = null
  try {
    gl = canvas.getContext('webgl', { ...GL_OPTIONS, powerPreference: 'high-performance' })
  } catch {
    gl = null
  }
  if (!gl) return null
  try {
    const pt = program(
      gl,
      VS_POINTS,
      FS_POINTS,
      ['aFrom', 'aTo', 'aCf', 'aCt', 'aSeed'],
      [
        'uP',
        'uT',
        'uScaleF',
        'uScaleT',
        'uBreath',
        'uAlpha',
        'uRes',
        'uCenterF',
        'uCenterT',
        'uMouse',
        'uTilt',
        'uSpinF',
        'uSpinT',
        'uPitchF',
        'uPitchT',
        'uCellF',
        'uCellT',
        'uForce',
        'uRadius',
        'uBurst',
      ],
    )
    const fade = program(gl, VS_QUAD, FS_FADE, ['a'], ['uC'])
    const veil = program(gl, VS_QUAD, FS_VEIL, ['a'], ['uC', 'uAxis', 'uA', 'uB', 'uStrength'])
    const buf: Gfx['buf'] = {}
    ;['from', 'to', 'cf', 'ct', 'seed', 'quad'].forEach((k) => (buf[k] = gl.createBuffer()))
    const seed = new Float32Array(N * 3)
    for (let i = 0; i < seed.length; i++) seed[i] = Math.random()
    gl.bindBuffer(gl.ARRAY_BUFFER, buf.seed)
    gl.bufferData(gl.ARRAY_BUFFER, seed, gl.STATIC_DRAW)
    gl.bindBuffer(gl.ARRAY_BUFFER, buf.quad)
    gl.bufferData(gl.ARRAY_BUFFER, QUAD, gl.STATIC_DRAW)
    gl.enable(gl.BLEND)
    gl.blendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA)
    gl.disable(gl.DEPTH_TEST)
    return { gl, pt, fade, veil, buf }
  } catch {
    return null
  }
}

// Starts the swarm on the canvas. Returns a function that stops it.
export function startField(canvas: HTMLCanvasElement): () => void {
  const root = document.documentElement
  const secs = [...document.querySelectorAll<HTMLElement>('[data-scene]')]
  const events = listeners()
  let stopped = false

  const gfx = setupGL(canvas)
  if (!gfx) root.classList.add('no-gl')

  // Build the non-text shapes now. The glyphs wait for the display font.
  const targets: Target[] = new Array(SCENES.length)
  const DUST = fit(dust())
  targets[0] = DUST
  targets[1] = fit(galaxy())
  targets[2] = fit(network())
  targets[3] = fit(stopSign())
  targets[7] = fit(squares())
  for (let i = 0; i < targets.length; i++) if (!targets[i]) targets[i] = DUST

  let boundPair = -1
  const buildGlyphs = () => {
    if (stopped) return
    targets[4] = fit(glyph('?', WHITE))
    targets[5] = fit(glyph('JSONL', INK))
    targets[6] = fit(glyph('rm -rf', INK))
    targets[8] = fit(glyph('$', INK))
    boundPair = -1
  }
  Promise.race([
    document.fonts?.load ? document.fonts.load('900 100px "Archivo"') : Promise.resolve(),
    new Promise((r) => setTimeout(r, 2600)),
  ]).then(buildGlyphs, buildGlyphs)
  picture(SAFEDEP_SVG, 0.5).then(
    (shape) => {
      if (stopped) return
      targets[9] = fit(shape)
      boundPair = -1
    },
    () => {},
  )

  let W = 1
  let H = 1
  let portrait = false
  let center = [0.42, -0.02]
  let scale = 0.74
  let needClear = true
  let wideCenter = [0, 0.15]
  let wideScale = 1
  let heroY = -0.02

  const placement = (k: number): [number[], number] => {
    if (SCENES[k].place === 'wide') return [wideCenter, wideScale]
    // The galaxy sits on the line of the hero copy.
    if (k <= 1 && !portrait) return [[center[0], heroY], scale]
    return [center, scale]
  }

  let anchors: { k: number; y: number }[] = []
  const computeAnchors = () => {
    const sy = scrollY
    anchors = secs.map((s) => {
      const r = s.getBoundingClientRect()
      return { k: +s.dataset.scene!, y: Math.max(0, r.top + sy + r.height / 2 - H / 2) }
    })
    anchors[0].y = 0
    const hc = document.querySelector('.hero .copy')
    if (hc && hc.firstElementChild) {
      const a = hc.firstElementChild.getBoundingClientRect()
      const b = hc.lastElementChild!.getBoundingClientRect()
      heroY = 1 - (2 * ((a.top + b.bottom) / 2 + sy)) / H - 0.1
    }
  }

  const resize = () => {
    const dpr = Math.min(2, devicePixelRatio || 1)
    W = innerWidth
    H = innerHeight
    canvas.width = Math.round(W * dpr)
    canvas.height = Math.round(H * dpr)
    if (gfx) gfx.gl.viewport(0, 0, canvas.width, canvas.height)
    const asp = W / H
    portrait = W < 900 || H > W * 1.15
    const gutter = Math.min(112, Math.max(24, 0.06 * W))
    const stageW = Math.min(1280, W - 2 * gutter)
    if (portrait) {
      center = [0, 0.5]
      scale = Math.min(0.36, (0.9 * asp) / 1.35)
      wideCenter = [0, 0.38]
    } else {
      // The right half of the stage.
      center = [stageW / (2 * W), -0.02]
      scale = Math.min(0.8, (((0.54 * stageW) / W) * asp) / 1.35)
      wideCenter = [0, 0.22]
    }
    wideScale = Math.min(1.3, (0.92 * asp) / 1.35)
    needClear = true
    computeAnchors()
  }
  events.on('resize', resize, { passive: true })
  const observer = 'ResizeObserver' in window ? new ResizeObserver(computeAnchors) : null
  observer?.observe(document.body)
  resize()

  // Hold the shape near each section and morph between them.
  const sceneFromScroll = (y: number) => {
    if (!anchors.length) return 1
    if (y <= anchors[0].y) return anchors[0].k
    for (let i = 0; i < anchors.length - 1; i++) {
      const a = anchors[i]
      const b = anchors[i + 1]
      if (y < b.y) {
        const u = (y - a.y) / Math.max(1, b.y - a.y)
        const v = clamp((u - 0.22) / 0.56, 0, 1)
        return a.k + (b.k - a.k) * v
      }
    }
    return anchors[anchors.length - 1].k
  }

  const mouse = newPointer()
  const aim = (e: PointerEvent) => {
    mouse.tx = (e.clientX / W) * 2 - 1
    mouse.ty = 1 - (e.clientY / H) * 2
    mouse.seen = true
  }
  events.on('pointermove', aim, { passive: true })
  events.on(
    'pointerdown',
    (e) => {
      mouse.down = true
      aim(e)
    },
    { passive: true },
  )
  events.on('pointerup', () => (mouse.down = false), { passive: true })
  events.on('pointercancel', () => (mouse.down = false), { passive: true })
  events.on('blur', () => (mouse.down = false))

  let lastG = ''
  let lastMode = ''
  const setGround = (g: number[]) => {
    const s = `rgb(${Math.round(g[0] * 255)},${Math.round(g[1] * 255)},${Math.round(g[2] * 255)})`
    if (s !== lastG) {
      root.style.setProperty('--g', s)
      lastG = s
    }
  }
  const setMode = (m: string) => {
    if (m !== lastMode) {
      root.classList.toggle('ink-text', m === 'k')
      lastMode = m
    }
  }

  let S = 1
  let last = performance.now()
  const t0 = last
  let raf = 0

  const frame = (now: number) => {
    const dt = Math.min(0.05, (now - last) / 1000)
    last = now
    const t = now / 1000

    const St = sceneFromScroll(scrollY)
    S += (St - S) * (REDUCED ? 1 : 1 - Math.exp(-dt * 5.5))
    root.classList.toggle('scrolled', scrollY > 24)

    const T = targets.length
    const i = clamp(Math.floor(S), 0, T - 2)
    const f = clamp(S - i, 0, 1)
    const g0 = GROUNDS[i]
    const g1 = GROUNDS[i + 1]
    const g = [lerp(g0[0], g1[0], f), lerp(g0[1], g1[1], f), lerp(g0[2], g1[2], f)]
    setGround(g)
    setMode(SCENES[clamp(Math.round(S), 0, T - 1)].t)

    if (gfx) {
      const { gl, pt, fade, veil, buf } = gfx
      if (i !== boundPair) {
        gl.bindBuffer(gl.ARRAY_BUFFER, buf.from)
        gl.bufferData(gl.ARRAY_BUFFER, targets[i].pos, gl.DYNAMIC_DRAW)
        gl.bindBuffer(gl.ARRAY_BUFFER, buf.to)
        gl.bufferData(gl.ARRAY_BUFFER, targets[i + 1].pos, gl.DYNAMIC_DRAW)
        gl.bindBuffer(gl.ARRAY_BUFFER, buf.cf)
        gl.bufferData(gl.ARRAY_BUFFER, targets[i].col, gl.DYNAMIC_DRAW)
        gl.bindBuffer(gl.ARRAY_BUFFER, buf.ct)
        gl.bufferData(gl.ARRAY_BUFFER, targets[i + 1].col, gl.DYNAMIC_DRAW)
        boundPair = i
      }

      stepPointer(mouse, dt, 0.2, 0.3)
      const a = targets[i]
      const b = targets[i + 1]

      // 1. Fade the last frame toward the ground colour. This leaves the trails.
      const fadeA = REDUCED || needClear ? 1 : 1 - Math.pow(0.7, dt * 60)
      needClear = false
      gl.useProgram(fade.p)
      attr(gl, fade, 'a', buf.quad, 2)
      gl.uniform4f(fade.u.uC, g[0], g[1], g[2], fadeA)
      gl.drawArrays(gl.TRIANGLES, 0, 3)

      // 2. The swarm.
      gl.useProgram(pt.p)
      attr(gl, pt, 'aFrom', buf.from, 3)
      attr(gl, pt, 'aTo', buf.to, 3)
      attr(gl, pt, 'aCf', buf.cf, 4)
      attr(gl, pt, 'aCt', buf.ct, 4)
      attr(gl, pt, 'aSeed', buf.seed, 3)
      gl.uniform1f(pt.u.uP, f)
      gl.uniform1f(pt.u.uT, t)
      const [cF, sF] = placement(i)
      const [cT, sT] = placement(i + 1)
      gl.uniform1f(pt.u.uScaleF, sF)
      gl.uniform1f(pt.u.uScaleT, sT)
      gl.uniform2f(pt.u.uCenterF, cF[0], cF[1])
      gl.uniform2f(pt.u.uCenterT, cT[0], cT[1])
      gl.uniform1f(pt.u.uBreath, REDUCED ? 0 : 0.0035)
      const arrive = REDUCED ? 1 : clamp((now - t0) / 700, 0, 1)
      gl.uniform1f(pt.u.uAlpha, (portrait ? 0.62 : 1) * easeOut(arrive))
      gl.uniform2f(pt.u.uRes, canvas.width, canvas.height)
      gl.uniform2f(pt.u.uMouse, mouse.x, mouse.y)
      gl.uniform2f(pt.u.uTilt, mouse.tilt[0], mouse.tilt[1])
      gl.uniform1f(pt.u.uSpinF, REDUCED ? 0 : a.spin * t)
      gl.uniform1f(pt.u.uSpinT, REDUCED ? 0 : b.spin * t)
      gl.uniform1f(pt.u.uPitchF, a.pitch)
      gl.uniform1f(pt.u.uPitchT, b.pitch)
      gl.uniform1f(pt.u.uCellF, a.cell)
      gl.uniform1f(pt.u.uCellT, b.cell)
      gl.uniform1f(pt.u.uForce, mouse.force)
      gl.uniform1f(pt.u.uRadius, mouse.radius)
      gl.uniform1f(pt.u.uBurst, b.burst)
      gl.drawArrays(gl.POINTS, 0, N)

      // 3. A veil of ground colour where the words sit. No veil under the wordmark.
      gl.useProgram(veil.p)
      attr(gl, veil, 'a', buf.quad, 2)
      gl.uniform3f(veil.u.uC, g[0], g[1], g[2])
      gl.uniform1f(veil.u.uStrength, 0.97 * (1 - clamp(S - (T - 2), 0, 1)))
      gl.uniform1f(veil.u.uAxis, portrait ? 1 : 0)
      gl.uniform1f(veil.u.uA, portrait ? 0.45 : 0.46)
      gl.uniform1f(veil.u.uB, 0.62)
      gl.drawArrays(gl.TRIANGLES, 0, 3)
    }

    raf = requestAnimationFrame(frame)
  }
  raf = requestAnimationFrame(frame)

  return () => {
    stopped = true
    cancelAnimationFrame(raf)
    events.off()
    observer?.disconnect()
  }
}
