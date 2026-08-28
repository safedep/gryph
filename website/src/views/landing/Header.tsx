import { Logo } from '../../components/Logo'
import { StarButton } from '../../components/StarButton'
import { DOCS_URL } from '../../data/site'

export function Header({ stars }: { stars: string }) {
  return (
    <header className="landing-header">
      <Logo />
      <div className="spacer" />
      <a className="landing-header-docs" href={DOCS_URL} target="_blank" rel="noreferrer">
        docs
      </a>
      <StarButton stars={stars} />
    </header>
  )
}
