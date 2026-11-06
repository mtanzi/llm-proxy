import { Link, Outlet } from '@tanstack/react-router'

// The five views and the admin page. Views come with ADRs 0031 to 0035.
const NAV = [
  { to: '/calls', label: 'Calls' },
  { to: '/sessions', label: 'Sessions' },
  { to: '/tools', label: 'Tools' },
  { to: '/cost', label: 'Cost' },
  { to: '/admin/config', label: 'Config' },
] as const

/** Shared shell: skip link, header, navigation and the main landmark. */
export function Layout() {
  return (
    <>
      <a className="skip-link" href="#main">
        Skip to content
      </a>
      <header className="app-header">
        <span className="brand">llm-proxy</span>
        <nav aria-label="Main">
          <ul>
            {NAV.map((item) => (
              <li key={item.to}>
                <Link to={item.to} activeProps={{ 'aria-current': 'page' }}>
                  {item.label}
                </Link>
              </li>
            ))}
          </ul>
        </nav>
      </header>
      <main id="main" tabIndex={-1}>
        <Outlet />
      </main>
    </>
  )
}
