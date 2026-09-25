import { useEffect, useRef, useState, useSyncExternalStore } from 'react'
import { Copyright, Crumbs, StarsLink } from '../shared/Brand'
import { Arrow } from '../shared/Arrow'
import { Cmd, InstallTabs } from '../shared/Cmd'
import { DISCORD_URL, DOCS_URL, REPO_URL, cx } from '../shared/site'
import { startField } from './field'
import { MILESTONES, type Task } from './milestones'

const TOTAL = MILESTONES.length
// Progress lives in this browser only.
const KEY = 'gryph-path'
const NONE = new Set<string>()

let progress: Set<string> | null = null
const listeners = new Set<() => void>()

function readProgress(): Set<string> {
  if (!progress) {
    try {
      progress = new Set(JSON.parse(localStorage.getItem(KEY) || '[]'))
    } catch {
      progress = new Set()
    }
  }
  return progress
}

function writeProgress(next: Set<string>) {
  progress = next
  try {
    localStorage.setItem(KEY, JSON.stringify([...next]))
  } catch {
    // Private mode or blocked storage. Progress stays in memory.
  }
  listeners.forEach((f) => f())
}

function subscribeProgress(f: () => void) {
  listeners.add(f)
  return () => listeners.delete(f)
}

function subscribeHash(f: () => void) {
  addEventListener('hashchange', f)
  return () => removeEventListener('hashchange', f)
}

// The server snapshots match the prerendered intro, so hydration is clean.
const useProgress = () => useSyncExternalStore(subscribeProgress, readProgress, () => NONE)
const useHash = () => useSyncExternalStore(subscribeHash, () => location.hash, () => '')

export function Protect() {
  const hash = useHash()
  const done = useProgress()
  const [view, step] = hash.slice(1).split('/')
  const inGuide = view === 'guide'
  const index = MILESTONES.findIndex((m) => m.id === step)
  const firstOpen = MILESTONES.find((m) => !done.has(m.id))?.id ?? 'done'
  const resume = done.size ? firstOpen : MILESTONES[0].id

  useEffect(() => {
    if (inGuide && step !== 'done' && index < 0) location.replace(`#guide/${resume}`)
  }, [inGuide, step, index, resume])

  useEffect(() => {
    if (inGuide) scrollTo(0, 0)
  }, [inGuide, step])

  const complete = (id: string) => writeProgress(new Set(done).add(id))
  const reset = () => {
    writeProgress(new Set())
    location.hash = ''
  }

  return (
    <>
      <header className="top">
        <Crumbs brandHref="/" />
        <nav aria-label="Project">
          <a href="/">Overview</a>
          <a href={DOCS_URL}>Docs</a>
          <StarsLink />
        </nav>
      </header>

      <main>
        {inGuide ? (
          <section className="guide" id="guide">
            <Rail current={step} done={done} />
            <article className="page" id="page" aria-live="polite">
              {step === 'done' ? (
                <Finish done={done} firstOpen={firstOpen} onReset={reset} />
              ) : index >= 0 ? (
                <MilestonePage key={step} index={index} done={done} onComplete={complete} />
              ) : null}
            </article>
          </section>
        ) : (
          <Intro
            href={`#guide/${resume}`}
            label={
              done.size && done.size < TOTAL ? 'Continue guided setup' : 'Start guided setup'
            }
          />
        )}
      </main>

      <footer className="foot">
        <Copyright />
        <a href={DISCORD_URL}>Ask a question on Discord</a>
      </footer>
    </>
  )
}

function Intro({ href, label }: { href: string; label: string }) {
  const canvas = useRef<HTMLCanvasElement>(null)
  useEffect(() => startField(canvas.current!), [])

  return (
    <section className="intro" id="intro" aria-label="Guided setup">
      <h1 className="vh">Protect your agents</h1>
      <div className="art">
        <canvas id="field" ref={canvas} aria-hidden="true" />
      </div>
      <div className="under">
        <a className="btn fill big" id="start" href={href}>
          {label}
        </a>
        <p id="start-note">{TOTAL} steps. Self-paced. Everything stays local.</p>
      </div>
    </section>
  )
}

function Rail({ current, done }: { current: string; done: Set<string> }) {
  return (
    <aside className="nav" aria-label="Milestones">
      <small>GUIDED SETUP</small>
      <div className="progress" id="bar">
        <span style={{ width: `${(done.size / TOTAL) * 100}%` }} />
      </div>
      <span className="count" id="count">
        {done.size} of {TOTAL} complete
      </span>
      <ol id="navlist">
        {MILESTONES.map((m, i) => {
          const on = m.id === current
          const ok = done.has(m.id)
          return (
            <li key={m.id}>
              <a
                href={`#guide/${m.id}`}
                className={cx(on && 'on', ok && 'done')}
                aria-current={on ? 'step' : undefined}
              >
                <span className="n">{ok ? '✓' : i + 1}</span>
                {m.title}
              </a>
            </li>
          )
        })}
      </ol>
    </aside>
  )
}

