import { useState } from 'react'
import { Landing } from './views/landing/Landing'
import { Wizard } from './views/wizard/Wizard'
import { Completion } from './views/complete/Completion'
import { useStars } from './hooks/useStars'
import { TRACKS } from './data/tracks'

type View = 'landing' | 'wizard' | 'complete'

export default function App() {
  const [view, setView] = useState<View>('landing')
  const [ti, setTi] = useState(0)
  const stars = useStars()

  const go = (next: View, track?: number) => {
    setView(next)
    if (track !== undefined) setTi(track)
    window.scrollTo(0, 0)
  }

  if (view === 'wizard') {
    return (
      <Wizard
        stars={stars}
        ti={ti}
        onHome={() => go('landing', 0)}
        onSelectTrack={(i) => go('wizard', i)}
        onBack={() => (ti === 0 ? go('landing', 0) : go('wizard', ti - 1))}
        onNext={() => (ti >= TRACKS.length ? go('complete') : go('wizard', ti + 1))}
      />
    )
  }

  if (view === 'complete') {
    return <Completion stars={stars} onHome={() => go('landing', 0)} />
  }

  return <Landing stars={stars} onStart={() => go('wizard', 0)} />
}
