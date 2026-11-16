import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { useRef } from 'react'
import { Link, Navigate } from 'react-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '../api/client'
import { AppLayout } from '../layout/AppLayout'
import { useSession } from '../session/SessionProvider'
import { anonymous, renderApp, signedIn, type Route } from '../test/renderApp'
import { UnsavedChangesDialog } from './UnsavedChangesDialog'
import { useLeaveGuard } from './useLeaveGuard'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

/** A bare form that is armed once its field changes. */
function ProbeForm() {
  const dirty = useRef(false)
  const { blocker } = useLeaveGuard(() => dirty.current)
  return (
    <form onChange={() => (dirty.current = true)}>
      <label>
        Name
        <input name="name" />
      </label>
      <Link to="/dns">To DNS</Link>
      <Link to="/probe-form?tab=2">Same page query</Link>
      <Link to="/probe-form#part">Same page hash</Link>
      <Link to="/login">To login</Link>
      <button type="button" onClick={() => void api.get('/ping').catch(() => {})}>
        Ping
      </button>
      <UnsavedChangesDialog blocker={blocker} />
    </form>
  )
}

/** As App's RequireSession: the shell renders only for a signed-in session. */
function SignedIn() {
  const { bootstrap } = useSession()
  if (!bootstrap) return null
  if (!bootstrap.authenticated) return <Navigate to="/login" replace />
  return <AppLayout />
}

// Inside the app shell, so the top bar's Log out is there.
const probe = [{ element: <SignedIn />, children: [{ path: '/probe-form', element: <ProbeForm /> }] }]

function routes(over: Record<string, Route> = {}): Record<string, Route> {
  return {
    'GET /bootstrap': () => ({ body: signedIn({}) }),
    'GET /dns': () => ({
      body: { zones: [], usage: { zones: '0', zonesLimit: '5', suspended: '0', records: '0', recordsLimit: '250' } },
    }),
    ...over,
  }
}

async function typeName(value = 'changed') {
  const input = (await screen.findByRole('textbox', { name: 'Name' })) as HTMLInputElement
  fireEvent.change(input, { target: { value } })
  return input
}

const dialog = () => screen.queryByRole('dialog', { name: 'Unsaved changes' })

