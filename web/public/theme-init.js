// Applies the stored theme before first paint, so a forced light or dark
// theme does not flash the other one (ADR 0053). It is a classic blocking
// script in <head> because the CSP allows no inline script. It repeats
// readTheme and applyTheme from src/lib/theme.ts: keep the two in step.
;(function () {
  try {
    var stored = window.localStorage.getItem('spyglass-theme')
    if (stored === 'light' || stored === 'dark') {
      document.documentElement.setAttribute('data-theme', stored)
    }
  } catch (e) {
    // Storage is unavailable: follow the system.
  }
})()
