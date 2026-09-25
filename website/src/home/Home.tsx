import { useEffect, useRef } from 'react'
import { Copyright, Crumbs, StarsLink } from '../shared/Brand'
import { Cmd, InstallTabs } from '../shared/Cmd'
import { useVersion } from '../shared/github'
import { DISCORD_URL, DOCS_URL, REPO_URL } from '../shared/site'
import { startField } from './field'

const AGENTS = [
  { name: 'Claude Code', img: 'claude.webp' },
  { name: 'Codex', img: 'codex.png' },
  { name: 'Cursor', img: 'cursor.png' },
  { name: 'Windsurf', img: 'windsurf.svg' },
  { name: 'Gemini CLI', img: 'gemini.png', dark: true },
  { name: 'OpenCode', img: 'opencode.webp' },
  { name: 'Pi Agent', img: 'pi.png', dark: true },
  { name: 'Devin CLI', img: 'devin.jpg' },
  { name: 'Command Code', img: 'commandcode.png', dark: true },
]

const PROTECT = '/protect'

export function Home() {
  const field = useRef<HTMLCanvasElement>(null)
  const version = useVersion()

  useEffect(() => startField(field.current!), [])

  return (
    <>
      <canvas id="field" ref={field} aria-hidden="true" />

      <header className="top">
        <Crumbs brandHref="/" />
        <nav className="top-nav" aria-label="Project">
          <a className="link docs-link" href={DOCS_URL}>
            Docs
          </a>
          <StarsLink />
          <a className="btn fill sm" href={PROTECT}>
            Protect your agents
          </a>
        </nav>
      </header>

      <main id="top">
        <section className="scene hero" data-scene="1">
          <div className="stage">
            <div className="copy">
              <p className="abstract">
                <em>A security layer for AI coding agents.</em> It records every read, write and
                command, and checks your policy before any of them runs.
              </p>
              <div className="actions">
                <a className="btn fill" href={PROTECT}>
                  Protect your agents
                </a>
                <a className="btn ghost" href={REPO_URL}>
                  GitHub
                </a>
                <a className="btn ghost hero-docs" href={DOCS_URL}>
                  Docs
                </a>
              </div>
            </div>
          </div>
          <div className="hero-foot">
            <h1 className="title">
              <span>Gryph</span>
              <sup>
                OSS &middot; <b>{version}</b>
              </sup>
            </h1>
            <div className="scroll-cue" aria-hidden="true">
              <i />
            </div>
          </div>
        </section>

        <section className="scene" data-scene="2" id="overview">
          <div className="stage">
            <div className="copy">
              <h2>Everyone runs YOLO mode. Gryph keeps the receipts.</h2>
              <div className="agent-stack" aria-hidden="true">
                <img className="tile t1" src="/agents/claude.webp" alt="" loading="lazy" />
                <img className="tile t2" src="/agents/codex.png" alt="" loading="lazy" />
                <img className="tile t3" src="/agents/cursor.png" alt="" loading="lazy" />
              </div>
              <div className="log-peek" aria-hidden="true">
                <p className="sess">session a91f &middot; claude-code</p>
                <p>
                  <i>READ</i>readme.txt
                </p>
                <p>
                  <i>WRITE</i>readme.txt
                </p>
                <p>
                  <i>EXEC</i>ls -la
                </p>
                <p className="sess">session b23c &middot; cursor</p>
                <p>
                  <i>READ</i>src/auth/login.go
                </p>
              </div>
            </div>
          </div>
        </section>

        <section className="scene" data-scene="3" id="live">
          <div className="stage">
            <div className="copy wide">
              <h2>Actions. Enforcement. Live.</h2>
              <p>
                <code>gryph logs --live</code> streams every action as your agent works, allowed or
                blocked.
              </p>
              <div className="term" aria-label="Live gryph logs, monitoring a Claude Code session">
                <div className="term-bar">
                  <img className="term-icon" src="/agents/claude.webp" alt="" loading="lazy" />
                  <span className="term-title">Claude Code &mdash; gryph logs --live</span>
                  <span className="term-dots">
                    <i />
                    <i />
                    <i />
                  </span>
                </div>
                <div className="term-body">
                  <p className="term-line term-prompt">gryph logs --live</p>
                  <p className="term-line">
                    <b className="allow">ALLOW</b>
                    <span className="term-verb">write</span>readme.txt
                  </p>
                  <p className="term-line">
                    <b className="block">BLOCK</b>
                    <span className="term-verb">exec</span>rm -rf /
                  </p>
                  <p className="term-line term-policy">policy: no-destructive &middot; claude-code</p>
                  <p className="term-line">
                    <b className="allow">ALLOW</b>
                    <span className="term-verb">read</span>src/main.go
                  </p>
                  <p className="term-line term-cursor">
                    <i />
                  </p>
                </div>
              </div>
            </div>
          </div>
        </section>

        <section className="scene" data-scene="4" id="problem">
          <div className="stage">
            <div className="copy">
              <blockquote className="ask">
                A developer asks Claude Code to refactor a module. It runs 47 tool calls in 90
                seconds. Then the tests fail.
              </blockquote>
              <ul className="moments">
                <li>
                  <i />
                  <p>Which files did it read before making changes?</p>
                </li>
                <li>
                  <i />
                  <p>Did it run commands that were not expected?</p>
                </li>
                <li>
                  <i />
                  <p>Did it touch secrets, config or CI pipelines?</p>
                </li>
                <li>
                  <i />
                  <p>What did the file look like before and after?</p>
                </li>
              </ul>
              <p className="aside">
                Without Gryph, you're guessing. With Gryph, <code>gryph logs</code> shows
                everything.
              </p>
            </div>
          </div>
        </section>

        <section className="scene" data-scene="5" id="monitor">
          <div className="stage">
            <div className="copy">
              <h2 className="word">Pipe it anywhere.</h2>
              <p>JSON Lines exports flow into your dashboards and internal tools.</p>
              <Cmd text="gryph export --since 1w" />
              <div className="jsonl" aria-hidden="true">
                <p>{'{"event":"file_read","agent":"claude-code"}'}</p>
                <p>{'{"event":"command_exec","agent":"cursor"}'}</p>
                <p>{'{"event":"file_write","agent":"pi-agent"}'}</p>
              </div>
              <p className="aside">
                Lives in SQLite by default. Pipe it into your own tools, or OpenSearch for
                centralized review.
              </p>
            </div>
          </div>
        </section>

        <section className="scene" data-scene="6" id="policy">
          <div className="stage">
            <div className="copy">
              <h2 className="word tight">
                Block, Warn
                <br />
                Custom Policies
              </h2>
              <p>A YAML rule, checked before the action runs.</p>
              <div className="draft" aria-label="Example policy">
                <div className="draft-head">
                  <span>File</span>
                  <b>policies/no-force-push.yaml</b>
                  <span>Policy</span>
                  <b>enforcing</b>
                </div>
                <div className="draft-body">
                  <pre>
                    {`version: "1"
rules:
  - id: no-force-push
    `}
                    <mark>action: block</mark>
                    {`
    match:
      action_types: [command_exec]
      command_patterns: ['git push .*--force']
    message: Force push needs a human.

  - id: warn-session-write-volume
    action: warn
    condition: context.files_written >= 25`}
                  </pre>
                </div>
              </div>
              <p className="aside">
                A blocked action never reaches the agent's tool. Guidance goes back to it as plain
                text. Gryph records every decision.
              </p>
            </div>
          </div>
        </section>

        <section className="scene" data-scene="7" id="agents">
          <div className="stage">
            <div className="copy">
              <h2>Works with the agent you already run.</h2>
              <ul className="tiles">
                {AGENTS.map((a) => (
                  <li key={a.name} className={a.dark ? 'logo dark' : 'logo'}>
                    <img src={`/agents/${a.img}`} alt="" loading="lazy" />
                    <h3>{a.name}</h3>
                  </li>
                ))}
              </ul>
              <p className="aside">One command hooks every agent it finds. No per-agent setup.</p>
            </div>
          </div>
        </section>

        <section className="scene closing" data-scene="8" id="install">
          <div className="stage">
            <div className="copy">
              <h2>Protect your agents.</h2>
              <p className="soft">Install Gryph, then hook every agent it finds.</p>
              <InstallTabs />
              <Cmd text="gryph install && gryph status" />
              <div className="actions">
                <a className="btn fill" href={PROTECT}>
                  Protect your agents
                </a>
                <a className="btn" href={REPO_URL}>
                  Star on GitHub
                </a>
              </div>
            </div>
          </div>
        </section>
      </main>

      <footer className="scene foot" data-scene="9">
        <div className="stage">
          <Crumbs brandHref="#top" brandLabel="Back to top" />
          <nav aria-label="Footer">
            <a href="#overview">Overview</a>
            <a href="#live">How it works</a>
            <a href={DOCS_URL}>Docs</a>
            <a href={PROTECT}>Protect your agents</a>
            <a href={REPO_URL}>GitHub</a>
            <a href={DISCORD_URL}>Discord</a>
          </nav>
          <Copyright />
        </div>
      </footer>
    </>
  )
}
