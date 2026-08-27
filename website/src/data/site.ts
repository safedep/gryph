export const REPO_URL = 'https://github.com/safedep/gryph'
export const DOCS_URL = 'https://github.com/safedep/gryph/tree/main/docs'
export const DOCS_FILE_URL = 'https://github.com/safedep/gryph/blob/main/docs/'
export const DISCORD_URL = 'https://discord.gg/kAGEj25dCn'
export const FALLBACK_STARS = '1.2k'

export const AGENTS = [
  'Claude Code',
  'Cursor',
  'Gemini CLI',
  'OpenCode',
  'Windsurf',
  'Codex',
  'Pi Agent',
]

export const RAIL_TITLES = [
  'Orientation',
  'Install and hook',
  'Capture session',
  'Query and export',
  'Privacy and config',
  'Enforce policy',
  'Receipts',
  'Cost and stats',
  'Housekeeping',
]

export const PLANES = [
  {
    name: 'Observe',
    desc: 'Gryph records every read, write, and command in a local SQLite database.',
    entry: 'logs \u00b7 query \u00b7 sessions \u00b7 export \u00b7 stats',
  },
  {
    name: 'Enforce',
    desc: 'A YAML policy controls actions before they run.',
    entry: 'policy \u00b7 receipts \u00b7 keys',
  },
]

export const FEATURES = [
  {
    name: 'Observe',
    desc: 'Gryph records each read, write, and command in a local SQLite file.',
  },
  { name: 'Enforce', desc: 'YAML rules control actions before they run.' },
  {
    name: 'Verify',
    desc: 'Gryph signs a receipt for each session and links them in a chain.',
  },
  { name: 'Local', desc: 'No cloud. No telemetry.' },
]

export const SHOWCASE_TABS = ['Enforce', 'Observe', 'Verify']

export const SHOWCASE_SUBS = [
  'one policy, four decisions',
  'a full session, reconstructed',
  'signed receipts, hash-chained',
]

export const MILESTONES = [
  { name: 'Hooked', desc: 'agent registered' },
  { name: 'Recorded', desc: 'first session recorded' },
  { name: 'Queried', desc: 'audit trail rebuilt' },
  { name: 'Guarded', desc: 'rm -rf blocked in a test' },
  { name: 'Enforced', desc: 'block receipt recorded' },
  { name: 'Verified', desc: 'receipt chain correct' },
]

export interface ReceiptLine {
  text: string
  tone: 'normal' | 'accent' | 'ok'
}

export const RECEIPT_LINES: ReceiptLine[] = [
  { text: 'milestones ......... 6/6 \u2713', tone: 'normal' },
  { text: 'agents hooked ...... 1', tone: 'normal' },
  { text: 'events captured .... 128', tone: 'normal' },
  { text: 'policy ............. enforcing', tone: 'normal' },
  { text: 'blocks recorded .... 1', tone: 'accent' },
  { text: 'chain .............. verified \u2713', tone: 'ok' },
]
