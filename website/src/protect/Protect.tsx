import { useEffect, useState, useSyncExternalStore } from 'react'
import { Footer, StarsLink, Topbar } from '../shared/Brand'
import { Blob } from '../shared/Blob'
import { Arrow, Check, Cross } from '../shared/Icons'
import { Cmd, InstallTabs } from '../shared/Cmd'
import { DISCORD_URL, DOCS_URL, REPO_URL, cx } from '../shared/site'
import { Window } from '../shared/Window'
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
      <Topbar
        brandHref="/"
        wide={inGuide}
        nav={
          <nav className="topnav" aria-label="Project">
            <a href="/">Overview</a>
            <a href={DOCS_URL}>Docs</a>
          </nav>
        }
      >
        {inGuide && <Xp done={done.size} />}
        <StarsLink />
      </Topbar>

      <main id="top">
        {inGuide && step === 'done' ? (
          <Finish done={done} firstOpen={firstOpen} onReset={reset} />
        ) : inGuide ? (
          <section className="guide" id="guide">
            <Rail current={step} done={done} />
            <article className="page" id="page" aria-live="polite">
              {index >= 0 && (
                <MilestonePage key={step} index={index} done={done} onComplete={complete} />
              )}
            </article>
          </section>
        ) : (
          <Intro
            href={`#guide/${resume}`}
            done={done}
            label={
              done.size && done.size < TOTAL ? 'Continue guided setup' : 'Start guided setup'
            }
          />
        )}
      </main>

      <Footer>
        <a href="/">Overview</a>
        <a href={DOCS_URL}>Docs</a>
        <a href={REPO_URL}>GitHub</a>
        <a href={DISCORD_URL}>Ask a question on Discord</a>
      </Footer>
    </>
  )
}

function Xp({ done }: { done: number }) {
  return (
    <div className="xp" role="img" aria-label={`${done} of ${TOTAL} complete`}>
      <span className="pixel xp-label">XP</span>
      <span className="xp-bar">
        {MILESTONES.map((m, i) => (
          <i key={m.id} className={cx(i < done && 'on')} />
        ))}
      </span>
      <span className="xp-count">
        {done} / {TOTAL}
      </span>
    </div>
  )
}

function Intro({ href, label, done }: { href: string; label: string; done: Set<string> }) {
  return (
    <section className="intro dots" id="intro" aria-labelledby="intro-title">
      <div className="intro-card">
        <Window title={'gryph \u00b7 guided setup'} dark>
          <ol className="intro-list">
            {MILESTONES.map((m, i) => (
              <li key={m.id} className={cx(done.has(m.id) && 'done')}>
                <span className="t-mute">{String(i + 1).padStart(2, '0')}</span> {m.title}
                {done.has(m.id) && <Check />}
              </li>
            ))}
          </ol>
        </Window>
        <div className="intro-under">
          <p className="kicker">Guided setup</p>
          <h1 id="intro-title">Protect your agents</h1>
          <p id="start-note">{TOTAL} steps. Self-paced. Everything stays local.</p>
          <a className="btn btn-coin" id="start" href={href}>
            {label}
            <Arrow />
          </a>
        </div>
      </div>
    </section>
  )
}

function Rail({ current, done }: { current: string; done: Set<string> }) {
  return (
    <aside className="rail" aria-label="Milestones">
      <p className="kicker">Guided setup</p>
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
                <span className="n">{ok ? <Check /> : i + 1}</span>
                {m.title}
              </a>
            </li>
          )
        })}
      </ol>
    </aside>
  )
}

