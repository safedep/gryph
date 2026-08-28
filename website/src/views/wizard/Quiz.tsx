import type { TrackQuiz } from '../../data/tracks'
import { CHECK, CROSS, MIDDOT } from '../../data/glyphs'

interface QuizProps {
  quiz: TrackQuiz
  pick: number | undefined
  onPick: (i: number) => void
}

export function Quiz({ quiz, pick, onPick }: QuizProps) {
  const answered = pick != null
  return (
    <div className="quiz">
      <div className="quiz-q">
        Quiz {MIDDOT} {quiz.q}
      </div>
      <div className="quiz-options">
        {quiz.opts.map((opt, i) => {
          let cls = 'quiz-option'
          let mark = ''
          if (answered) {
            if (opt.correct) {
              cls += ' quiz-option-correct'
              mark = CHECK
            } else if (i === pick) {
              cls += ' quiz-option-wrong'
              mark = CROSS
            }
          }
          return (
            <button type="button" key={opt.text} className={cls} onClick={() => onPick(i)}>
              <span className="quiz-option-text">{opt.text}</span>
              <span className="quiz-option-mark">{mark}</span>
            </button>
          )
        })}
      </div>
      {answered && <div className="quiz-explain">{quiz.explain}</div>}
    </div>
  )
}
