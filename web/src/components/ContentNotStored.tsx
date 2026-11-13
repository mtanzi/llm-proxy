/**
 * The one notice for places where a reader might expect message text, tool
 * arguments or tool results. Spyglass stores metadata only (ADR 0002), so
 * the UI says so once instead of showing empty panels.
 */
export function ContentNotStored() {
  return (
    <p className="notice" role="note">
      Content is not stored
    </p>
  )
}
