import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderApp, signedIn } from '../../test/renderApp'
import { toolsRoutes } from '../../test/toolsFixtures'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const none = {
  web: false, dns: false, db: false, mail: false, cron: false, backup: false, incremental: false,
  logs: true, loginHistory: true, accessKeys: false, server: false, firewall: false, firewallBans: false,
  terminal: false, fileManager: false,
}
const group = () => within(screen.getByRole('group', { name: 'Your account' }))
const carol = (features: Record<string, boolean> = none) => ({
  ...toolsRoutes(),
  'GET /bootstrap': () => ({
    body: signedIn({ name: 'carol', displayName: 'Carol', role: 'user', landing: '/error', package: 'empty', features }),
  }),
  'POST /auth/logout': () => ({ body: { status: 'ok' } }),
})

describe('empty package page', () => {
  it('says there is nothing to manage and names the package, inside the app shell', async () => {
    renderApp('/error', carol())
    expect(await screen.findByRole('heading', { name: 'Nothing to manage yet' })).toBeTruthy()
    expect(
      screen.getByText(
        "Your package (empty) doesn't include web domains, DNS zones, mail domains, databases, cron jobs or backups. Ask your administrator to change it.",
      ),
    ).toBeTruthy()
    // the shell is still there
    expect(screen.getByRole('button', { name: 'carol' })).toBeTruthy()
    expect(document.body.textContent).not.toMatch(/403|Hestia/)
  })

  it('links the account pages the package features allow', async () => {
    renderApp('/error', carol())
    await screen.findByRole('heading', { name: 'Nothing to manage yet' })
    const main = group().getByRole('link', { name: 'Edit account' })
    expect(main.getAttribute('href')).toBe('/account')
    expect(group().getByRole('link', { name: 'SSH keys' }).getAttribute('href')).toBe('/account/ssh-keys')
    expect(group().getByRole('link', { name: 'Logs' }).getAttribute('href')).toBe('/logs')
    expect(group().getByRole('link', { name: 'Notifications' }).getAttribute('href')).toBe('/notifications')
    for (const name of ['Access keys', 'Web terminal', 'File manager']) {
      expect(group().queryByRole('link', { name })).toBeNull()
    }
  })

  it('adds Access keys, Web terminal and File manager when the features are on', async () => {
    renderApp('/error', carol({ ...none, accessKeys: true, terminal: true, fileManager: true, logs: false }))
    await screen.findByRole('heading', { name: 'Nothing to manage yet' })
    expect(group().getByRole('link', { name: 'Access keys' }).getAttribute('href')).toBe('/account/access-keys')
    expect(group().getByRole('link', { name: 'Web terminal' }).getAttribute('href')).toBe('/terminal')
    expect(group().getByRole('link', { name: 'File manager' }).getAttribute('href')).toBe('/files')
    expect(group().queryByRole('link', { name: 'Logs' })).toBeNull()
  })

  it('logs out', async () => {
    const { calls } = renderApp('/error', carol())
    await screen.findByRole('heading', { name: 'Nothing to manage yet' })
    fireEvent.click(group().getByRole('button', { name: 'Log out' }))
    await waitFor(() => expect(calls.some((c) => c.key === 'POST /auth/logout')).toBe(true))
  })
})

describe('old Hestia URLs', () => {
  it('sends a signed-in /login/ to the landing page', async () => {
    const { location } = renderApp('/login/', toolsRoutes())
    await waitFor(() => expect(location()).toBe('/users'))
  })

  it('sends /list/user/ through / to the landing page', async () => {
    const { location } = renderApp('/list/user/', toolsRoutes())
    await waitFor(() => expect(location()).toBe('/users'))
  })
})
