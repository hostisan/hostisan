import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { devPackages, packageEntry, pkg, userEntry, usersRoutes } from '../../test/accountFixtures'
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
const menu = (name: string) => fireEvent.click(screen.getByRole('button', { name: `Actions for ${name}` }))
const items = () => screen.getAllByRole('menuitem').map((i) => i.textContent)
const rowSync = (name: string) => screen.getByRole('button', { name: `Actions for ${name}` }).closest('tr') as HTMLElement

describe('packages list', () => {
  it('opens in the saved sort order (owner ruling 16)', async () => {
    const two = [pkg('alpha', { DATE: '2026-01-01' }), pkg('zulu', { DATE: '2026-09-01' })].map(packageEntry)
    const order = () =>
      screen.getAllByRole('button', { name: /^Actions for / }).map((b) => b.getAttribute('aria-label'))
    const routes = (sortOrder: string) =>
      usersRoutes({ features: full, sortOrder }, { 'GET /packages': () => ({ body: { packages: two } }) })
    renderApp('/packages', routes('name'))
    await screen.findByRole('table')
    expect(order()).toEqual(['Actions for alpha', 'Actions for zulu'])
    cleanup()
    renderApp('/packages', routes('date'))
    await screen.findByRole('table')
    expect(order()).toEqual(['Actions for zulu', 'Actions for alpha'])
  })

  it('shows limits, the infinity sign and the SSH icon', async () => {
    const unlimited = pkg('gold', { DISK_QUOTA: 'unlimited', BANDWIDTH: 'unlimited', WEB_DOMAINS: 'unlimited', SHELL: 'bash', DATABASES: '7' })
    renderApp(
      '/packages',
      usersRoutes({ features: full }, { 'GET /packages': () => ({ body: { packages: [...devPackages, unlimited].map(packageEntry) } }) }),
    )
    await screen.findByRole('table')
    const gold = within(rowSync('gold'))
    expect(gold.getAllByLabelText('Unlimited').length).toBeGreaterThanOrEqual(3) // quota, bandwidth, web domains
    expect(gold.getByText('7')).toBeTruthy()
    expect(gold.getByTitle('Web domains: Unlimited')).toBeTruthy()
    expect(within(rowSync('starter-plus')).getAllByText('4.88 GB')).toHaveLength(2) // quota and bandwidth
    expect(within(rowSync('gold')).getByRole('img', { name: 'SSH access: bash' })).toBeTruthy()
    expect(within(rowSync('starter')).getByRole('img', { name: 'SSH access: nologin' })).toBeTruthy()
  })

  it('counts the users on each package and has a plural footer', async () => {
    renderApp('/packages', usersRoutes({ features: full }))
    await screen.findByRole('table')
    // dev users: admin on system, bob and susp on default
    await waitFor(() => expect(within(rowSync('default')).getAllByRole('cell')[2].textContent).toBe('2'))
    expect(screen.getByText(/4\s+packages/)).toBeTruthy()
  })

  it('links every package but system, and marks the built-in ones', async () => {
    renderApp('/packages', usersRoutes({ features: full }))
    const table = within(await screen.findByRole('table'))
    expect(table.getByRole('link', { name: 'starter' }).getAttribute('href')).toBe('/packages/starter')
    expect(table.queryByRole('link', { name: 'system' })).toBeNull()
    expect(table.getAllByText('Built-in')).toHaveLength(2)
  })

  it('offers Duplicate for every package and no Edit or Delete for system', async () => {
    renderApp('/packages', usersRoutes({ features: full }))
    await screen.findByRole('table')
    menu('system')
    expect(items()).toEqual(['Duplicate'])
    fireEvent.keyDown(screen.getByRole('menu'), { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('menu')).toBeNull())
    menu('starter')
    expect(items()).toEqual(['Edit', 'Duplicate', 'Delete'])
  })

  it('duplicates through the server', async () => {
    const { calls } = renderApp('/packages', usersRoutes({ features: full }, { 'POST /packages/starter/copy': ok }))
    await screen.findByRole('table')
    menu('starter')
    fireEvent.click(screen.getByRole('menuitem', { name: 'Duplicate' }))
    await waitFor(() => expect(calls.some((c) => c.key === 'POST /packages/starter/copy')).toBe(true))
    expect(await screen.findByText('Package starter-copy created')).toBeTruthy()
    expect(screen.queryByText('starter-copy created')).toBeNull()
  })

  it('confirms with the Hestia text before deleting', async () => {
    const { calls } = renderApp('/packages', usersRoutes({ features: full }, { 'DELETE /packages/starter': ok }))
    await screen.findByRole('table')
    menu('starter')
    fireEvent.click(screen.getByRole('menuitem', { name: 'Delete' }))
    const dialog = within(await screen.findByRole('dialog'))
    expect(dialog.getByText('Are you sure you want to delete package starter?')).toBeTruthy()
    fireEvent.click(dialog.getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(calls.some((c) => c.key === 'DELETE /packages/starter')).toBe(true))
  })

  it('names both packages when a delete is refused as in use', async () => {
    renderApp(
      '/packages',
      usersRoutes(
        { features: full },
        {
          'DELETE /packages/starter': () => ({
            status: 409,
            body: {
              error: {
                code: 'in_use',
                message: "Package %s can't be deleted while package %s has users.",
                args: ['starter', 'starter-plus'],
              },
            },
          }),
        },
      ),
    )
    await screen.findByRole('table')
    menu('starter')
    fireEvent.click(screen.getByRole('menuitem', { name: 'Delete' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Delete' }))
    expect(await screen.findByText("Package starter can't be deleted while package starter-plus has users.")).toBeTruthy()
  })

  it('bulk-deletes the selection without the built-in packages', async () => {
    const { calls } = renderApp(
      '/packages',
      usersRoutes({ features: full }, { 'POST /packages/bulk': () => ({ body: { status: 'ok', failed: [] } }) }),
    )
    await screen.findByRole('table')
    for (const n of ['default', 'starter', 'starter-plus']) fireEvent.click(within(rowSync(n)).getByRole('checkbox'))
    fireEvent.click(await screen.findByRole('button', { name: 'Delete' }))
    const dialog = within(await screen.findByRole('dialog'))
    expect(dialog.getByText('Are you sure you want to delete 2 packages?')).toBeTruthy()
    fireEvent.click(dialog.getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(calls.some((c) => c.key === 'POST /packages/bulk')).toBe(true))
    await waitFor(() => expect(calls.filter((c) => c.key === 'POST /packages/bulk')).toHaveLength(2))
    // one package per request, like users
    expect(calls.filter((c) => c.key === 'POST /packages/bulk').map((c) => c.body)).toEqual([
      { action: 'delete', names: ['starter'] },
      { action: 'delete', names: ['starter-plus'] },
    ])
  })

  it('uses the singular for one package', async () => {
    renderApp('/packages', usersRoutes({ features: full }, { 'GET /users': () => ({ body: { users: [userEntry('bob')], rootUser: 'admin', self: 'admin' } }) }))
    await screen.findByRole('table')
    fireEvent.change(screen.getByPlaceholderText('Search packages'), { target: { value: 'starter-plus' } })
    await waitFor(() => expect(screen.getByText(/1 \(of 4\) package$/)).toBeTruthy())
  })

  it('shows progress while packages are deleted one at a time', async () => {
    let release: () => void = () => {}
    const gate = new Promise<void>((r) => (release = r))
    let n = 0
    renderApp('/packages', usersRoutes({ features: full }, { 'POST /packages/bulk': () => ({ body: { status: 'ok', failed: [] } }) }))
    const real = globalThis.fetch
    globalThis.fetch = (async (url: string, init?: RequestInit) => {
      if (String(url).endsWith('/packages/bulk') && init?.method === 'POST' && ++n === 2) await gate
      return real(url, init)
    }) as typeof fetch
    await screen.findByRole('table')
    for (const p of ['starter', 'starter-plus']) fireEvent.click(within(rowSync(p)).getByRole('checkbox'))
    fireEvent.click(await screen.findByRole('button', { name: 'Delete' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Delete' }))
    expect(await screen.findByText(/Delete: 1 of 2/)).toBeTruthy()
    release()
    await waitFor(() => expect(screen.queryByText(/Delete: \d of 2/)).toBeNull())
  })

  it('shows the error state when the list fails', async () => {
    renderApp(
      '/packages',
      usersRoutes({ features: full }, { 'GET /packages': () => ({ status: 500, body: { error: { code: 'server_error', message: 'Error: packages down' } } }) }),
    )
    expect(await screen.findByText('Error: packages down')).toBeTruthy()
  })
})
