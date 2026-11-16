import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderApp } from '../../test/renderApp'
import { serverRoutes, settingsView } from '../../test/serverFixtures'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('server information', () => {
  it('shows tabs only for installed systems', async () => {
    const settings = settingsView({
      webSystem: '',
      dnsSystem: '',
      dnsStatus: false,
      mail: { ...settingsView().mail, system: '' },
      db: { ...settingsView().db, system: 'mysql' },
    })
    renderApp('/server/info', serverRoutes({ 'GET /server/settings': () => ({ body: settings }) }))
    expect(await screen.findByRole('tab', { name: 'DB' })).toBeTruthy()
    for (const name of ['CPU', 'RAM', 'Disk', 'Network']) expect(screen.getByRole('tab', { name })).toBeTruthy()
    for (const name of ['Web', 'DNS', 'Mail']) expect(screen.queryByRole('tab', { name })).toBeNull()
  })

  it('treats a system set to "no" as not installed', async () => {
    const settings = settingsView({
      webSystem: 'no',
      dnsSystem: 'no',
      dnsStatus: false,
      mail: { ...settingsView().mail, system: 'no' },
      db: { ...settingsView().db, system: 'mysql' },
    })
    renderApp('/server/info', serverRoutes({ 'GET /server/settings': () => ({ body: settings }) }))
    expect(await screen.findByRole('tab', { name: 'DB' })).toBeTruthy()
    for (const name of ['Web', 'DNS', 'Mail']) expect(screen.queryByRole('tab', { name })).toBeNull()
  })

  it('hides the DNS tab when the server offers no DNS status (RHEL)', async () => {
    const settings = settingsView({ dnsSystem: 'named', dnsStatus: false })
    renderApp('/server/info', serverRoutes({ 'GET /server/settings': () => ({ body: settings }) }))
    expect(await screen.findByRole('tab', { name: 'Mail' })).toBeTruthy()
    expect(screen.queryByRole('tab', { name: 'DNS' })).toBeNull()
  })

  it('shows every tab when everything is installed', async () => {
    renderApp('/server/info', serverRoutes())
    expect(await screen.findByRole('tab', { name: 'Mail' })).toBeTruthy()
    for (const name of ['Web', 'DNS', 'DB']) expect(screen.getByRole('tab', { name })).toBeTruthy()
  })

  it('renders the dump as text inside a pre, never as HTML, and has no meters', async () => {
    renderApp(
      '/server/info',
      serverRoutes({ 'GET /server/status/cpu': () => ({ body: { kind: 'cpu', text: 'Model <b>x</b>\n  cores 4' } }) }),
    )
    const pre = await waitFor(() => {
      const el = document.querySelector('pre')
      if (!el) throw new Error('no pre')
      return el
    })
    expect(pre.textContent).toBe('Model <b>x</b>\n  cores 4')
    expect(pre.querySelector('b')).toBeNull()
    expect(within(screen.getByRole('main')).queryByRole('meter')).toBeNull()
    expect(document.body.textContent).not.toMatch(/Hestia/)
  })

  it('loads the dump of the chosen tab and refreshes it', async () => {
    const { calls } = renderApp('/server/info', serverRoutes())
    fireEvent.click(await screen.findByRole('tab', { name: 'Disk' }))
    await waitFor(() => expect(document.querySelector('pre')?.textContent).toBe('disk status'))
    const before = calls.filter((c) => c.key === 'GET /server/status/disk').length
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }))
    await waitFor(() => expect(calls.filter((c) => c.key === 'GET /server/status/disk').length).toBeGreaterThan(before))
  })

  it('shows the error when a dump cannot be read', async () => {
    renderApp(
      '/server/info',
      serverRoutes({ 'GET /server/status/cpu': () => ({ status: 500, body: { error: { code: 'command_failed', message: 'Error: boom' } } }) }),
    )
    expect(await screen.findByText('Error: boom')).toBeTruthy()
  })

  it('says so when the installed systems cannot be read', async () => {
    renderApp(
      '/server/info',
      serverRoutes({ 'GET /server/settings': () => ({ status: 500, body: { error: { code: 'command_failed', message: 'Error: no config' } } }) }),
    )
    expect(await screen.findByText(/Some tabs may be missing/)).toBeTruthy()
    expect(screen.getByRole('tab', { name: 'CPU' })).toBeTruthy()
  })
})
