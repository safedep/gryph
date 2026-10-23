import { useEffect, useRef, useState } from 'react'
import { Footer, StarsLink, Topbar } from '../shared/Brand'
import { Arrow, GitHubMark } from '../shared/Icons'
import { Cmd, InstallTabs } from '../shared/Cmd'
import { Glyph, Icon, Scene } from '../shared/Glyph'
import { DISCORD_URL, DOCS_URL, REPO_URL } from '../shared/site'
import { Cursor, Window } from '../shared/Window'
import { Intent } from './Intent'

const AGENTS = [
  { name: 'Claude Code', file: 'claude.webp' },
  { name: 'Codex', file: 'codex.png' },
  { name: 'Cursor', file: 'cursor.png' },
  { name: 'Gemini CLI', file: 'gemini.png' },
  { name: 'Windsurf', file: 'windsurf.svg' },
  { name: 'OpenCode', file: 'opencode.webp' },
  { name: 'Pi Agent', file: 'pi.png', dark: true },
  { name: 'Devin', file: 'devin.jpg' },
  { name: 'Command Code', file: 'commandcode.png' },
]

const SEES = [
  { scene: 'files', title: 'Files', text: 'Every read and write, with line counts and the session it came from.' },
  { scene: 'commands', title: 'Commands', text: 'Shell commands, before they run. A short rule stops the risky ones.' },
  { scene: 'web', title: 'Web requests', text: 'Every host your agent talks to. Secrets never leave your machine.' },
] as const

const TRUST = [
  { icon: 'home', tone: 'tone-mint', kicker: 'Local', title: 'Your data stays home', text: 'Events live in a SQLite file on your machine.' },
  { icon: 'lock', tone: 'tone-cherry', kicker: 'Secrets', title: 'Secrets stay secret', text: 'Reads of .env, SSH and cloud keys are flagged. Contents are never saved.' },
  { icon: 'shield', tone: 'tone-grape', kicker: 'Self-protection', title: 'Guards its own setup', text: 'With rules on, Gryph blocks agent tools from editing its files and hooks.' },
] as const

// What the agent sees for each decision a rule can make. The keys in the Rules section pick one.
const DECISIONS = [
  { name: 'block', glyph: '✕', tone: 'cherry', verdict: 'verdict-block', label: 'BLOCK', cmd: 'git push origin main --force', msg: 'Force push needs a human.', next: 'git push origin fix-cart' },
  { name: 'warn', glyph: '!', tone: 'coin', verdict: 'verdict-warn', label: 'WARN', cmd: 'rm -rf node_modules', msg: 'Deletes many files. Allowed and logged.', next: 'npm install' },
  { name: 'guide', glyph: '→', tone: 'mint', verdict: 'verdict-allow', label: 'GUIDE', cmd: 'npm install left-pad', msg: 'This repo uses pnpm. Run pnpm add left-pad.', next: 'pnpm add left-pad' },
  { name: 'ask me first', glyph: '?', tone: 'grape', verdict: 'verdict-guide', label: 'ASK', cmd: 'npm publish', msg: 'Publishing needs your OK.', next: null },
]

const PROTECT = '/protect'

