import './components.css'

interface CopyCommandProps {
  cmd: string
  copied: boolean
  onCopy: () => void
  small?: boolean
}

export function CopyCommand({ cmd, copied, onCopy, small }: CopyCommandProps) {
  return (
    <button
      type="button"
      className={small ? 'copy-command copy-command-sm' : 'copy-command'}
      onClick={onCopy}
    >
      <span className="copy-command-text">
        <span className="copy-command-prompt">$</span> {cmd}
      </span>
      <span className="copy-command-chip">{copied ? 'copied' : 'copy'}</span>
    </button>
  )
}
