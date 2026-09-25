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
} from '../shared/gl'

// Each agent action rains through a neck into the record: an amber pool.

const N = 6000
const PANEL = [0x0b / 255, 0x7a / 255, 0x69 / 255]

const VS = `
precision highp float;
attribute vec3 aPos; attribute vec4 aCol; attribute vec3 aSeed; attribute float aKind;
uniform float uT, uScale, uCellA, uCellB, uAlpha, uBreath, uForce, uRadius, uFlow;
uniform vec2 uRes, uCenter, uMouse, uTilt;
varying vec4 vC; varying float vPx;
mat3 rotY(float a){ float c=cos(a), s=sin(a); return mat3(c,0.,-s, 0.,1.,0., s,0.,c); }
mat3 rotX(float a){ float c=cos(a), s=sin(a); return mat3(1.,0.,0., 0.,c,s, 0.,-s,c); }
/* half-width of the stream at height y: wide above, a neck at -0.12, a slim stream below */
float spread(float y){
  float s = smoothstep(-0.12, 0.30, y);
  float above = mix(0.07, 1.0, pow(s, 1.6));
  float below = 0.07 + 0.06 * (1.0 - smoothstep(-0.5, -0.12, y));
  return y > -0.12 ? above : below;
}
void main(){
  vec3 pos; vec4 col; float cell;
  if (aKind < 0.5) {
    /* a falling particle: its place on the way down depends only on time and its seed */
    float u = fract(aSeed.x + uT * uFlow * (0.75 + 0.5*aSeed.z));
    float y = 1.05 - u * 1.6;
    float x = (aSeed.y * 2.0 - 1.0) * spread(y) + 0.012 * sin(uT * 1.7 + aSeed.y * 30.0);
    float a = mix(0.18, 0.95, 1.0 - smoothstep(-0.1, 0.9, y));
    a *= 1.0 - smoothstep(0.9, 1.05, y);
    vec3 c = mix(vec3(1.0), vec3(1.0, 0.70, 0.0), 1.0 - smoothstep(-0.5, -0.1, y));
    a *= smoothstep(-0.55, -0.38, y);
    pos = vec3(x, y, (aSeed.z - 0.5) * 0.5);
    col = vec4(c, a); cell = uCellA;
  } else {
    pos = aPos; col = aCol; cell = uCellB;
    pos.xy += uBreath * vec2(sin(uT*1.3 + aSeed.y*40.), cos(uT*1.1 + aSeed.z*40.));
  }
  pos = rotX(uTilt.x)*rotY(uTilt.y)*pos;
  float persp = 1.0/(1.0 + pos.z*0.45);
  vec2 xy = pos.xy*persp;
  float asp = uRes.x/uRes.y;
  vec2 clip = uCenter + vec2(xy.x/asp, xy.y)*uScale;
  vec2 dm = vec2((clip.x-uMouse.x)*asp, clip.y-uMouse.y);
  float md = length(dm);
  if (md < uRadius) { float f = 1.0 - md/uRadius; f = f*f; vec2 n = dm/max(md, 1e-4); clip += vec2(n.x/asp, n.y) * f * uForce * (0.6 + 0.8*aSeed.z); }
  gl_Position = vec4(clip, 0., 1.);
  float px = cell * uScale * uRes.y * 0.5 * persp * (0.84 + 0.22*aSeed.y);
  gl_PointSize = clamp(px, 1.0, 60.0); vPx = px;
  vC = col; vC.a *= uAlpha;
}`

// The ground of the box, painted each frame with a little memory so the pixels leave trails.
const FQ = `precision mediump float; varying vec2 vUv; uniform vec3 uA, uB; uniform float uAxis, uSplit, uFade;
void main(){ float t = uAxis < 0.5 ? vUv.x : (1.0 - vUv.y); vec3 c = t < uSplit ? uA : uB; gl_FragColor = vec4(c, uFade); }`

const CELL_A = 0.016
const CELL_B = 0.02

// A dense amber disc that the stream lands in. All other points fall.
function buffers() {
  const pool: number[][] = []
  const PR = 0.3
  const PY = -0.72
  const step = CELL_B * 0.72
  for (let y = -PR; y <= PR; y += step)
    for (let x = -PR; x <= PR; x += step) {
      if (x * x + y * y <= PR * PR) {
        const edge = Math.hypot(x, y) / PR
        pool.push([
          x + (Math.random() - 0.5) * step * 0.3,
          PY + y + (Math.random() - 0.5) * step * 0.3,
          (Math.random() - 0.5) * 0.12,
          1,
          0.7 + 0.12 * edge,
          0.0 + 0.18 * edge,
          0.85 + 0.15 * Math.random(),
        ])
      }
    }
  const pos = new Float32Array(N * 3)
  const col = new Float32Array(N * 4)
  const seed = new Float32Array(N * 3)
  const kind = new Float32Array(N)
  for (let i = 0; i < N; i++) {
    seed.set([Math.random(), Math.random(), Math.random()], i * 3)
    if (i < pool.length) {
      const p = pool[i]
      kind[i] = 1
      pos.set(p.slice(0, 3), i * 3)
      col.set(p.slice(3, 7), i * 4)
    } else {
      col[i * 4 + 3] = 1
    }
  }
  return { pos, col, seed, kind, quad: QUAD }
}

