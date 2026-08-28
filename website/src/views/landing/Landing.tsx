import { Header } from './Header'
import { Hero } from './Hero'
import { Showcase } from './Showcase'
import { AgentStrip } from './AgentStrip'
import { LocalCallout } from './LocalCallout'
import { Footer } from './Footer'
import './landing.css'

interface LandingProps {
  stars: string
  onStart: () => void
}

export function Landing({ stars, onStart }: LandingProps) {
  return (
    <div className="landing">
      <Header stars={stars} />
      <Hero onStart={onStart} />
      <Showcase />
      <AgentStrip />
      <LocalCallout onStart={onStart} />
      <Footer />
    </div>
  )
}
