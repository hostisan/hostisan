import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { devUsers } from '../../test/accountFixtures'
import source from './accounts.ts?raw'
import { dnsRoutes, renderApp } from '../../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const every = {
  web: true, dns: true, db: true, mail: true, cron: true, backup: true, incremental: true, logs: true,
  accessKeys: true, server: true, firewall: true, firewallBans: true,
}

async function openPalette(user: Record<string, unknown>, extra = {}) {
  const routes = {
    ...dnsRoutes(user),
    'GET /users': () => ({ body: { users: devUsers, rootUser: 'admin', self: 'admin' } }),
    ...extra,
  }
  const app = renderApp('/dns', routes)
  await screen.findByText('Hosting')
  fireEvent.keyDown(window, { key: 'k', ctrlKey: true })
  const input = await screen.findByPlaceholderText(/Search pages, domains/)
  await screen.findByText('Add DNS zone')
  return { ...app, input }
}

describe('account commands in the palette', () => {
  it('builds the users group from the users API, not from mock data', async () => {
    const { calls, input } = await openPalette({ features: every })
    const dialog = screen.getByRole('dialog')
    // the palette lists resources once a query is typed
    for (const u of ['bob', 'susp', 'ops']) {
      fireEvent.change(input, { target: { value: u } })
      await waitFor(() => expect(within(dialog).getByText(u)).toBeTruthy())
    }
    expect(within(dialog).getByText('Users')).toBeTruthy()
    expect(calls.some((c) => c.key === 'GET /users')).toBe(true)
    expect(source).not.toMatch(/mocks\//)
  })

  it('opens the user from the group', async () => {
    const { location, input } = await openPalette({ features: every })
    fireEvent.change(input, { target: { value: 'susp' } })
    const dialog = screen.getByRole('dialog')
    fireEvent.click(await within(dialog).findByText('susp'))
    await waitFor(() => expect(location()).toBe('/users/susp'))
  })

  it('does not ask for users while the palette is closed, or for a non-admin', async () => {
    const closed = renderApp('/dns', {
      ...dnsRoutes({ features: every }),
      'GET /users': () => ({ body: { users: devUsers, rootUser: 'admin', self: 'admin' } }),
    })
    await screen.findByText('Hosting')
    expect(closed.calls.some((c) => c.key === 'GET /users')).toBe(false)
    cleanup()
    const { calls } = await openPalette({ features: every, role: 'user', landing: '/dns' })
    expect(calls.some((c) => c.key === 'GET /users')).toBe(false)
    expect(screen.queryByText('Users')).toBeNull()
  })

  it('leaves the other sections untouched', async () => {
    await openPalette({ features: every })
    expect(screen.getByText('Add web domain')).toBeTruthy()
    expect(screen.getByText('Add cron job')).toBeTruthy()
    expect(screen.getByText('Add user')).toBeTruthy()
    expect(screen.getByText('Create backup')).toBeTruthy()
    expect(screen.getByText('Edit account')).toBeTruthy()
  })

  it('opens your own entry on the account page and leaves root out for a non-root admin', async () => {
    const asOps = { 'GET /users': () => ({ body: { users: devUsers, rootUser: 'admin', self: 'ops' } }) }
    const { input, location } = await openPalette({ name: 'ops', features: every }, asOps)
    const dialog = screen.getByRole('dialog')
    fireEvent.change(input, { target: { value: 'admin@' } })
    await waitFor(() => expect(within(dialog).queryByText('System Administrator')).toBeNull())
    fireEvent.change(input, { target: { value: 'ops' } })
    fireEvent.click(await within(dialog).findByText('ops'))
    // self goes to /account, not /users/ops
    await waitFor(() => expect(location()).toBe('/account'))
  })
})
