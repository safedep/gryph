import { Footer, StarsLink, Topbar } from '../shared/Brand'
import { Arrow, GitHubMark } from '../shared/Icons'
import { Blob } from '../shared/Blob'
import { Cmd, InstallTabs } from '../shared/Cmd'
import { useVersion } from '../shared/github'
import { DISCORD_URL, DOCS_URL, REPO_URL } from '../shared/site'
import { Cursor, Window } from '../shared/Window'
import { Intent } from './Intent'

const AGENTS = [
  { name: 'Claude Code', seed: 'claude' },
  { name: 'Codex', seed: 'codex' },
  { name: 'Cursor', seed: 'cursor' },
  { name: 'Gemini CLI', seed: 'gemini' },
  { name: 'Windsurf', seed: 'windsurf' },
  { name: 'OpenCode', seed: 'opencode' },
  { name: 'Pi Agent', seed: 'pi' },
  { name: 'Devin', seed: 'devin' },
  { name: 'Command Code', seed: 'commandcode' },
]

const SEES = ['Files', 'Commands', 'Web requests', 'Sessions', 'Cost (Claude Code)']
const DECISIONS = ['Block', 'Warn', 'Guide', 'Ask me first']

const TRUST = [
  {
    tone: 'tone-mint',
    kicker: 'Local',
    title: 'Your data stays home',
    text: 'Events live in a SQLite file on your machine.',
  },
  {
    tone: 'tone-cherry',
    kicker: 'Secrets',
    title: 'Secrets stay secret',
    text: 'Reads of .env, SSH and cloud keys are flagged. Contents are never saved.',
  },
  {
    tone: 'tone-grape',
    kicker: 'Self-protection',
    title: 'Guards its own setup',
    text: 'With rules on, Gryph blocks agent tools from editing its files and hooks.',
  },
]

const PROTECT = '/protect'

export function Home() {
  const version = useVersion()

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
        <header className="hero dots section">
          <div className="wrap hero-in">
            <span className="pill">
              <i />
              Gryph OSS &middot; <span className="ver">{version}</span>
            </span>
            <h1>
              A security layer for <span className="hl-box">AI coding agents.</span>
            </h1>
            <p className="lead">
              <span>See every file, command and web request your agent makes.</span>{' '}
              <span>Block the risky ones before they run.</span>
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
              <a className="btn" href={DOCS_URL}>
                Docs
              </a>
            </div>
            <p className="hero-note">Local. Open source. No account.</p>
          </div>
        </header>

        <section className="section section-sand" id="agents">
          <div className="wrap">
            <p className="kicker">Agents</p>
            <h2 className="title-lg">
              One install. <span className="hl-grape">Every agent.</span>
            </h2>
            <p className="lead">
              <code>gryph install</code> finds the agents on your machine and hooks each one.
            </p>
            <ul className="agents">
              {AGENTS.map((a) => (
                <li key={a.name} className="agent">
                  <Blob seed={a.seed} size={28} />
                  {a.name}
                </li>
              ))}
            </ul>
            <div className="dry-run">
              <Cmd text="gryph install --dry-run" />
            </div>
            <p className="aside">Preview first. Gryph backs up every config it changes.</p>
          </div>
        </section>

        <section className="section section-cream" id="see">
          <div className="wrap split">
            <div>
              <p className="kicker">See</p>
              <h2 className="title-lg">
                See what it did, <span className="hl-grape">not what it said.</span>
              </h2>
              <p className="lead">
                Every read, write, command and web request, with sessions and line counts. Search
                it later.
              </p>
              <Chips items={SEES} />
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
                <b className="verdict verdict-block">BLOCK</b> <span className="t-mute">read </span>{' '}
                <span className="t-mute">[sensitive]</span>
              </p>
              <p>
                <b className="verdict verdict-block">BLOCK</b> <span className="t-mute">exec </span> git push origin
                main --force
              </p>
              <p>
                <b className="verdict verdict-allow">ALLOW</b> <span className="t-mute">read </span> src/checkout.ts
                <Cursor />
              </p>
            </Window>
          </div>
        </section>

        <section className="section" id="rules">
          <div className="wrap split">
            <div>
              <p className="kicker">Rules</p>
              <h2 className="title-lg">
                Stop the risky move <span className="hl-grape">before it runs.</span>
              </h2>
              <p className="lead">Write a short rule. Gryph blocks the action and tells your agent why.</p>
              <Chips items={DECISIONS} />
              <p className="aside">
                Rules are off by default. Turn them on with{' '}
                <code>gryph config set policy.enabled true</code>.
              </p>
            </div>
            <div className="stack">
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
              <Window title="Claude Code" dark label="The agent sees why Gryph blocked it">
                <p>
                  <span className="t-prompt">&gt;</span> <span className="t-cmd">git push origin main --force</span>
                </p>
                <p>
                  <b className="verdict verdict-block">BLOCK</b> Force push needs a human.
                </p>
                <p>
                  <span className="t-prompt">&gt;</span> <span className="t-cmd">git push origin fix-cart</span>
                  <Cursor />
                </p>
              </Window>
            </div>
          </div>
        </section>

        <Intent />

        <section className="section section-cream" id="trust">
          <div className="wrap">
            <p className="kicker">Trust</p>
            <h2 className="title-lg">
              Runs on your machine. <span className="hl-grape">Stays there.</span>
            </h2>
            <p className="lead">No account, no cloud and no telemetry. You can remove it with one command.</p>
            <ul className="trust">
              {TRUST.map((t) => (
                <li key={t.kicker} className={`card card-lift ${t.tone}`}>
                  <span className="kicker">{t.kicker}</span>
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
                <a className="btn btn-coin btn-on-ink" href={PROTECT}>
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
        </section>
      </main>

      <Footer
        note={
          <>
            Running a team? Gryph supports <a href={DOCS_URL}>managed installs</a> and compliance
            reports.
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

function Chips({ items }: { items: string[] }) {
  return (
    <ul className="chips">
      {items.map((c) => (
        <li key={c}>{c}</li>
      ))}
    </ul>
  )
}
