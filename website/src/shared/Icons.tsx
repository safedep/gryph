// An SVG arrow sits on the optical centre of the text. A text arrow sits low in most fonts.
export function Arrow({ back }: { back?: boolean }) {
  return (
    <svg
      className="arrow"
      viewBox="0 0 16 16"
      aria-hidden="true"
      style={back ? { transform: 'scaleX(-1)' } : undefined}
    >
      <path
        d="M2.5 8h11M9 3.5 13.5 8 9 12.5"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="square"
        strokeLinejoin="miter"
      />
    </svg>
  )
}

// The pixel GitHub mark from the Cartridge design system, on a 24 by 24 grid.
const GITHUB_PIXEL =
  'M5 2h4v2H7v2H5V2Zm0 10H3V6h2v6Zm2 2H5v-2h2v2Zm2 2v-2H7v2H3v-2H1v2h2v2h4v4h2v-4h2v-2H9Zm0 0v2H7v-2h2Zm6-12v2H9V4h6Zm4 2h-2V4h-2V2h4v4Zm0 6V6h2v6h-2Zm-2 2v-2h2v2h-2Zm-2 2v-2h2v2h-2Zm0 2h-2v-2h2v2Zm0 0h2v4h-2v-4Z'

export function GitHubMark() {
  return (
    <svg className="gh" viewBox="0 0 24 24" aria-hidden="true" shapeRendering="crispEdges">
      <path fill="currentColor" d={GITHUB_PIXEL} />
    </svg>
  )
}

export function Check() {
  return (
    <svg className="check" viewBox="0 0 10 10" aria-hidden="true">
      <path d="M1.5 5.5 4 8l4.5-6" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="square" />
    </svg>
  )
}

export function Cross() {
  return (
    <svg className="check" viewBox="0 0 10 10" aria-hidden="true">
      <path d="M2 2l6 6M8 2 2 8" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="square" />
    </svg>
  )
}
