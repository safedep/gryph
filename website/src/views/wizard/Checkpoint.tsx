import { CopyCommand } from '../../components/CopyCommand'
import { CHECK } from '../../data/glyphs'

interface CheckpointProps {
  cmd: string
  prompt: string
  copied: boolean
  onCopy: () => void
  confirmed: boolean
  onConfirm: () => void
}

export function Checkpoint({ cmd, prompt, copied, onCopy, confirmed, onConfirm }: CheckpointProps) {
  return (
    <div className="checkpoint">
      <div className="checkpoint-title">{CHECK} CHECKPOINT</div>
      <CopyCommand small cmd={cmd} copied={copied} onCopy={onCopy} />
      <div className="checkpoint-prompt">{prompt}</div>
      <button
        type="button"
        className={confirmed ? 'checkpoint-button checkpoint-button-done' : 'checkpoint-button'}
        onClick={onConfirm}
      >
        {confirmed ? `${CHECK} checkpoint cleared` : 'mark checkpoint done'}
      </button>
    </div>
  )
}
