const W = 100
const H = 24

/**
 * A small trend line of one value per slot. A null value (a slot without
 * calls, for a ratio or a percentile) breaks the line. The number beside it
 * carries the meaning, so the line is decorative and hidden from assistive
 * technology.
 */
export function Sparkline({ values, tone = 'neutral' }: { values: (number | null)[]; tone?: 'neutral' | 'error' }) {
  const known = values.filter((v): v is number => v !== null)
  if (known.length === 0) return <svg className="sparkline" aria-hidden="true" viewBox={`0 0 ${W} ${H}`} />
  const min = Math.min(0, ...known)
  const max = Math.max(...known)
  const span = max - min || 1
  const step = values.length > 1 ? W / (values.length - 1) : W
  const segments: string[] = []
  let current: string[] = []
  values.forEach((v, i) => {
    if (v === null) {
      if (current.length > 0) segments.push(current.join(' '))
      current = []
      return
    }
    const y = H - 1 - ((v - min) / span) * (H - 2)
    current.push(`${(i * step).toFixed(1)},${y.toFixed(1)}`)
  })
  if (current.length > 0) segments.push(current.join(' '))
  return (
    <svg className={`sparkline sparkline-${tone}`} aria-hidden="true" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none">
      {segments.map((points) =>
        points.includes(' ') ? (
          <polyline key={points} points={points} vectorEffect="non-scaling-stroke" />
        ) : (
          <circle key={points} cx={points.split(',')[0]} cy={points.split(',')[1]} r={1} />
        ),
      )}
    </svg>
  )
}
