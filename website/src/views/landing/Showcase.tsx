import { useState } from 'react'
import { Card } from '../../components/Card'
import { BlinkCursor } from '../../components/BlinkCursor'
import { SHOWCASE_SUBS, SHOWCASE_TABS } from '../../data/site'
import { FeatureGrid } from './FeatureGrid'
import { CHECK, ELLIPSIS, MIDDOT, NBSP } from '../../data/glyphs'

export function Showcase() {
  const [tab, setTab] = useState(0)
  return (
    <section className="showcase">
      <div className="showcase-head">
        <span className="showcase-title">Showcase</span>
        <span className="showcase-sub">{SHOWCASE_SUBS[tab]}</span>
      </div>
      <div className="showcase-tabs">
        {SHOWCASE_TABS.map((label, i) => (
          <button
            type="button"
            key={label}
            className={i === tab ? 'showcase-tab showcase-tab-active' : 'showcase-tab'}
            onClick={() => setTab(i)}
          >
            {label}
          </button>
        ))}
      </div>
      {tab === 0 && <EnforcePanels />}
      {tab === 1 && <ObservePanels />}
      {tab === 2 && <VerifyPanels />}
      <FeatureGrid />
    </section>
  )
}

function EnforcePanels() {
  return (
    <div className="showcase-grid">
      <Card title="gryph logs --live">
        <div>
          <span className="badge badge-block">BLOCK</span> exec rm -rf /
        </div>
        <div className="term-note">policy: no-destructive {MIDDOT} claude-code</div>
        <div>
          <span className="badge badge-warn">WARN{NBSP}</span> write .env
        </div>
        <div className="term-note">policy: sensitive-file {MIDDOT} cursor</div>
        <div>
          <span className="badge badge-allow">ALLOW</span> read src/auth/login.go
        </div>
        <div>
          <span className="badge badge-guide">GUIDE</span> exec git push --force
          <BlinkCursor />
        </div>
      </Card>
      <Card title="policy.yaml">
        <div>
          <span className="term-dim">rules:</span>
        </div>
        <div className="term-indent">
          <span className="term-dim">- match:</span> {'{ action: '}
          <span className="term-red">exec</span>,
        </div>
        <div className="term-indent-2">
          command: <span className="term-bold">"rm -rf*"</span>
          {' }'}
        </div>
        <div className="term-indent">
          effect: <span className="badge badge-block">block</span>
        </div>
        <div className="term-indent term-gap">
          <span className="term-dim">- match:</span> {'{ path: '}
          <span className="term-bold">"**/.env"</span>
          {' }'}
        </div>
        <div className="term-indent">
          effect: <span className="badge badge-warn">warn</span>
        </div>
      </Card>
    </div>
  )
}

function ObservePanels() {
  return (
    <div className="showcase-grid">
      <Card title="gryph logs --today">
        <div>
          <span className="term-dim">session</span> a91f {MIDDOT} claude-code
        </div>
        <div className="term-indent">
          <span className="badge badge-allow">READ</span>
          {NBSP} readme.txt
        </div>
        <div className="term-indent">
          <span className="badge badge-allow">WRITE</span> readme.txt
        </div>
        <div className="term-indent">
          <span className="badge badge-allow">EXEC{NBSP}</span> ls -la
        </div>
        <div className="term-gap">
          <span className="term-dim">session</span> b23c {MIDDOT} cursor
        </div>
        <div className="term-indent">
          <span className="badge badge-allow">READ</span>
          {NBSP} src/auth/login.go
          <BlinkCursor />
        </div>
      </Card>
      <Card title="gryph query">
        <div>
          <span className="term-dim">$</span> gryph query --action file_write --count
        </div>
        <div className="term-count">14</div>
        <div>
          <span className="term-dim">$</span> gryph sessions
        </div>
        <div className="term-indent">
          {`a91f${NBSP} claude-code${NBSP} `}
          <span className="term-dim">22 events</span>
        </div>
        <div className="term-indent">
          {`b23c${NBSP} cursor${NBSP.repeat(6)} `}
          <span className="term-dim">9 events</span>
        </div>
      </Card>
    </div>
  )
}

function VerifyPanels() {
  return (
    <div className="showcase-grid">
      <Card title="gryph receipts show">
        <div>
          <span className="term-dim">receipt</span> 0x4f2a{ELLIPSIS}
        </div>
        <div className="term-indent">action: exec rm -rf /</div>
        <div className="term-indent">
          {`effect:${NBSP} `}
          <span className="badge badge-block">block</span>
        </div>
        <div className="term-indent">{`policy:${NBSP} no-destructive`}</div>
        <div className="term-indent">{`prev:${NBSP.repeat(3)} 0x1a08${ELLIPSIS}`}</div>
        <div className="term-indent">{`sig:${NBSP.repeat(4)} ed25519 ${CHECK}`}</div>
      </Card>
      <Card title="gryph policy receipts --verify">
        <div>
          <span className="term-dim">$</span> gryph policy receipts --verify
        </div>
        <div className="term-indent">36{NBSP.repeat(2)}fa4f6dd3{NBSP.repeat(2)}pi-agent{NBSP.repeat(2)}edit{NBSP.repeat(2)}allow{NBSP.repeat(2)}success</div>
        <div className="term-indent">35{NBSP.repeat(2)}fa4f6dd3{NBSP.repeat(2)}pi-agent{NBSP.repeat(2)}write{NBSP.repeat(2)}allow{NBSP.repeat(2)}success</div>
        <div className="term-indent">34{NBSP.repeat(2)}fa4f6dd3{NBSP.repeat(2)}pi-agent{NBSP.repeat(2)}exec{NBSP.repeat(2)}block{NBSP.repeat(2)}success</div>
        <div className="term-gap">Chain verification: OK</div>
        <div className="term-gap">
          <span className="badge badge-warn">{`SIGNATURES OK ${CHECK}`}</span>
          <BlinkCursor />
        </div>
      </Card>
    </div>
  )
}
