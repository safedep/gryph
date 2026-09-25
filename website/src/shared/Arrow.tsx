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
        strokeWidth="1.6"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}
