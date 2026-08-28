import type { Track } from '../../data/tracks'
import { CopyCommand } from '../../components/CopyCommand'
import { Checkpoint } from './Checkpoint'
import { Quiz } from './Quiz'
import { MIDDOT } from '../../data/glyphs'

interface StepTrackProps {
  num: number
  track: Track
  copied: string | null
  onCopy: (text: string) => void
  installMethod: string
  onInstallMethod: (key: string) => void
  confirmed: boolean
  onToggleConfirm: () => void
  quizPick: number | undefined
  onQuizPick: (i: number) => void
}

export function StepTrack({
  num,
  track,
  copied,
  onCopy,
  installMethod,
  onInstallMethod,
  confirmed,
  onToggleConfirm,
  quizPick,
  onQuizPick,
}: StepTrackProps) {
  return (
    <div className="steptrack">
      <div className="kicker">
        TRACK {num} {MIDDOT} {track.time.toUpperCase()}
      </div>
      <h2 className="track-title">{track.title}</h2>
      <div className="goal-block">
        <b>Goal.</b> {track.goal}
      </div>

      <div className="steps">
        {track.steps.map((s, i) => {
          const selected = s.methods?.find((m) => m.key === installMethod) ?? s.methods?.[0]
          const cmd = selected?.cmd ?? s.cmd ?? ''
          return (
            <div className="step" key={s.label}>
              <span className="step-num">{i + 1}</span>
              <div>
                <div className="step-label">{s.label}</div>
                {s.methods && (
                  <div className="step-methods">
                    {s.methods.map((m) => (
                      <button
                        type="button"
                        key={m.key}
                        className={
                          m.key === selected?.key ? 'step-method step-method-active' : 'step-method'
                        }
                        onClick={() => onInstallMethod(m.key)}
                      >
                        {m.label}
                      </button>
                    ))}
                  </div>
                )}
                <CopyCommand cmd={cmd} copied={copied === cmd} onCopy={() => onCopy(cmd)} />
                <div className="step-expected">{s.expected}</div>
              </div>
            </div>
          )
        })}
      </div>

      <div className="checkpoint-takeaway">
        <Checkpoint
          cmd={track.checkpoint.cmd}
          prompt={track.checkpoint.prompt}
          copied={copied === track.checkpoint.cmd}
          onCopy={() => onCopy(track.checkpoint.cmd)}
          confirmed={confirmed}
          onConfirm={onToggleConfirm}
        />
        <div className="takeaway">
          <div className="takeaway-label">Key takeaway</div>
          <div className="takeaway-text">{track.takeaway}</div>
        </div>
      </div>

      <Quiz quiz={track.quiz} pick={quizPick} onPick={onQuizPick} />
    </div>
  )
}
