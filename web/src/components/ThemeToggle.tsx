import { useEffect, useState } from 'react'
import { applyTheme, readTheme, saveTheme, THEME_CHOICES, type ThemeChoice } from '../lib/theme'

const LABELS: Record<ThemeChoice, string> = { system: 'System', light: 'Light', dark: 'Dark' }

/**
 * The theme switch in the sidebar footer (ADR 0053): System, Light or Dark as
 * a segmented control. The pressed button is the current choice. The choice
 * is applied to <html> and remembered per browser.
 */
export function ThemeToggle() {
  const [choice, setChoice] = useState<ThemeChoice>(readTheme)
  useEffect(() => applyTheme(choice), [choice])
  const choose = (next: ThemeChoice) => {
    saveTheme(next)
    setChoice(next)
  }
  return (
    <div className="segmented theme-toggle" role="group" aria-label="Theme">
      {THEME_CHOICES.map((c) => (
        <button key={c} type="button" aria-pressed={choice === c} onClick={() => choose(c)}>
          {LABELS[c]}
        </button>
      ))}
    </div>
  )
}
