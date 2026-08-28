import { REPO_URL } from '../data/site'
import { STAR } from '../data/glyphs'
import { GitHubIcon } from './GitHubIcon'
import './components.css'

interface StarButtonProps {
  stars: string
  size?: 'md' | 'sm'
}

export function StarButton({ stars, size = 'md' }: StarButtonProps) {
  return (
    <a
      className={size === 'sm' ? 'star-button star-button-sm' : 'star-button'}
      href={REPO_URL}
      target="_blank"
      rel="noreferrer"
      aria-label="Gryph on GitHub"
    >
      <span className="star-button-label">
        <GitHubIcon />
      </span>
      <span className="star-button-count">
        {STAR} {stars}
      </span>
    </a>
  )
}
