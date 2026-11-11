import { Link, Outlet } from '@tanstack/react-router'
import { SpyglassIcon } from './SpyglassIcon'
import { ThemeToggle } from './ThemeToggle'

// The overview (ADR 0052), the views and the admin page. The Cost and Config pages are
// placeholders until ADR 0035 builds them, so their links stay out of the sidebar. Their
// routes remain. Re-enable the links when those pages exist (issue 69).
const NAV = [
  { to: '/', label: 'Overview' },
  { to: '/calls', label: 'Calls' },
  { to: '/sessions', label: 'Sessions' },
  { to: '/tools', label: 'Tools' },
] as const

/**
 * Shared shell: skip link, a Linear-style sidebar with the Spyglass brand,
 * the main navigation (the current page is marked with aria-current and the
 * accent) and the theme toggle in its footer, and the main landmark beside it.
 */
export function Layout() {
  return (
    <div className="app-shell">
      <a className="skip-link" href="#main">
        Skip to content
      </a>
      <header className="app-sidebar">
        <span className="brand">
          <SpyglassIcon />
          Spyglass
        </span>
        <nav aria-label="Main">
          <ul>
            {NAV.map((item) => (
              <li key={item.to}>
                <Link
                  to={item.to}
                  activeOptions={{ exact: item.to === '/', includeSearch: false }}
                  activeProps={{ 'aria-current': 'page' }}
                >
                  {item.label}
                </Link>
              </li>
            ))}
          </ul>
        </nav>
        <div className="sidebar-foot">
          <ThemeToggle />
        </div>
      </header>
      <main id="main" tabIndex={-1}>
        <Outlet />
      </main>
    </div>
  )
}
