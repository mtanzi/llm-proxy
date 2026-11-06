import { errorMessage } from '../api/errors'

export function LoadingState({ label = 'Loading' }: { label?: string }) {
  return (
    <p className="state" role="status">
      {label}…
    </p>
  )
}

export function EmptyState({ children }: { children: React.ReactNode }) {
  return <p className="state">{children}</p>
}

/** Inline message for an API or network error (ADR 0030). */
export function ErrorMessage({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  return (
    <div className="state error" role="alert">
      <span>{errorMessage(error)}</span>
      {onRetry && (
        <button type="button" onClick={onRetry}>
          Retry
        </button>
      )}
    </div>
  )
}
