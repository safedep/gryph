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
  'Control policy',
  'Agent authoring',
  'Receipts',
  'Cost and stats',
  'Housekeeping',
]

export const PLANES = [
  {
    name: 'Observe',
    desc: 'Gryph records every read, write, and command in a local database.',
    entry: 'logs \u00b7 query \u00b7 sessions \u00b7 export \u00b7 stats',
  },
  {
    name: 'Control',
    desc: 'A YAML policy controls actions before they run.',
    entry: 'policy \u00b7 receipts \u00b7 keys',
  },
]

export const FEATURES = [
  {
    name: 'Observe',
    desc: 'Gryph records each read, write, and command in a local database.',
  },
  { name: 'Control', desc: 'YAML rules and CEL expressions control actions before they run.' },
  {
    name: 'Verify',
    desc: 'Gryph signs a receipt for each session and links them in a chain.',
  },
  { name: 'Integrate', desc: 'JSON Lines exports pipe into your dashboards and internal tools.' },
]

export const SHOWCASE_TABS = ['Observe', 'Control', 'Verify', 'Integrate', 'Author']

export const SHOWCASE_SUBS = [
  'a full session, reconstructed',
  'one policy, four decisions',
  'signed receipts, hash-chained',
  'json lines to anything',
  'your agent drafts, you install',
]

export const MILESTONES = [
  { name: 'Hooked', desc: 'agent registered' },
  { name: 'Recorded', desc: 'first session recorded' },
  { name: 'Queried', desc: 'audit trail rebuilt' },
  { name: 'Guarded', desc: 'rm -rf blocked in a test' },
  { name: 'Enforced', desc: 'block receipt recorded' },
  { name: 'Authored', desc: 'agent drafted, you installed' },
  { name: 'Verified', desc: 'receipt chain correct' },
]

export interface ReceiptLine {
  text: string
  tone: 'normal' | 'accent' | 'ok'
}

export const RECEIPT_LINES: ReceiptLine[] = [
  { text: 'milestones ......... 7/7 \u2713', tone: 'normal' },
  { text: 'agents hooked ...... 1', tone: 'normal' },
  { text: 'events captured .... 128', tone: 'normal' },
  { text: 'policy ............. enforcing', tone: 'normal' },
  { text: 'blocks recorded .... 1', tone: 'accent' },
  { text: 'chain .............. verified \u2713', tone: 'ok' },
]
