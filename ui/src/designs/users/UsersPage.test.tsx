import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { devUsers, usersRoutes } from '../../test/accountFixtures'
import { renderApp, type Route } from '../../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const full = {
  web: true, dns: true, db: true, mail: true, cron: true, backup: true, incremental: true, logs: true,
  accessKeys: true, server: true, firewall: true, firewallBans: true,
}
const ok: Route = () => ({ body: { status: 'ok' } })
const opsList = { users: devUsers, rootUser: 'admin', self: 'ops' }

const menu = (name: string) => fireEvent.click(screen.getByRole('button', { name: `Actions for ${name}` }))
const rowOf = async (name: string) => (await screen.findByRole('button', { name: `Actions for ${name}` })).closest('tr') as HTMLElement
const rowSync = (name: string) => screen.getByRole('button', { name: `Actions for ${name}` }).closest('tr') as HTMLElement
const items = () => screen.getAllByRole('menuitem').map((i) => i.textContent)
const confirmDialog = async () => within(await screen.findByRole('dialog'))

describe('users list', () => {
  it('stacks the username and contact line in a flex column, not a grid', async () => {
    // Chrome sizes a grid below its content in a crowded auto-layout table, so the
    // contact line ran under the Package and Disk columns at 1280px wide
    renderApp('/users', usersRoutes())
    const name = await screen.findByRole('button', { name: 'bob' })
    const stack = name.parentElement!.parentElement as HTMLElement
    expect(stack.className.split(' ')).toContain('flex-col')
    expect(stack.className.split(' ')).not.toContain('grid')
  })

  it('lists every account with its columns and a footer count', async () => {
    renderApp('/users', usersRoutes({ features: full }))
    await screen.findByRole('table')
    for (const h of ['User', 'Package', 'Disk', 'Bandwidth', 'Status']) {
      expect(screen.getByRole('columnheader', { name: new RegExp(h) })).toBeTruthy()
    }
    for (const u of ['admin', 'bob', 'susp', 'ops']) expect(screen.getByRole('button', { name: `Actions for ${u}` })).toBeTruthy()
    expect(screen.getByText(/4\s+user accounts/)).toBeTruthy()
    // system is plain text, other packages link to their form
    const bob = await rowOf('bob')
    expect(within(bob).getByRole('link', { name: 'default' }).getAttribute('href')).toBe('/packages/default')
    expect(within(await rowOf('admin')).queryByRole('link', { name: 'system' })).toBeNull()
    expect(within(await rowOf('susp')).getByText('Suspended')).toBeTruthy()
  })

  it('uses the singular in the footer for one match', async () => {
    renderApp('/users', usersRoutes({ features: full }))
    await screen.findByRole('table')
    fireEvent.change(screen.getByPlaceholderText(/Search usernames/), { target: { value: 'susp' } })
    await waitFor(() => expect(screen.getByText(/1 \(of 4\) user account$/)).toBeTruthy())
  })

  it('sorts from the sort menu', async () => {
    renderApp('/users', usersRoutes({ features: full }))
    await screen.findByRole('table')
    fireEvent.click(screen.getByRole('button', { name: /Sort by/ }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Disk' }))
    await waitFor(() => expect(screen.getByRole('button', { name: /Sort by: Disk/ })).toBeTruthy())
    const order = screen.getAllByRole('button', { name: /^Actions for / }).map((b) => b.getAttribute('aria-label'))
    // ascending disk: the 512 MB users come before admin's 7342 MB
    expect(order[order.length - 1]).toBe('Actions for admin')
  })

  it('opens newest first when the admin sorts by date', async () => {
    renderApp('/users', usersRoutes({ features: full, sortOrder: 'date' }))
    await screen.findByRole('table')
    expect(screen.getByRole('button', { name: /Sort by: Date/ })).toBeTruthy()
  })

  it('sends your own name to the account page and logs in as the others', async () => {
    const { calls, location } = renderApp(
      '/users',
      usersRoutes({ features: full }, { 'POST /auth/login-as': () => ({ body: { status: 'ok', landing: '/web' } }) }),
    )
    const table = within(await screen.findByRole('table'))
    expect(table.getByRole('link', { name: 'admin' }).getAttribute('href')).toBe('/account')
    expect(table.queryByRole('link', { name: 'bob' })).toBeNull()
    fireEvent.click(table.getByRole('button', { name: 'bob' }))
    await waitFor(() => expect(location()).toBe('/web'))
    expect(calls.find((c) => c.key === 'POST /auth/login-as')?.body).toEqual({ user: 'bob' })
  })

  it('offers Log in as in the row menu for others only', async () => {
    renderApp('/users', usersRoutes({ features: full }))
    await screen.findByRole('table')
    menu('admin')
    expect(items()).not.toContain('Log in as admin')
    fireEvent.keyDown(screen.getByRole('menu'), { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('menu')).toBeNull())
    menu('bob')
    expect(items()).toContain('Log in as bob')
  })

  it('hides Edit for the root account unless you are root', async () => {
    renderApp('/users', usersRoutes({ features: full }))
    await screen.findByRole('table')
    menu('admin')
    expect(items()).toEqual(['Edit'])
    cleanup()
    renderApp('/users', usersRoutes({ name: 'ops', features: full }, { 'GET /users': () => ({ body: opsList }) }))
    await screen.findByRole('table')
    menu('admin')
    expect(items()).toEqual(['Log in as admin'])
  })

  it('hides suspend and delete for yourself and the root account', async () => {
    renderApp('/users', usersRoutes({ name: 'ops', features: full }, { 'GET /users': () => ({ body: opsList }) }))
    await screen.findByRole('table')
    menu('ops')
    expect(items()).toEqual(['Edit'])
    fireEvent.keyDown(screen.getByRole('menu'), { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('menu')).toBeNull())
    menu('admin')
    expect(items()).not.toContain('Suspend')
    expect(items()).not.toContain('Delete')
    fireEvent.keyDown(screen.getByRole('menu'), { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('menu')).toBeNull())
    menu('susp')
    expect(items()).toEqual(['Log in as susp', 'Edit', 'Unsuspend', 'Delete'])
  })

  it.each([
    ['Suspend', 'bob', 'suspend', 'POST /users/bob/suspend'],
    ['Unsuspend', 'susp', 'unsuspend', 'POST /users/susp/unsuspend'],
    ['Delete', 'bob', 'delete', 'DELETE /users/bob'],
  ])('asks before %s and then runs it', async (label, user, verb, key) => {
    const { calls } = renderApp('/users', usersRoutes({ features: full }, { [key]: ok }))
    await screen.findByRole('table')
    menu(user)
    fireEvent.click(screen.getByRole('menuitem', { name: label }))
    const dialog = await confirmDialog()
    expect(dialog.getByText(`Are you sure you want to ${verb} user ${user}?`)).toBeTruthy()
    fireEvent.click(dialog.getByRole('button', { name: label }))
    await waitFor(() => expect(calls.some((c) => c.key === key)).toBe(true))
  })

  it('suspends the selection one user at a time with progress and names failures', async () => {
    let n = 0
    const { calls } = renderApp(
      '/users',
      usersRoutes(
        { features: full },
        {
          'POST /users/bulk': () => {
            n++
            return {
              body: { status: 'ok', failed: n === 2 ? [{ name: 'susp', message: 'Error: user susp is suspended' }] : [] },
            }
          },
        },
      ),
    )
    await screen.findByRole('table')
    for (const u of ['admin', 'bob', 'susp']) fireEvent.click(within(rowSync(u)).getByRole('checkbox'))
    fireEvent.click(await screen.findByRole('button', { name: 'Suspend' }))
    const dialog = await confirmDialog()
    // the signed-in admin is left out
    expect(dialog.getByText('Are you sure you want to suspend 2 users?')).toBeTruthy()
    fireEvent.click(dialog.getByRole('button', { name: 'Suspend' }))
    await waitFor(() => expect(calls.filter((c) => c.key === 'POST /users/bulk')).toHaveLength(2))
    expect(calls.filter((c) => c.key === 'POST /users/bulk').map((c) => c.body)).toEqual([
      { action: 'suspend', names: ['bob'] },
      { action: 'suspend', names: ['susp'] },
    ])
    expect(await screen.findByText('Error: user susp is suspended')).toBeTruthy()
  })

  it('updates counters for the whole selection in one request', async () => {
    const { calls } = renderApp(
      '/users',
      usersRoutes({ features: full }, { 'POST /users/bulk': () => ({ body: { status: 'ok', failed: [] } }) }),
    )
    await screen.findByRole('table')
    for (const u of ['bob', 'susp', 'ops']) fireEvent.click(within(rowSync(u)).getByRole('checkbox'))
    fireEvent.click(await screen.findByRole('button', { name: 'Rebuild' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Update usage counters' }))
    await waitFor(() => expect(calls.some((c) => c.key === 'POST /users/bulk')).toBe(true))
    expect(calls.filter((c) => c.key === 'POST /users/bulk').map((c) => c.body)).toEqual([
      { action: 'update-counters', names: ['bob', 'ops', 'susp'] },
    ])
  })

  it('rebuilds one user per request', async () => {
    const { calls } = renderApp(
      '/users',
      usersRoutes({ features: full }, { 'POST /users/bulk': () => ({ body: { status: 'ok', failed: [] } }) }),
    )
    await screen.findByRole('table')
    for (const u of ['bob', 'ops']) fireEvent.click(within(rowSync(u)).getByRole('checkbox'))
    fireEvent.click(await screen.findByRole('button', { name: 'Rebuild' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Rebuild web domains' }))
    await waitFor(() => expect(calls.filter((c) => c.key === 'POST /users/bulk')).toHaveLength(2))
    expect(calls.filter((c) => c.key === 'POST /users/bulk').map((c) => c.body)).toEqual([
      { action: 'rebuild-web', names: ['bob'] },
      { action: 'rebuild-web', names: ['ops'] },
    ])
  })

  it('shows the error when the list fails', async () => {
    renderApp(
      '/users',
      usersRoutes({ features: full }, { 'GET /users': () => ({ status: 500, body: { error: { code: 'server_error', message: 'Error: boom' } } }) }),
    )
    expect(await screen.findByText('Error: boom')).toBeTruthy()
  })

  it('has no selection or add button when read-only', async () => {
    renderApp('/users', usersRoutes({ features: full, readOnly: true }))
    await screen.findByRole('table')
    expect(screen.queryAllByRole('checkbox')).toHaveLength(0)
    expect(screen.queryByRole('link', { name: 'Add user' })).toBeNull()
    menu('bob')
    expect(items()).toEqual(['Log in as bob', 'Edit'])
  })

  it('routes a row click: self to the account page, root not at all for a non-root admin', async () => {
    const { location } = renderApp('/users', usersRoutes({ name: 'ops', features: full }, { 'GET /users': () => ({ body: opsList }) }))
    await screen.findByRole('table')
    fireEvent.click(within(rowSync('admin')).getAllByRole('cell')[3])
    expect(location()).toBe('/users')
    fireEvent.click(within(rowSync('ops')).getAllByRole('cell')[3])
    await waitFor(() => expect(location()).toBe('/account'))
  })

  it('opens another user from a row click', async () => {
    const { location } = renderApp('/users', usersRoutes({ features: full }))
    await screen.findByRole('table')
    fireEvent.click(within(rowSync('susp')).getAllByRole('cell')[3])
    await waitFor(() => expect(location()).toBe('/users/susp'))
  })

  it('shows "Suspend: 1 of 2" while the second request is still running', async () => {
    let release: () => void = () => {}
    const gate = new Promise<void>((r) => (release = r))
    let n = 0
    const { calls } = renderApp(
      '/users',
      usersRoutes({ features: full }, { 'POST /users/bulk': () => ({ body: { status: 'ok', failed: [] } }) }),
    )
    // The fake API answers at once, so hold the second bulk reply at the fetch layer.
    const real = globalThis.fetch
    globalThis.fetch = (async (url: string, init?: RequestInit) => {
      if (String(url).endsWith('/users/bulk') && init?.method === 'POST' && ++n === 2) await gate
      return real(url, init)
    }) as typeof fetch
    await screen.findByRole('table')
    for (const u of ['bob', 'susp']) fireEvent.click(within(rowSync(u)).getByRole('checkbox'))
    fireEvent.click(await screen.findByRole('button', { name: 'Suspend' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Suspend' }))
    expect(await screen.findByText(/Suspend: 1 of 2/)).toBeTruthy()
    release()
    await waitFor(() => expect(screen.queryByText(/Suspend: \d of 2/)).toBeNull())
    expect(calls.filter((c) => c.key === 'POST /users/bulk')).toHaveLength(2)
  })

  it('toasts every failure a rebuild reports', async () => {
    renderApp(
      '/users',
      usersRoutes(
        { features: full },
        { 'POST /users/bulk': () => ({ body: { status: 'ok', failed: [{ name: 'bob', message: 'Error: rebuild exploded' }] } }) },
      ),
    )
    await screen.findByRole('table')
    fireEvent.click(within(rowSync('bob')).getByRole('checkbox'))
    fireEvent.click(await screen.findByRole('button', { name: 'Rebuild' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Rebuild databases' }))
    expect(await screen.findByText('Error: rebuild exploded')).toBeTruthy()
  })

  it('names the failure when a batch request itself fails', async () => {
    renderApp(
      '/users',
      usersRoutes(
        { features: full },
        { 'POST /users/bulk': () => ({ status: 500, body: { error: { code: 'server_error', message: 'Error: batch down' } } }) },
      ),
    )
    await screen.findByRole('table')
    fireEvent.click(within(rowSync('bob')).getByRole('checkbox'))
    fireEvent.click(await screen.findByRole('button', { name: 'Rebuild' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Rebuild all' }))
    expect(await screen.findByText('Error: batch down')).toBeTruthy()
  })

  it('leaves root out of a bulk action when root is not you, and says so if nothing is left', async () => {
    const { calls } = renderApp(
      '/users',
      usersRoutes({ name: 'ops', features: full }, { 'GET /users': () => ({ body: opsList }), 'POST /users/bulk': () => ({ body: { status: 'ok', failed: [] } }) }),
    )
    await screen.findByRole('table')
    fireEvent.click(within(rowSync('admin')).getByRole('checkbox'))
    fireEvent.click(await screen.findByRole('button', { name: 'Delete' }))
    expect(await screen.findByText('The signed-in admin and the root account can’t be suspended or deleted.')).toBeTruthy()
    expect(screen.queryByText(/Are you sure you want to delete/)).toBeNull()
    fireEvent.click(within(rowSync('bob')).getByRole('checkbox'))
    fireEvent.click(await screen.findByRole('button', { name: 'Delete' }))
    // the earlier error toast is also a dialog, so find the confirm by its text
    const dialog = within((await screen.findByText('Are you sure you want to delete user bob?')).closest('[role="dialog"]') as HTMLElement)
    fireEvent.click(dialog.getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(calls.some((c) => c.key === 'POST /users/bulk')).toBe(true))
    expect(calls.find((c) => c.key === 'POST /users/bulk')?.body).toEqual({ action: 'delete', names: ['bob'] })
  })

  it('selects the rows between two clicks on shift-click', async () => {
    renderApp('/users', usersRoutes({ features: full }))
    await screen.findByRole('table')
    const boxes = () => screen.getAllByRole('checkbox').slice(1)
    fireEvent.click(boxes()[0])
    fireEvent.click(boxes()[3], { shiftKey: true })
    await waitFor(() => expect(screen.getByText('4 selected')).toBeTruthy())
  })
})
