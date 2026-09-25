export const REDUCED =
  typeof matchMedia !== 'undefined' && matchMedia('(prefers-reduced-motion: reduce)').matches

export const GL_OPTIONS: WebGLContextAttributes = {
  alpha: false,
  antialias: false,
  depth: false,
  stencil: false,
  preserveDrawingBuffer: true,
}

// One full-screen triangle.
export const QUAD = new Float32Array([-1, -1, 3, -1, -1, 3])

export const VS_QUAD = `attribute vec2 a; varying vec2 vUv; void main(){ vUv = a*0.5+0.5; gl_Position = vec4(a,0.,1.); }`

// A soft rounded square for each point.
export const FS_POINTS = `
precision mediump float;
varying vec4 vC; varying float vPx;
void main(){
  vec2 p = gl_PointCoord*2.0 - 1.0;
  float r = 0.3;
  vec2 q = abs(p) - (1.0 - r);
  float d = length(max(q, 0.0)) + min(max(q.x, q.y), 0.0) - r;
  float aa = 1.6/max(vPx, 1.0);
  float a = 1.0 - smoothstep(-aa, aa, d);
  if (a <= 0.01) discard;
  gl_FragColor = vec4(vC.rgb, vC.a*a);
}`

export interface Program {
  p: WebGLProgram
  a: Record<string, number>
  u: Record<string, WebGLUniformLocation | null>
}

function shader(gl: WebGLRenderingContext, type: number, src: string): WebGLShader {
  const s = gl.createShader(type)!
  gl.shaderSource(s, src)
  gl.compileShader(s)
  if (!gl.getShaderParameter(s, gl.COMPILE_STATUS)) throw new Error(gl.getShaderInfoLog(s) ?? '')
  return s
}

export function program(
  gl: WebGLRenderingContext,
  vs: string,
  fs: string,
  attrs: string[],
  unis: string[],
): Program {
  const p = gl.createProgram()!
  gl.attachShader(p, shader(gl, gl.VERTEX_SHADER, vs))
  gl.attachShader(p, shader(gl, gl.FRAGMENT_SHADER, fs))
  gl.linkProgram(p)
  if (!gl.getProgramParameter(p, gl.LINK_STATUS)) throw new Error(gl.getProgramInfoLog(p) ?? '')
  const o: Program = { p, a: {}, u: {} }
  attrs.forEach((n) => (o.a[n] = gl.getAttribLocation(p, n)))
  unis.forEach((n) => (o.u[n] = gl.getUniformLocation(p, n)))
  return o
}

export function attr(
  gl: WebGLRenderingContext,
  prog: Program,
  name: string,
  buffer: WebGLBuffer | null,
  size: number,
) {
  gl.bindBuffer(gl.ARRAY_BUFFER, buffer)
  gl.enableVertexAttribArray(prog.a[name])
  gl.vertexAttribPointer(prog.a[name], size, gl.FLOAT, false, 0, 0)
}

export interface Pointer {
  x: number
  y: number
  tx: number
  ty: number
  spd: number
  down: boolean
  force: number
  radius: number
  seen: boolean
  tilt: [number, number]
}

export const newPointer = (): Pointer => ({
  x: 10,
  y: 10,
  tx: 10,
  ty: 10,
  spd: 0,
  down: false,
  force: 0.1,
  radius: 0.14,
  seen: false,
  tilt: [0, 0],
})

// Smooth the pointer, measure its speed, and derive the push on the swarm.
export function stepPointer(m: Pointer, dt: number, tiltX: number, tiltY: number) {
  const px = m.x
  const py = m.y
  m.x += (m.tx - m.x) * 0.22
  m.y += (m.ty - m.y) * 0.22
  const spd = Math.hypot(m.x - px, m.y - py) / Math.max(dt, 1e-3)
  m.spd += (spd - m.spd) * 0.15
  const wantR = m.down ? 0.6 : 0.14 + Math.min(0.24, m.spd * 0.09)
  const wantF = m.down ? -0.34 : 0.1 + Math.min(0.16, m.spd * 0.05)
  m.radius += (wantR - m.radius) * 0.12
  m.force += (wantF - m.force) * 0.12
  const tx = m.seen ? -m.y * tiltX : 0
  const ty = m.seen ? m.x * tiltY : 0
  m.tilt[0] += (tx - m.tilt[0]) * 0.04
  m.tilt[1] += (ty - m.tilt[1]) * 0.04
}

// Collects listeners so one call removes them all.
export function listeners() {
  const offs: (() => void)[] = []
  const on = <K extends keyof WindowEventMap>(
    type: K,
    fn: (e: WindowEventMap[K]) => void,
    opts?: AddEventListenerOptions,
  ) => {
    window.addEventListener(type, fn, opts)
    offs.push(() => window.removeEventListener(type, fn, opts))
  }
  return { on, off: () => offs.forEach((f) => f()) }
}
