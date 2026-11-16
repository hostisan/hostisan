import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { anonymous, renderApp, signedIn } from '../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('app shell', () => {
  it('sends signed-out visitors to the login page', async () => {
    const app = renderApp('/web', { 'GET /bootstrap': () => ({ body: anonymous }) })
    await waitFor(() => expect(app.location()).toBe('/login'))
  })

  it('shows administration to an admin', async () => {
    renderApp('/web', { 'GET /bootstrap': () => ({ body: signedIn({}) }) })
    expect(await screen.findByText('Administration')).toBeTruthy()
  })

  it('shows the impersonation banner and hides administration while looking', async () => {
    let looking = true
    const app = renderApp('/web', {
      'GET /bootstrap': () => ({ body: signedIn(looking ? { look: 'bob', landing: '/dns' } : {}) }),
      'POST /auth/logout': () => {
        looking = false
        return { body: { status: 'returned' } }
      },
    })
    expect(await screen.findByText(/You are viewing the panel as bob/)).toBeTruthy()
    expect(screen.queryByText('Administration')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /Return to admin/ }))
    await waitFor(() => expect(app.calls.some((c) => c.key === 'POST /auth/logout')).toBe(true))
    await waitFor(() => expect(screen.queryByText(/You are viewing the panel as/)).toBeNull())
  })

  it('goes to login with the reason when the session is lost', async () => {
    let lost = false
    const app = renderApp('/web', {
      'GET /bootstrap': () => ({ body: lost ? anonymous : signedIn({}) }),
      'POST /auth/logout': () => {
        lost = true
        return {
          status: 401,
          body: { error: { code: 'logged_out', message: 'You are logged out, please log in again.' } },
        }
      },
    })
    fireEvent.click(await screen.findByRole('button', { name: 'admin' }))
    fireEvent.click(await screen.findByText('Log out'))
    await waitFor(() => expect(app.location()).toBe('/login'))
    expect(await screen.findByText('You are logged out, please log in again.')).toBeTruthy()
  })

  it('goes to login with the reason when a reload finds the session ended', async () => {
    renderApp('/web', {
      'GET /bootstrap': () => ({ body: { ...anonymous, signedOutReason: 'You are logged out, please log in again.' } }),
    })
    expect(await screen.findByText('You are logged out, please log in again.')).toBeTruthy()
  })

  it('shows a retry screen, not a made-up session, when the server is unreachable', async () => {
    let up = false
    const app = renderApp('/web', {
      'GET /bootstrap': () =>
        up ? { body: anonymous } : { status: 502, body: { error: { code: 'bad_gateway', message: '' } } },
    })
    expect(await screen.findByText("Can't reach the server")).toBeTruthy()
    expect(screen.queryByText('System Administrator')).toBeNull()
    up = true
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
    await waitFor(() => expect(app.location()).toBe('/login'))
  })
})

describe('footer', () => {
  it('links to server status only when the user may see the server pages', async () => {
    renderApp('/web', { 'GET /bootstrap': () => ({ body: signedIn({}) }) })
    const footer = await screen.findByRole('navigation', { name: 'Footer' })
    expect(footer.textContent).toContain('Server status')
    cleanup()
    const base = signedIn({}).user
    renderApp('/web', {
      'GET /bootstrap': () => ({
        body: signedIn({ role: 'user', name: 'bob', features: { ...base.features, server: false } }),
      }),
    })
    const bobFooter = await screen.findByRole('navigation', { name: 'Footer' })
    expect(bobFooter.textContent).not.toContain('Server status')
    expect(bobFooter.textContent).toContain('Terms of use')
  })
})

describe('shell language', () => {
  const fr: Record<string, string> = {
    Hosting: 'Hébergement',
    Mail: 'Messagerie',
    'DNS records': 'Enregistrements DNS',
    Usage: 'Utilisation',
    Disk: 'Disque',
    Search: 'Rechercher',
    'You are viewing the panel as %s.': 'Vous consultez le panneau en tant que %s.',
    'Return to %s': 'Revenir à %s',
    'Edit account': 'Modifier le compte',
    'Log out': 'Déconnexion',
    'Add DNS zone': 'Ajouter une zone DNS',
    Actions: 'Actions rapides',
  }
  const french = (user: Record<string, unknown>) => ({
    'GET /bootstrap': () => ({ body: { ...signedIn(user), language: 'fr' } }),
    'GET /i18n/fr?v=1.0.0': () => ({ body: fr }),
  })

  it('the sidebar, breadcrumbs, banner and menus follow the language', async () => {
    renderApp('/dns/example.com/records/1', french({ look: 'bob', landing: '/dns' }))
    expect(await screen.findByText('Hébergement')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Messagerie' })).toBeTruthy()
    expect(screen.getByText('Utilisation')).toBeTruthy()
    expect(screen.getByText('Disque')).toBeTruthy()
    expect(screen.getByText('Vous consultez le panneau en tant que bob.')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Revenir à admin' })).toBeTruthy()
    // Named crumbs are translated; names from the URL are not.
    const trail = screen.getByRole('navigation', { name: /breadcrumb/i })
    expect(trail.textContent).toContain('example.com')
    fireEvent.click(screen.getByRole('button', { name: 'admin' }))
    expect(await screen.findByRole('menuitem', { name: 'Modifier le compte' })).toBeTruthy()
    expect(screen.getByRole('menuitem', { name: 'Déconnexion' })).toBeTruthy()
  })

  it('the palette follows the language', async () => {
    renderApp('/dns', french({}))
    await screen.findByText('Hébergement')
    fireEvent.keyDown(window, { key: 'k', ctrlKey: true })
    expect(await screen.findByText('Ajouter une zone DNS')).toBeTruthy()
    expect(screen.getByText('Actions rapides')).toBeTruthy()
  })

  it('segment crumbs are translated', async () => {
    renderApp('/mail/example.com/dns', french({}))
    await screen.findByText('Hébergement')
    const trail = screen.getByRole('navigation', { name: /breadcrumb/i })
    await waitFor(() => expect(trail.textContent).toContain('Enregistrements DNS'))
    expect(trail.textContent).toContain('Messagerie')
  })
})

describe('breadcrumbs on a phone', () => {
  // Kumo shows "… > parent > current" below sm, when it can see the crumbs as its own
  // children. Wrapped in fragments, the whole trail stayed and the current page was
  // squeezed out, leaving a bare ">".
  it('shorten to the parent and the current page', async () => {
    renderApp('/mail/example.com/dns', { 'GET /bootstrap': () => ({ body: signedIn({}) }) })
    const trail = await screen.findByRole('navigation', { name: /breadcrumb/i })
    await waitFor(() => expect(trail.querySelector('[aria-current="page"]')).toBeTruthy())
    const phone = trail.firstElementChild as HTMLElement
    expect(phone.className).toContain('sm:hidden')
    expect(phone.textContent).toContain('...')
    expect(phone.textContent).not.toContain('admin')
    expect(phone.lastElementChild?.getAttribute('aria-current')).toBe('page')
  })
})