function TaskItem({ task, n }: { task: Task; n: number }) {
  return (
    <li>
      <span className="step-n pixel" aria-hidden="true">
        {n}
      </span>
      <div className="step-body">
        <p className="do">{task.text}</p>
        {task.install && <InstallTabs pre />}
        {task.cmd && <Cmd text={task.cmd} pre />}
        {task.ask && (
          <div className="ask">
            <span className="kicker">Ask your agent</span>
            <p>{task.ask}</p>
          </div>
        )}
        {task.see && <p className="see">{task.see}</p>}
      </div>
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
      <p className="kicker">
        Step {index + 1} of {TOTAL}
      </p>
      <h1>{m.title}</h1>
      <div className="goal">
        <span className="badge">GOAL</span>
        <p className="lede">{m.lede}</p>
      </div>
      <ol className="tasks">
        {m.tasks.map((t, i) => (
          <TaskItem key={t.text} task={t} n={i + 1} />
        ))}
      </ol>
      <div className="card checkpoint">
        <div className="check-head">
          <span className="badge pixel">CHECKPOINT</span>
          <p className="q">{m.question}</p>
        </div>
        <div className="opts">
          {order.map((j) => (
            <button
              key={miss?.pick === j ? `${j}-${miss.tries}` : j}
              type="button"
              className={cx('opt', ok && j === 0 && 'yes', !ok && miss?.pick === j && 'no')}
              disabled={ok}
              onClick={() => choose(j)}
            >
              <span>{m.answers[j]}</span>
              {ok && j === 0 && <Check />}
              {!ok && miss?.pick === j && <Cross />}
            </button>
          ))}
        </div>
        <div className={cx('why', ok ? 'tone-mint' : miss ? 'tone-cherry' : undefined, (ok || !!miss) && 'on')}>
          {ok ? (
            <>
              <b className="ok">Correct.</b> {m.why}
            </>
          ) : miss ? (
            <>
              <b className="bad">Incorrect.</b> Read the steps again, then try again.
            </>
          ) : null}
        </div>
      </div>
      <nav className="pager" aria-label="Milestones">
        {prev && (
          <a className="btn" href={`#guide/${prev.id}`}>
            <Arrow back />
            Previous
          </a>
        )}
        <a className="btn btn-coin btn-wide" id="next" href={`#guide/${next?.id ?? 'done'}`}>
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
      <section className="intro dots" aria-labelledby="finish-title">
        <div className="intro-card">
          <div className="finish-body">
            <p className="kicker">Not complete</p>
            <h1 id="finish-title">{TOTAL - done.size} steps are not complete.</h1>
            <p className="sub">To complete a step, answer its checkpoint question.</p>
            <a className="btn btn-coin btn-wide" href={`#guide/${firstOpen}`}>
              Continue
              <Arrow />
            </a>
          </div>
        </div>
      </section>
    )
  }

  return (
    <section className="intro dots" aria-labelledby="finish-title">
      <div className="intro-card">
        <Window title={'gryph \u00b7 your setup'} dark>
          <p>
            milestones ......... <b className="t-read">{`${TOTAL}/${TOTAL}`} <Check /></b>
          </p>
          <p>
            agents hooked ...... <b>1</b>
          </p>
          <p>
            policy ............. <b className="t-exec">turned on</b>
          </p>
          <p>
            block .............. <b className="t-block">tested</b>
          </p>
          <p>
            chain .............. <b className="t-read">checked <Check /></b>
          </p>
        </Window>
        <div className="finish-body">
          <Blob seed="gryph" expression="happy" size={72} title="Gryph mascot" />
          <p className="pixel finish-tag">You completed all {TOTAL} steps</p>
          <h1 id="finish-title">You set up Gryph for your agents.</h1>
          <ol className="milestones">
            {MILESTONES.map((m) => (
              <li key={m.id}>
                <span className="n">
                  <Check />
                </span>
                {m.title}
              </li>
            ))}
          </ol>
          <a className="btn btn-coin btn-wide" href={REPO_URL}>
            Star Gryph on GitHub
          </a>
          <div className="finish-more">
            <a className="btn btn-md" href="/">
              Go to the overview
            </a>
            <button type="button" className="btn btn-md btn-plain" id="reset" onClick={onReset}>
              Start again
            </button>
          </div>
        </div>
      </div>
    </section>
  )
}
