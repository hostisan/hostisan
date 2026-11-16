import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { keysRoutes, usersRoutes } from '../../test/accountFixtures'
import { renderApp, signedIn } from '../../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const ok = () => ({ body: { status: 'ok' } })
const routes = (user: Record<string, unknown> = {}, over: Parameters<typeof keysRoutes>[1] = {}) => ({
  ...usersRoutes(user),
  ...keysRoutes(user, over),
})
const asUser = { role: 'user', landing: '/web' }

async function pick(option: string) {
  fireEvent.click(screen.getByRole('combobox', { name: 'Show logs for user' }))
  const opt = await screen.findByRole('option', { name: option })
  fireEvent.pointerDown(opt)
  fireEvent.mouseDown(opt)
  fireEvent.pointerUp(opt)
  fireEvent.mouseUp(opt)
  fireEvent.click(opt)
  await waitFor(() => expect(screen.queryByRole('option')).toBeNull())
}

describe('logs page', () => {
  it('shows the log in the API order and counts it', async () => {
    renderApp('/logs', routes())
    expect(await screen.findByText('Backup upload to SFTP host failed.')).toBeTruthy()
    const rows = screen.getAllByRole('row').map((r) => r.textContent ?? '')
    expect(rows.findIndex((r) => r.includes('Backup upload'))).toBeLessThan(rows.findIndex((r) => r.includes('Added new web domain')))
    expect(screen.getByText(/5 log entries/)).toBeTruthy()
  })

  it('shows Info rows with a dot badge kumo accepts', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    renderApp('/logs', routes())
    expect(await screen.findByText('Added new web domain', { exact: false })).toBeTruthy()
    expect(screen.getAllByText('Information').length).toBeGreaterThan(0)
    expect(warn.mock.calls.filter((c) => String(c[0]).includes('[kumo]'))).toEqual([])
    warn.mockRestore()
  })

  it('shows the error state instead of a made-up row', async () => {
    renderApp(
      '/logs',
      routes({}, { 'GET /logs': () => ({ status: 500, body: { error: { code: 'x', message: 'Error: boom' } } }) }),
    )
    expect(await screen.findByText('Error: boom')).toBeTruthy()
    expect(screen.queryByText('Unable to load logs')).toBeNull()
  })

  it('opens the system log at /logs?user=system for an admin', async () => {
    const app = renderApp('/logs?user=system', routes())
    expect(await screen.findByText('Banned IP address 198.51.100.23 (ssh).')).toBeTruthy()
    expect(app.calls.some((c) => c.key === 'GET /logs?user=system')).toBe(true)
    expect(app.calls.some((c) => c.key === 'GET /logs')).toBe(false)
  })

  it('switches users from the admin select', async () => {
    const app = renderApp('/logs', routes())
    await screen.findByText('Backup upload to SFTP host failed.')
    await pick('System log')
    await waitFor(() => expect(app.calls.some((c) => c.key === 'GET /logs?user=system')).toBe(true))
    expect(await screen.findByText('Banned IP address 198.51.100.23 (ssh).')).toBeTruthy()
    await pick('User: bob')
    await waitFor(() => expect(app.calls.some((c) => c.key === 'GET /logs?user=bob')).toBe(true))
  })

  it('does not list users while logged in as another user (owner ruling 12)', async () => {
    const app = renderApp('/logs', routes({ look: 'bob' }))
    await screen.findByText('Backup upload to SFTP host failed.')
    expect(app.calls.some((c) => c.key === 'GET /users')).toBe(false)
  })

  it('has no user switcher for a regular user and never sends ?user=', async () => {
    const app = renderApp('/logs?user=system', routes(asUser))
    await screen.findByText('Backup upload to SFTP host failed.')
    expect(screen.queryByRole('combobox', { name: 'Show logs for user' })).toBeNull()
    expect(app.calls.some((c) => c.key.includes('user='))).toBe(false)
  })

  it('deletes the system log after confirming', async () => {
    const app = renderApp('/logs?user=system', routes({}, { 'DELETE /logs?user=system': ok }))
    await screen.findByText('Banned IP address 198.51.100.23 (ssh).')
    fireEvent.click(screen.getByRole('button', { name: 'Delete logs' }))
    const dialog = await screen.findByRole('dialog', { name: 'Delete logs' })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete logs' }))
    await waitFor(() => expect(app.calls.some((c) => c.key === 'DELETE /logs?user=system')).toBe(true))
  })

  it('hides delete without the permission, and for the read-only session', async () => {
    renderApp('/logs', routes({ ...asUser, canDeleteLogs: false }))
    await screen.findByText('Backup upload to SFTP host failed.')
    expect(screen.queryByRole('button', { name: 'Delete logs' })).toBeNull()
    cleanup()
    renderApp('/logs', routes({ ...asUser, readOnly: true }))
    await screen.findByText('Backup upload to SFTP host failed.')
    expect(screen.queryByRole('button', { name: 'Delete logs' })).toBeNull()
  })
})

describe('logs page, other cases', () => {
  it('treats an empty ?user= as the own log', async () => {
    const app = renderApp('/logs?user=', routes())
    await screen.findByText('Backup upload to SFTP host failed.')
    expect(app.calls.some((c) => c.key === 'GET /logs')).toBe(true)
    expect(app.calls.some((c) => c.key.includes('user='))).toBe(false)
  })

  it('offers the user switcher on phones too', async () => {
    renderApp('/logs', routes())
    await screen.findByText('Backup upload to SFTP host failed.')
    const select = screen.getByRole('combobox', { name: 'Show logs for user' })
    expect(select.className).not.toMatch(/max-md:hidden/)
  })

  it('hides Login history for the protected admin', async () => {
    renderApp('/logs', routes({ readOnly: true, look: 'admin' }))
    await screen.findByText('Backup upload to SFTP host failed.')
    expect(screen.queryByRole('link', { name: 'Login history' })).toBeNull()
  })

  it('hides Login history in demo mode', async () => {
    const features = { ...(signedIn({}).user.features as Record<string, boolean>), loginHistory: false }
    renderApp('/logs', routes({ features }))
    await screen.findByText('Backup upload to SFTP host failed.')
    expect(screen.queryByRole('link', { name: 'Login history' })).toBeNull()
  })

  it('links Login history otherwise', async () => {
    renderApp('/logs', routes())
    expect(await screen.findByRole('link', { name: 'Login history' })).toBeTruthy()
  })
})

describe('login history', () => {
  it('lists the attempts without a session column', async () => {
    renderApp('/logs/auth', routes())
    expect(await screen.findByText('198.51.100.23')).toBeTruthy()
    expect(screen.getAllByText('Failed').length).toBeGreaterThan(0)
    expect(screen.queryByText('Session')).toBeNull()
  })

  it('deletes the history after confirming', async () => {
    const app = renderApp('/logs/auth', routes({}, { 'DELETE /logs/auth': ok }))
    await screen.findByText('198.51.100.23')
    fireEvent.click(screen.getByRole('button', { name: 'Delete logs' }))
    const dialog = await screen.findByRole('dialog', { name: 'Delete logs' })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete logs' }))
    await waitFor(() => expect(app.calls.some((c) => c.key === 'DELETE /logs/auth')).toBe(true))
  })

  it('shows the forbidden state for the protected admin', async () => {
    renderApp(
      '/logs/auth',
      routes({}, { 'GET /logs/auth': () => ({ status: 403, body: { error: { code: 'forbidden', message: '' } } }) }),
    )
    expect(await screen.findByText(/You don't have permission to view this page/)).toBeTruthy()
  })
})