// Starts the swarm on the canvas. Returns a function that stops it.
export function startField(canvas: HTMLCanvasElement): () => void {
  const root = document.documentElement
  let gl: WebGLRenderingContext | null = null
  try {
    gl = canvas.getContext('webgl', GL_OPTIONS)
  } catch {
    gl = null
  }
  if (!gl) {
    root.classList.add('no-gl')
    return () => {}
  }

  let pt, fade
  try {
    pt = program(
      gl,
      VS,
      FS_POINTS,
      ['aPos', 'aCol', 'aSeed', 'aKind'],
      [
        'uT',
        'uScale',
        'uCellA',
        'uCellB',
        'uAlpha',
        'uBreath',
        'uForce',
        'uRadius',
        'uFlow',
        'uRes',
        'uCenter',
        'uMouse',
        'uTilt',
      ],
    )
    fade = program(gl, VS_QUAD, FQ, ['a'], ['uA', 'uB', 'uAxis', 'uSplit', 'uFade'])
  } catch {
    root.classList.add('no-gl')
    return () => {}
  }

  const buf: Record<string, WebGLBuffer | null> = {}
  Object.entries(buffers()).forEach(([k, d]) => {
    buf[k] = gl.createBuffer()
    gl.bindBuffer(gl.ARRAY_BUFFER, buf[k])
    gl.bufferData(gl.ARRAY_BUFFER, d, gl.STATIC_DRAW)
  })
  gl.enable(gl.BLEND)
  gl.blendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA)

  const events = listeners()
  const center = [0, 0.02]
  let scale = 0.9
  let needClear = true
  const resize = () => {
    const dpr = Math.min(2, devicePixelRatio || 1)
    const r = canvas.getBoundingClientRect()
    const W = Math.max(1, r.width)
    const H = Math.max(1, r.height)
    canvas.width = Math.round(W * dpr)
    canvas.height = Math.round(H * dpr)
    gl.viewport(0, 0, canvas.width, canvas.height)
    scale = Math.min(0.92, (0.98 * W) / H)
    needClear = true
  }
  events.on('resize', resize, { passive: true })
  resize()
  const observer = 'ResizeObserver' in window ? new ResizeObserver(resize) : null
  observer?.observe(canvas)

  const mouse = newPointer()
  events.on(
    'pointermove',
    (e) => {
      const r = canvas.getBoundingClientRect()
      mouse.tx = ((e.clientX - r.left) / r.width) * 2 - 1
      mouse.ty = 1 - ((e.clientY - r.top) / r.height) * 2
      mouse.seen = true
    },
    { passive: true },
  )
  events.on(
    'pointerdown',
    (e) => {
      if ((e.target as Element).closest?.('button, a, summary, pre')) return
      mouse.down = true
    },
    { passive: true },
  )
  events.on('pointerup', () => (mouse.down = false), { passive: true })
  events.on('pointercancel', () => (mouse.down = false), { passive: true })

  let last = performance.now()
  const t0 = last
  let raf = 0
  const frame = (now: number) => {
    const dt = Math.min(0.05, (now - last) / 1000)
    last = now
    const t = now / 1000
    stepPointer(mouse, dt, 0.18, 0.26)

    const fadeA = REDUCED || needClear ? 1 : 1 - Math.pow(0.7, dt * 60)
    needClear = false
    gl.useProgram(fade.p)
    attr(gl, fade, 'a', buf.quad, 2)
    gl.uniform3f(fade.u.uA, PANEL[0], PANEL[1], PANEL[2])
    gl.uniform3f(fade.u.uB, PANEL[0], PANEL[1], PANEL[2])
    gl.uniform1f(fade.u.uAxis, 0)
    gl.uniform1f(fade.u.uSplit, 2.0)
    gl.uniform1f(fade.u.uFade, fadeA)
    gl.drawArrays(gl.TRIANGLES, 0, 3)

    gl.useProgram(pt.p)
    attr(gl, pt, 'aPos', buf.pos, 3)
    attr(gl, pt, 'aCol', buf.col, 4)
    attr(gl, pt, 'aSeed', buf.seed, 3)
    attr(gl, pt, 'aKind', buf.kind, 1)
    const arrive = REDUCED ? 1 : Math.min(1, (now - t0) / 700)
    gl.uniform1f(pt.u.uT, t)
    gl.uniform1f(pt.u.uScale, scale)
    gl.uniform1f(pt.u.uCellA, CELL_A)
    gl.uniform1f(pt.u.uCellB, CELL_B)
    gl.uniform1f(pt.u.uFlow, REDUCED ? 0 : 0.055)
    gl.uniform1f(pt.u.uAlpha, 1 - Math.pow(1 - arrive, 3))
    gl.uniform1f(pt.u.uBreath, REDUCED ? 0 : 0.0025)
    gl.uniform1f(pt.u.uForce, mouse.force)
    gl.uniform1f(pt.u.uRadius, mouse.radius)
    gl.uniform2f(pt.u.uRes, canvas.width, canvas.height)
    gl.uniform2f(pt.u.uCenter, center[0], center[1])
    gl.uniform2f(pt.u.uMouse, mouse.x, mouse.y)
    gl.uniform2f(pt.u.uTilt, mouse.tilt[0], mouse.tilt[1])
    gl.drawArrays(gl.POINTS, 0, N)

    raf = requestAnimationFrame(frame)
  }
  raf = requestAnimationFrame(frame)

  return () => {
    cancelAnimationFrame(raf)
    events.off()
    observer?.disconnect()
  }
}