export function Home() {
  const [pick, setPick] = useState(0)
  const decision = DECISIONS[pick]
  const [track, step, setStep] = useStage()

  return (
    <>
      <Topbar
        brandHref="/"
        nav={
          <nav className="topnav" aria-label="Project">
            <a href="#agents">Agents</a>
            <a href="#rules">Rules</a>
            <a href={DOCS_URL}>Docs</a>
          </nav>
        }
      >
        <StarsLink />
        <a className="btn btn-sm btn-coin" href={PROTECT}>
          Protect your agents
        </a>
      </Topbar>

      <main id="top">
        <header className="hero section">
          <div className="wrap hero-in">
            <div className="hero-copy">
            <h1>
              A security layer for <span className="hero-soft">AI coding agents.</span>
            </h1>
            <p className="lead">
              See every file, command and web request your agent makes. Block the risky ones before they run.
            </p>
            <div className="actions">
              <a className="btn btn-coin" href={PROTECT}>
                Protect your agents
                <Arrow />
              </a>
              <a className="btn" href={REPO_URL}>
                <GitHubMark />
                GitHub
              </a>
            </div>
            <p className="hero-note">
              Local &middot; Open source &middot; No account
            </p>
            </div>

          </div>
          <Glyph kind="plume" cols={64} rows={50} seed={3} className="hero-plume" />
          <Glyph kind="shade" cols={240} rows={16} className="hero-art" />
        </header>

        <section className="section has-dust section-grape" id="agents">
          <Glyph kind="dust" cols={90} rows={18} seed={21} className="section-dust" />
          <div className="wrap">
            <h2 className="title-lg">
              One install. <span className="hl-grape">Every agent.</span>
            </h2>
            <p className="lead">
              <code>gryph install</code> finds the agents on your machine and hooks each one.
            </p>
            <ul className="logos" aria-label="Supported agents">
              {AGENTS.map((a, i) => (
                <li key={a.name} className={'dark' in a ? 'card-dark' : undefined} style={{ '--i': i - (AGENTS.length - 1) / 2 } as React.CSSProperties}>
                  <img src={`/agents/${a.file}`} alt={a.name} title={a.name} width="44" height="44" loading="lazy" />
                </li>
              ))}
            </ul>
            <div className="dry-run">
              <Cmd text="gryph install --dry-run" />
            </div>
            <p className="aside">Preview first. Gryph backs up every config it changes.</p>
          </div>
        </section>

        <section className="section" id="see">
          <div className="wrap">
            <div className="split">
              <div>
                <h2 className="title-lg">
                  See what it did.
                </h2>
                <p className="lead">
                  Every read, write, command and web request, with sessions and line counts. Search it later.
                </p>
              </div>
              <Window title="gryph logs --live" dark status="live" label="Gryph live log of a Claude Code session">
                <p>
                  <span className="t-prompt">$</span> <span className="t-cmd">gryph logs --live</span>
                </p>
                <p>
                  <b className="verdict verdict-allow">ALLOW</b> <span className="t-mute">read </span> src/cart.ts
                </p>
                <p>
                  <b className="verdict verdict-allow">ALLOW</b> <span className="t-mute">write</span> src/cart.ts{' '}
                  <span className="t-read">+12</span> <span className="t-block">-3</span>
                </p>
                <p>
                  <b className="verdict verdict-allow">ALLOW</b> <span className="t-mute">exec </span> npm test
                </p>
                <p>
                  <b className="verdict verdict-guide">ASK  </b> <span className="t-mute">exec </span> npm publish
                </p>
                <p>
                  <b className="verdict verdict-block">BLOCK</b> <span className="t-mute">read </span> ~/.aws/credentials
                </p>
                <p>
                  <b className="verdict verdict-block">BLOCK</b> <span className="t-mute">exec </span> git push origin main
                  --force
                </p>
                <p>
                  <b className="verdict verdict-allow">ALLOW</b> <span className="t-mute">read </span> src/checkout.ts
                  <Cursor />
                </p>
              </Window>
            </div>
            <ul className="features">
              {SEES.map((f) => (
                <li key={f.title} className="feature">
                  <Scene name={f.scene} />
                  <h3>{f.title}</h3>
                  <p>{f.text}</p>
                </li>
              ))}
            </ul>
          </div>
        </section>

        <section className="section has-dust section-mint" id="rules">
          <Glyph kind="dust" cols={90} rows={18} seed={22} className="section-dust" />
          <div className="wrap">
            <div className="rules-track" ref={track}>
            <div className="split rules-pin">
              <div>
                <h2 className="title-lg">
                  Stop the risky move <span className="hl-mint">before it runs.</span>
                </h2>
                <p className="lead">Write a short rule. Gryph blocks the action and tells your agent why.</p>
                <div className="keys" role="group" aria-label="Decisions a rule can make">
                  {DECISIONS.map((d, i) => (
                    <button
                      key={d.name}
                      type="button"
                      className={`key key-${d.tone}`}
                      aria-pressed={i === pick}
                      onClick={() => {
                        setPick(i)
                        setStep(1)
                      }}
                    >
                      <span aria-hidden="true">{d.glyph}</span>
                      {d.name}
                    </button>
                  ))}
                </div>
                <p className="aside">
                  Rules are off by default. Turn them on with <code>gryph config set policy.enabled true</code>.
                </p>
              </div>
              <div className="stage" data-step={step}>
                <Window title="policy.yaml" status="enforcing" label="Example policy">
                  <pre>
                    {`version: "1"
rules:
  - id: no-force-push
    action: `}
                    <span className="tag tag-block">block</span>
                    {`
    match:
      action_types: [command_exec]
      command_patterns: ['git push.* (-f|--force)']
    message: Force push needs a human.

  - id: ask-before-publish
    action: `}
                    <span className="tag tag-guide">escalate</span>
                    {`
    match:
      action_types: [command_exec]
      command_patterns: ['npm publish.*']
    message: Publishing needs my OK.`}
                  </pre>
                </Window>
                <Window title="Claude Code" dark label={`What the agent sees when a rule says ${decision.name}`}>
                  <div aria-live="polite" key={decision.name} className="agent-view">
                    <p>
                      <span className="t-prompt">&gt;</span> <span className="t-cmd">{decision.cmd}</span>
                    </p>
                    <p>
                      <b className={`verdict ${decision.verdict}`}>{decision.label}</b> {decision.msg}
                    </p>
                    <p>
                      {decision.next ? (
                        <>
                          <span className="t-prompt">&gt;</span> <span className="t-cmd">{decision.next}</span>
                        </>
                      ) : (
                        <span className="t-mute">waiting for you</span>
                      )}
                      <Cursor />
                    </p>
                  </div>
                </Window>
                <ol className="stage-steps" aria-hidden="true">
                  <li className={step === 0 ? 'on' : undefined}>01 you write a rule</li>
                  <li className={step === 1 ? 'on' : undefined}>02 the agent is told why</li>
                </ol>
              </div>
            </div>
            </div>
          </div>
        </section>

        <Intent />

        <section className="section has-dust section-cherry" id="trust">
          <Glyph kind="dust" cols={90} rows={18} seed={23} className="section-dust" />
          <div className="wrap">
            <h2 className="title-lg">
              Runs on your machine. <span className="hl-cherry">Stays there.</span>
            </h2>
            <p className="lead">No account, no cloud and no telemetry. You can remove it with one command.</p>
            <ul className="trust">
              {TRUST.map((t) => (
                <li key={t.kicker} className={`card card-lift ${t.tone}`}>
                  <Icon name={t.icon} />
                  <h3>{t.title}</h3>
                  <p>{t.text}</p>
                </li>
              ))}
            </ul>
            <div className="uninstall">
              <Cmd text="gryph uninstall --purge --restore-backup" />
            </div>
          </div>
        </section>

        <section className="section section-ink" id="install">
          <div className="wrap closing">
            <div>
              <h2 className="title-lg">
                Keep YOLO mode. <span className="hl-coin">Keep control.</span>
              </h2>
              <p className="lead">Install Gryph, then run one command to hook every agent.</p>
              <div className="actions">
                <a className="btn btn-coin" href={PROTECT}>
                  Protect your agents
                  <Arrow />
                </a>
                <a className="btn btn-on-ink" href={REPO_URL}>
                  <GitHubMark />
                  Star on GitHub
                </a>
              </div>
            </div>
            <div className="install">
              <InstallTabs />
              <Cmd text="gryph install" />
              <p className="install-note">Apache 2.0. Linux, macOS and Windows.</p>
            </div>
          </div>
          <Glyph kind="shade" cols={240} rows={10} seed={9} className="closing-art" />
        </section>
      </main>

      <Footer
        note={
          <>
            Running a team? Gryph supports <a href={DOCS_URL}>managed installs</a> and compliance reports.
          </>
        }
      >
        <a href="#agents">Agents</a>
        <a href="#rules">Rules</a>
        <a href={DOCS_URL}>Docs</a>
        <a href={REPO_URL}>GitHub</a>
        <a href={DISCORD_URL}>Discord</a>
        <a href={PROTECT}>Protect your agents</a>
      </Footer>
    </>
  )
}

// The Rules section pins its stage while you scroll through a taller track. The first half of the track
// shows the policy, the second half shows what the agent sees. A key press jumps to the agent view.
function useStage() {
  const track = useRef<HTMLDivElement>(null)
  const [step, setStep] = useState(0)

  useEffect(() => {
    let frame = 0
    const update = () => {
      frame = 0
      const el = track.current
      if (!el) return
      const r = el.getBoundingClientRect()
      const span = r.height - innerHeight
      if (span <= 0) return
      setStep(-r.top / span > 0.45 ? 1 : 0)
    }
    const onScroll = () => {
      if (!frame) frame = requestAnimationFrame(update)
    }
    addEventListener('scroll', onScroll, { passive: true })
    return () => {
      removeEventListener('scroll', onScroll)
      cancelAnimationFrame(frame)
    }
  }, [])

  return [track, step, setStep] as const
}