function TaskItem({ task }: { task: Task }) {
  return (
    <li>
      <p className="do">{task.text}</p>
      {task.install && <InstallTabs pre />}
      {task.cmd && <Cmd text={task.cmd} pre />}
      {task.ask && (
        <p className="ask">
          <small>ASK YOUR AGENT</small>
          {task.ask}
        </p>
      )}
      {task.see && <p className="see">{task.see}</p>}
    </li>
  )
}

interface MilestonePageProps {
  index: number
  done: Set<string>
  onComplete: (id: string) => void
}

function MilestonePage({ index, done, onComplete }: MilestonePageProps) {
  // miss.tries changes the key of the wrong answer, so its nudge animation plays again.
  const [miss, setMiss] = useState<{ pick: number; tries: number } | null>(null)
  const m = MILESTONES[index]
  const ok = done.has(m.id)
  const order = index % 2 ? [1, 0] : [0, 1]
  const prev = MILESTONES[index - 1]
  const next = MILESTONES[index + 1]

  const choose = (j: number) => {
    if (j === 0) onComplete(m.id)
    else setMiss((s) => ({ pick: j, tries: (s?.tries ?? 0) + 1 }))
  }

  return (
    <>
      <h1>{m.title}</h1>
      <p className="lede">{m.lede}</p>
      <ol className="tasks">
        {m.tasks.map((t) => (
          <TaskItem key={t.text} task={t} />
        ))}
      </ol>
      <div className="check">
        <small>Checkpoint</small>
        <p className="q">{m.question}</p>
        <div className="opts">
          {order.map((j) => (
            <button
              key={miss?.pick === j ? `${j}-${miss.tries}` : j}
              type="button"
              className={cx('opt', ok && j === 0 && 'yes', !ok && miss?.pick === j && 'no')}
              disabled={ok}
              onClick={() => choose(j)}
            >
              {m.answers[j]}
            </button>
          ))}
        </div>
        <p className={cx('why', (ok || !!miss) && 'on')}>
          {ok ? (
            <>
              <b className="ok">&#10003; Correct.</b> {m.why}
            </>
          ) : miss ? (
            <>
              <b className="bad">Incorrect.</b> Read the steps again, then try again.
            </>
          ) : null}
        </p>
      </div>
      <nav className="pager" aria-label="Milestones">
        {prev && (
          <a className="link" href={`#guide/${prev.id}`}>
            <Arrow back />
            Previous
          </a>
        )}
        <a className={cx('btn', ok && 'fill')} id="next" href={`#guide/${next?.id ?? 'done'}`}>
          {next ? `Next: ${next.title}` : 'Finish'}
          <Arrow />
        </a>
      </nav>
    </>
  )
}

interface FinishProps {
  done: Set<string>
  firstOpen: string
  onReset: () => void
}

function Finish({ done, firstOpen, onReset }: FinishProps) {
  if (done.size < TOTAL) {
    return (
      <div className="finish">
        <p className="kicker">Not complete</p>
        <h1>{TOTAL - done.size} steps are not complete.</h1>
        <p className="lede">To complete a step, answer its checkpoint question.</p>
        <nav className="pager">
          <a className="btn fill" href={`#guide/${firstOpen}`}>
            Continue
            <Arrow />
          </a>
        </nav>
      </div>
    )
  }

  return (
    <div className="finish">
      <p className="kicker">You completed all {TOTAL} steps</p>
      <h1>Gryph now protects your agents.</h1>
      <div className="receipt">
        milestones ......... <b>{`${TOTAL}/${TOTAL} ✓`}</b>
        {'\n'}agents hooked ...... <b>1</b>
        {'\n'}policy ............. <b>enforcing</b>
        {'\n'}blocks recorded .... <b>1</b>
        {'\n'}chain .............. <b>verified &#10003;</b>
      </div>
      <div className="actions">
        <a className="btn fill" href={REPO_URL}>
          Star Gryph on GitHub
        </a>
        <a className="btn" href="/">
          Go to the overview
        </a>
        <button type="button" className="link" id="reset" onClick={onReset}>
          Start again
        </button>
      </div>
    </div>
  )
}
