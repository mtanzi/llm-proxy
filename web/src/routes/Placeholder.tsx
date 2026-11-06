import { ContentNotStored } from '../components/ContentNotStored'

/** Stands in for a view until its ADR (0031 to 0035) lands. */
export function Placeholder({ title }: { title: string }) {
  return (
    <section aria-labelledby="view-title">
      <h1 id="view-title">{title}</h1>
      <p>This view is under construction.</p>
      <ContentNotStored />
    </section>
  )
}
