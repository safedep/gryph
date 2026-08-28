import './components.css'

interface LogoProps {
  size?: 'md' | 'sm'
  onClick?: () => void
}

export function Logo({ size = 'md', onClick }: LogoProps) {
  const className =
    'logo' + (size === 'sm' ? ' logo-sm' : '') + (onClick ? ' logo-clickable' : '')
  return (
    <span className={className} onClick={onClick}>
      <span className="logo-ring">
        <span className="logo-dot" />
      </span>
      <span className="logo-word">Gryph</span>
    </span>
  )
}
