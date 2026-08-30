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
      {tab === 0 && <ObservePanels />}
      {tab === 1 && <ControlPanels />}
      {tab === 2 && <VerifyPanels />}
      {tab === 3 && <IntegratePanels />}
      {tab === 4 && <AuthorPanels />}
      <FeatureGrid />
    </section>
  )
}

function AuthorPanels() {
  return (
    <div className="showcase-grid">
      <Card title="your agent drafts">
        <div>
          <span className="term-dim">&gt;</span> make gryph block force pushes
        </div>
        <div className="term-gap term-dim">drafting gryph-policy/force-push.yaml</div>
        <div>
          <span className="term-dim">$</span> gryph policy validate --file {ELLIPSIS}
        </div>
        <div className="term-indent">
          valid <span className="term-red">{CHECK}</span>
        </div>
        <div>
          <span className="term-dim">$</span> gryph policy test {ELLIPSIS} --force
        </div>
        <div className="term-indent">
          <span className="badge badge-block">BLOCK</span> no-force-push
        </div>
        <div className="term-gap">
          run: <span className="term-bold">gryph policy install</span>
        </div>
      </Card>
      <Card title="you install">
        <div>
          <span className="term-dim">$</span> gryph policy install force-push.yaml
        </div>
        <div className="term-indent">
          installed to policies <span className="term-red">{CHECK}</span>
        </div>
        <div className="term-gap">
          <span className="term-dim">$</span> gryph policy list
        </div>
        <div className="term-indent">
          {`builtin${NBSP.repeat(9)} `}
          <span className="term-dim">12 rules</span>
        </div>
        <div className="term-indent">
          {`force-push.yaml${NBSP} `}
          <span className="term-dim">1 rule</span>
        </div>
        <div className="term-gap term-dim">
          roll back: remove the file
          <BlinkCursor />
        </div>
      </Card>
    </div>
  )
}

function ControlPanels() {
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

function IntegratePanels() {
  return (
    <div className="showcase-grid">
      <Card title="gryph export --since 1w">
        <div>
          <span className="term-dim">$</span> gryph export --since 1w
        </div>
        <div className="term-indent">{'{"event":"file_read","agent":"claude-code"}'}</div>
        <div className="term-indent">{'{"event":"command_exec","agent":"cursor"}'}</div>
        <div className="term-indent">{'{"event":"file_write","agent":"pi-agent"}'}</div>
        <div className="term-gap">
          <span className="term-dim">one JSON object per line</span>
          <BlinkCursor />
        </div>
      </Card>
      <Card title="your pipeline">
        <div>
          <span className="term-dim">$</span> gryph export --since 1w | jq .event
        </div>
        <div className="term-indent">"file_read"</div>
        <div className="term-indent">"command_exec"</div>
        <div className="term-gap">
          <span className="term-dim">$</span> gryph export --since 1w | soc-ingest
        </div>
        <div className="term-indent">...elastic ...splunk ...dashboard</div>
        <div className="term-gap">
          <span className="term-dim">pipe to any ingestion or export tool</span>
          <BlinkCursor />
        </div>
      </Card>
    </div>
  )
}
