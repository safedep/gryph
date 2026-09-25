export const REPO_URL = 'https://github.com/safedep/gryph'
export const DOCS_URL = 'https://github.com/safedep/gryph/tree/main/docs'
export const DISCORD_URL = 'https://discord.gg/kAGEj25dCn'
export const SAFEDEP_URL = 'https://safedep.io'

export const INSTALL_METHODS: [label: string, cmd: string][] = [
  ['curl', 'curl -fsSL https://raw.githubusercontent.com/safedep/gryph/main/install.sh | sh'],
  ['brew', 'brew install safedep/tap/gryph'],
  ['npm', 'npm install -g @safedep/gryph'],
  ['go', 'go install github.com/safedep/gryph/cmd/gryph@latest'],
]

export const cx = (...names: (string | false | undefined)[]) =>
  names.filter(Boolean).join(' ') || undefined