describe('useLeaveGuard', () => {
  it('a changed field asks before leaving through a link', async () => {
    const app = renderApp('/probe-form', routes(), probe)
    await typeName()
    fireEvent.click(screen.getByRole('link', { name: 'To DNS' }))
    expect(await screen.findByRole('dialog', { name: 'Unsaved changes' })).toBeTruthy()
    expect(app.location()).toBe('/probe-form')
  })

  it('an unchanged form leaves without asking', async () => {
    const app = renderApp('/probe-form', routes(), probe)
    await screen.findByRole('textbox', { name: 'Name' })
    fireEvent.click(screen.getByRole('link', { name: 'To DNS' }))
    await waitFor(() => expect(app.location()).toBe('/dns'))
    expect(dialog()).toBeNull()
  })

  it('stay keeps the page and the typed values', async () => {
    const app = renderApp('/probe-form', routes(), probe)
    await typeName('kept')
    fireEvent.click(screen.getByRole('link', { name: 'To DNS' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Stay on page' }))
    await waitFor(() => expect(screen.getByRole('dialog', { hidden: true }).getAttribute('data-closed')).not.toBeNull())
    expect(app.location()).toBe('/probe-form')
    expect((screen.getByRole('textbox', { name: 'Name' }) as HTMLInputElement).value).toBe('kept')
    expect(
      app.router.state.blockers.size === 0 ||
        [...app.router.state.blockers.values()].every((b) => b.state === 'unblocked'),
    ).toBe(true)
  })

  it('leave goes to the link', async () => {
    const app = renderApp('/probe-form', routes(), probe)
    await typeName()
    fireEvent.click(screen.getByRole('link', { name: 'To DNS' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Leave page' }))
    await waitFor(() => expect(app.location()).toBe('/dns'))
  })

  it('beforeunload is asked only while changed', async () => {
    renderApp('/probe-form', routes(), probe)
    await screen.findByRole('textbox', { name: 'Name' })
    const clean = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(clean)
    expect(clean.defaultPrevented).toBe(false)

    await typeName()
    const changed = new Event('beforeunload', { cancelable: true }) as Event & { returnValue: unknown }
    // As BeforeUnloadEvent's: a plain string field (jsdom's Event has the legacy boolean).
    Object.defineProperty(changed, 'returnValue', { value: '', writable: true })
    window.dispatchEvent(changed)
    expect(changed.defaultPrevented).toBe(true)
    // Chromium before 119 asks only for a truthy returnValue.
    expect(Boolean(changed.returnValue)).toBe(true)

    cleanup()
    const after = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(after)
    expect(after.defaultPrevented).toBe(false)
  })

  it('a query or hash change on the same page does not ask', async () => {
    const app = renderApp('/probe-form', routes(), probe)
    await typeName()
    fireEvent.click(screen.getByRole('link', { name: 'Same page query' }))
    await waitFor(() => expect(app.router.state.location.search).toBe('?tab=2'))
    fireEvent.click(screen.getByRole('link', { name: 'Same page hash' }))
    await waitFor(() => expect(app.router.state.location.hash).toBe('#part'))
    expect(dialog()).toBeNull()
  })

  it('a server-ended session goes to login without asking', async () => {
    let lost = false
    const app = renderApp(
      '/probe-form',
      routes({
        'GET /bootstrap': () => ({ body: lost ? anonymous : signedIn({}) }),
        'GET /ping': () => {
          lost = true
          return { status: 401, body: { error: { code: 'session_expired', message: 'Session expired.' } } }
        },
      }),
      probe,
    )
    await typeName()
    fireEvent.click(screen.getByRole('button', { name: 'Ping' }))
    await waitFor(() => expect(app.location()).toBe('/login'))
    expect(dialog()).toBeNull()
  })

  it('a move to the login page does not ask', async () => {
    const app = renderApp('/probe-form', routes(), probe)
    await typeName()
    fireEvent.click(screen.getByRole('link', { name: 'To login' }))
    // A signed-in visitor goes on from the login page to the landing page.
    await waitFor(() => expect(app.location()).not.toBe('/probe-form'))
    expect(dialog()).toBeNull()
  })

  it('log out does not ask', async () => {
    let out = false
    const app = renderApp(
      '/probe-form',
      routes({
        'GET /bootstrap': () => ({ body: out ? anonymous : signedIn({}) }),
        'POST /auth/logout': () => {
          out = true
          return { body: { status: 'ok' } }
        },
      }),
      probe,
    )
    await typeName()
    fireEvent.click(await screen.findByRole('button', { name: 'admin' }))
    fireEvent.click(await screen.findByText('Log out'))
    await waitFor(() => expect(app.location()).toBe('/login'))
    expect(dialog()).toBeNull()
  })

  it('returning to the admin account does not ask', async () => {
    let looking = true
    const app = renderApp(
      '/probe-form',
      routes({
        'GET /bootstrap': () => ({ body: signedIn(looking ? { look: 'bob' } : {}) }),
        'POST /auth/logout': () => {
          looking = false
          return { body: { status: 'returned' } }
        },
        'GET /users': () => ({ body: { users: [] } }),
      }),
      probe,
    )
    await typeName()
    fireEvent.click(await screen.findByRole('button', { name: /Return to admin/ }))
    await waitFor(() => expect(app.location()).not.toBe('/probe-form'))
    expect(dialog()).toBeNull()
  })

  it("the dialog uses Hestia's message and stays mounted", async () => {
    renderApp('/probe-form', routes(), probe)
    await typeName()
    // Mounted but closed before any navigation.
    expect(dialog()).toBeNull()
    fireEvent.click(screen.getByRole('link', { name: 'To DNS' }))
    const open = await screen.findByRole('dialog', { name: 'Unsaved changes' })
    expect(open.textContent).toContain('Are you sure you want to leave the page?')
    expect(screen.getByRole('button', { name: 'Leave page' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Stay on page' })).toBeTruthy()
  })

  it('translates the dialog', async () => {
    renderApp(
      '/probe-form',
      routes({
        'GET /bootstrap': () => ({ body: { ...signedIn({}), language: 'fr' } }),
        'GET /i18n/fr?v=1.0.0': () => ({
          body: {
            'Unsaved changes': 'Modifications non enregistrées',
            'Are you sure you want to leave the page?': 'Êtes-vous sûr de vouloir quitter la page ?',
            'Leave page': 'Quitter la page',
            'Stay on page': 'Rester sur la page',
          },
        }),
      }),
      probe,
    )
    await typeName()
    fireEvent.click(screen.getByRole('link', { name: 'To DNS' }))
    const open = await screen.findByRole('dialog', { name: 'Modifications non enregistrées' })
    expect(open.textContent).toContain('Êtes-vous sûr de vouloir quitter la page ?')
    expect(screen.getByRole('button', { name: 'Quitter la page' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Rester sur la page' })).toBeTruthy()
  })
})
