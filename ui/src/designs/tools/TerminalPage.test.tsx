import { act, cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { signedIn } from '../../test/renderApp'
import { renderApp } from '../../test/renderApp'
import { toolsRoutes } from '../../test/toolsFixtures'

class FakeSocket {
  static all: FakeSocket[] = []
  sent: unknown[] = []
  binaryType = ''
  onopen: (() => void) | null = null
  onmessage: ((e: { data: unknown }) => void) | null = null
  onclose: ((e: { code: number; reason: string; wasClean: boolean }) => void) | null = null
  onerror: (() => void) | null = null
  url: string
  constructor(url: string) {
    this.url = url
    FakeSocket.all.push(this)
  }
  send(d: unknown) {
    this.sent.push(d)
  }
  close() {}
}

// jsdom has no renderer: real xterm schedules animation frames that can run after
// dispose and throw. A minimal fake keeps these tests deterministic.
vi.mock('@xterm/xterm', () => ({
  Terminal: class {
    cols = 80
    rows = 24
    loadAddon() {}
    open() {}
    reset() {}
    focus() {}
    write() {}
    onData() {
      return { dispose() {} }
    }
    dispose() {}
  },
}))
vi.mock('@xterm/addon-fit', () => ({
  FitAddon: class {
    fit() {}
    activate() {}
    dispose() {}
  },
}))

beforeEach(() => {
  FakeSocket.all = []
  vi.stubGlobal('WebSocket', FakeSocket)
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const features = (over: Record<string, boolean>) => ({
  web: true,
  dns: true,
  db: true,
  mail: true,
  cron: true,
  backup: true,
  incremental: true,
  logs: true,
  loginHistory: true,
  accessKeys: true,
  server: true,
  firewall: true,
  firewallBans: true,
  terminal: true,
  fileManager: true,
  ...over,
})

describe('terminal page', () => {
  it('posts a token, then opens the socket on the page host and authenticates', async () => {
    const app = renderApp('/terminal', toolsRoutes())
    await waitFor(() => expect(FakeSocket.all).toHaveLength(1))
    expect(app.calls.filter((c) => c.key === 'POST /terminal/token')).toHaveLength(1)
    const ws = FakeSocket.all[0]
    expect(ws.url).toBe(`${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/api/v1/terminal/socket`)
    act(() => ws.onopen?.())
    const auth = JSON.parse(ws.sent[0] as string)
    expect(auth).toMatchObject({ type: 'auth', token: '0123456789abcdef0123456789abcdef' })
    expect(typeof auth.cols).toBe('number')
    expect(screen.getByTestId('terminal')).toBeTruthy()
    expect(document.body.textContent).not.toMatch(/Hestia/)
  })

  it('shows no-shell as a banner and opens no socket', async () => {
    renderApp(
      '/terminal',
      toolsRoutes({
        'POST /terminal/token': () => ({
          status: 403,
          body: { error: { code: 'no_shell', message: 'You have no shell access.' } },
        }),
      }),
    )
    const banner = await screen.findByText('You have no shell access.')
    expect(banner).toBeTruthy()
    expect(FakeSocket.all).toHaveLength(0)
  })

  it('shows the close reason and reconnects with a fresh token', async () => {
    const app = renderApp('/terminal', toolsRoutes())
    await waitFor(() => expect(FakeSocket.all).toHaveLength(1))
    act(() => FakeSocket.all[0].onclose?.({ code: 1000, reason: 'Your session has expired.', wasClean: true }))
    expect(await screen.findByText('Your session has expired.')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Reconnect' }))
    await waitFor(() => expect(FakeSocket.all).toHaveLength(2))
    expect(app.calls.filter((c) => c.key === 'POST /terminal/token')).toHaveLength(2)
    expect(screen.queryByText('Your session has expired.')).toBeNull()
  })

  it('shows the terminal process refusing another shell', async () => {
    renderApp('/terminal', toolsRoutes())
    await waitFor(() => expect(FakeSocket.all).toHaveLength(1))
    const reason = 'Too many terminals are open. Close one and try again.'
    act(() => FakeSocket.all[0].onclose?.({ code: 1000, reason, wasClean: true }))
    expect(await screen.findByText(reason)).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Reconnect' })).toBeTruthy()
  })

  it('shows an empty state and posts nothing when the terminal is off', async () => {
    const app = renderApp('/terminal', {
      ...toolsRoutes(),
      'GET /bootstrap': () => ({ body: signedIn({ features: features({ terminal: false }) }) }),
    })
    expect(await screen.findByText('The web terminal is not available')).toBeTruthy()
    expect(app.calls.some((c) => c.key === 'POST /terminal/token')).toBe(false)
    expect(FakeSocket.all).toHaveLength(0)
  })

  it('says why a read-only session has no terminal', async () => {
    const app = renderApp('/terminal', {
      ...toolsRoutes(),
      'GET /bootstrap': () => ({ body: signedIn({ readOnly: true, features: features({ terminal: false }) }) }),
    })
    expect(await screen.findByText("Read-only sessions can't use the web terminal.")).toBeTruthy()
    expect(app.calls.some((c) => c.key === 'POST /terminal/token')).toBe(false)
  })

  it('goes back to the landing page', async () => {
    const { location: loc } = renderApp('/terminal', toolsRoutes())
    await waitFor(() => expect(FakeSocket.all).toHaveLength(1))
    fireEvent.click(screen.getByRole('link', { name: 'Back' }))
    await waitFor(() => expect(loc()).toBe('/users'))
  })

  it('shows connecting, focuses on open and tells a dropped link from a clean close', async () => {
    renderApp('/terminal', toolsRoutes())
    expect(await screen.findByText('Connecting…')).toBeTruthy()
    await waitFor(() => expect(FakeSocket.all).toHaveLength(1))
    act(() => FakeSocket.all[0].onmessage?.({ data: new Uint8Array([104]).buffer }))
    await waitFor(() => expect(screen.queryByText('Connecting…')).toBeNull())
    act(() => FakeSocket.all[0].onerror?.())
    const note = await screen.findByText('Connection error.')
    expect(note.closest('[data-variant="error"], [class*="danger"], [class*="error"]')).toBeTruthy()
  })

  it('offers Reconnect when the token request fails', async () => {
    let n = 0
    renderApp(
      '/terminal',
      toolsRoutes({
        'POST /terminal/token': () =>
          ++n === 1
            ? { status: 500, body: { error: { code: 'server_error', message: 'Error: boom' } } }
            : { body: { token: 'abcd', expires: '2026-10-28T10:00:30Z' } },
      }),
    )
    expect(await screen.findByText('Error: boom')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Reconnect' }))
    await waitFor(() => expect(FakeSocket.all).toHaveLength(1))
  })

  it('explains a refusal for a protected account', async () => {
    renderApp(
      '/terminal',
      toolsRoutes({
        'POST /terminal/token': () => ({ status: 403, body: { error: { code: 'forbidden', message: '' } } }),
      }),
    )
    expect(await screen.findByText("The web terminal isn't available for this account.")).toBeTruthy()
    expect(FakeSocket.all).toHaveLength(0)
  })

  it('is unavailable for a read-only session even with the feature on', async () => {
    const app = renderApp('/terminal', {
      ...toolsRoutes(),
      'GET /bootstrap': () => ({ body: signedIn({ readOnly: true, features: features({}) }) }),
    })
    expect(await screen.findByText("Read-only sessions can't use the web terminal.")).toBeTruthy()
    expect(app.calls.some((c) => c.key === 'POST /terminal/token')).toBe(false)
  })
})
