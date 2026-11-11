import { UNKNOWN } from '../lib/format'

/** Whether a call failed: it has an error class or an HTTP status of 400 or more. */
export function isErrorStatus(status: number, errorClass: string): boolean {
  return errorClass !== '' || status >= 400
}

/**
 * A small status badge: the HTTP status, and on an error the error class.
 * An error badge also has a fill and a glyph, so it does not rely on colour
 * alone. Status 0 without an error class is not known.
 */
export function StatusBadge({ status, errorClass = '' }: { status: number; errorClass?: string }) {
  if (status === 0 && errorClass === '') return <span className="unknown">{UNKNOWN}</span>
  const error = isErrorStatus(status, errorClass)
  const text = [status === 0 ? '' : String(status), errorClass].filter((v) => v !== '').join(' · ')
  return <span className={error ? 'badge badge-error' : 'badge badge-ok'}>{text}</span>
}

/** A subtle tag for a name such as a model or a client. Unknown stays plain muted text. */
export function Tag({ text, className }: { text: string; className?: string }) {
  if (text === UNKNOWN) return <span className="unknown">{UNKNOWN}</span>
  return (
    <span className={className ? `tag ${className}` : 'tag'} title={text}>
      {text}
    </span>
  )
}
