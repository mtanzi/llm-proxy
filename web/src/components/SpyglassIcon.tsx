/**
 * The Spyglass mark, "Lens and glint" (ADR 0053), on a 32-unit grid: a lens
 * ring and a reflection arc in the accent (brass, from currentColor) and a
 * solid pupil in the second colour (teal, set by the .pupil rule). Decorative:
 * the product name always sits beside it.
 */
export function SpyglassIcon({ size = 16 }: { size?: number }) {
  return (
    <svg className="brand-icon" width={size} height={size} viewBox="0 0 32 32" aria-hidden="true" focusable="false">
      <circle cx="16" cy="16" r="11" fill="none" stroke="currentColor" strokeWidth="2.4" />
      <circle className="pupil" cx="16" cy="16" r="4.6" />
      <path
        d="M9.6 12.2a7.4 7.4 0 0 1 4.2-4.1"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        opacity="0.6"
      />
    </svg>
  )
}
