import { DISCORD_URL, DOCS_URL, REPO_URL } from '../../data/site'
import { MIDDOT } from '../../data/glyphs'

export function Footer() {
  return (
    <footer className="landing-footer">
      <span className="landing-footer-word">Gryph</span>
      <a href={DOCS_URL} target="_blank" rel="noreferrer">
        docs
      </a>
      <a href={REPO_URL} target="_blank" rel="noreferrer">
        github
      </a>
      <a href={DISCORD_URL} target="_blank" rel="noreferrer">
        discord
      </a>
      <div className="spacer" />
      <span>built for developers {MIDDOT} Apache 2.0</span>
    </footer>
  )
}
