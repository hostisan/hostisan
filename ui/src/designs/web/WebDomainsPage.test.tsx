import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderApp, type Route } from '../../test/renderApp'
import { webDomain, webOptions, webRoutes } from '../../test/webFixtures'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const open = (name: string) => fireEvent.click(screen.getByRole('button', { name: `Actions for ${name}` }))
const closeMenu = async () => {
  fireEvent.keyDown(screen.getByRole('menu'), { key: 'Escape' })
  await waitFor(() => expect(screen.queryByRole('menu')).toBeNull())
}
const ok: Route = () => ({ body: { status: 'ok' } })
const row = (name: string) => screen.getByRole('link', { name }).closest('tr') as HTMLElement
const items = async () => (await screen.findAllByRole('menuitem')).map((i) => i.textContent)
const withOptions = (over: Parameters<typeof webOptions>[0], user = {}, more: Record<string, Route> = {}) =>
  webRoutes(user, { 'GET /web/options': () => ({ body: webOptions(over) }), ...more })

describe('web domains list', () => {
  it('shows the columns: alias hint without www, display IP, sizes, SSL and stats icons', async () => {
    renderApp('/web', webRoutes())
    await screen.findByRole('link', { name: 'example.com' })
    const com = row('example.com')
    expect(within(com).getByText('blog.example.com')).toBeTruthy()
    expect(within(com).queryByText(/www\.example\.com/)).toBeNull()
    expect(within(com).getByText('203.0.113.10')).toBeTruthy()
    expect(within(com).getByText('128.00 MB')).toBeTruthy()
    expect(within(com).getByText('45.00 MB')).toBeTruthy()
    expect(within(com).getByRole('img', { name: 'SSL: Enabled' })).toBeTruthy()
    expect(within(com).getByRole('img', { name: 'Statistics: Enabled' })).toBeTruthy()
    const net = row('example.net')
    expect(within(net).getByRole('img', { name: 'SSL: Disabled' })).toBeTruthy()
    expect(within(net).getByRole('img', { name: 'Statistics: Disabled' })).toBeTruthy()
    expect(within(net).getByText('Suspended')).toBeTruthy()
  })

  it('stacks the name and alias hint in a flex column, not a grid', async () => {
    // Chrome sizes a grid below its content in a crowded auto-layout table, so the hint
    // ran under the IP address column at 1280px wide
    renderApp('/web', webRoutes())
    const link = await screen.findByRole('link', { name: 'example.com' })
    const stack = link.parentElement as HTMLElement
    expect(stack.className.split(' ')).toContain('flex-col')
    expect(stack.className.split(' ')).not.toContain('grid')
  })

  it('greys the icons of a suspended domain', async () => {
    renderApp(
      '/web',
      webRoutes(
        {},
        {
          'GET /web': () => ({
            body: {
              domains: [webDomain('gone.test', { SUSPENDED: 'yes' })],
              displayIps: { '203.0.113.10': '203.0.113.10' },
              usage: { domains: '1', domainsLimit: '5', suspended: '1', aliases: '1', aliasesLimit: '15' },
            },
          }),
        },
      ),
    )
    await screen.findByRole('link', { name: 'gone.test' })
    const icons = within(row('gone.test')).getAllByRole('img')
    expect(icons.length).toBeGreaterThanOrEqual(2)
    for (const icon of icons) expect(icon.innerHTML).toContain('text-kumo-inactive')
  })

  it("shows a NAT address instead of the domain's internal IP", async () => {
    renderApp('/web', webRoutes({ name: 'bob', role: 'user' }))
    await screen.findByRole('link', { name: 'bakery.example' })
    expect(within(row('bakery.example')).getByText('198.51.100.7')).toBeTruthy()
    expect(screen.queryByText('10.0.0.5')).toBeNull()
  })

  it('footer counts the domains with the singular for one', async () => {
    renderApp('/web', webRoutes())
    await screen.findByRole('link', { name: 'example.com' })
    expect(screen.getByText('2 web domains')).toBeTruthy()
    cleanup()
    renderApp('/web', webRoutes({ name: 'bob', role: 'user' }))
    await screen.findByRole('link', { name: 'bakery.example' })
    expect(screen.getByText('1 web domain')).toBeTruthy()
  })

  it('shows an error when the list fails', async () => {
    renderApp(
      '/web',
      webRoutes({}, { 'GET /web': () => ({ status: 500, body: { error: { code: 'server_error', message: 'Error: boom' } } }) }),
    )
    expect(await screen.findByText('Error: boom')).toBeTruthy()
  })

  it('opens by name for sortOrder name, newest first for date', async () => {
    const order = () => {
      const rows = screen.getAllByRole('row').map((r) => r.textContent ?? '')
      return rows.findIndex((r) => r.includes('example.net')) - rows.findIndex((r) => r.includes('example.com'))
    }
    renderApp('/web', webRoutes({ sortOrder: 'name' }))
    await screen.findByRole('link', { name: 'example.com' })
    expect(order()).toBeGreaterThan(0)
    cleanup()
    renderApp('/web', webRoutes({ sortOrder: 'date' }))
    await screen.findByRole('link', { name: 'example.com' })
    expect(order()).toBeLessThan(0)
  })

  it('shows usage, SSL and Let\'s Encrypt counts, disk and suspended in the aside', async () => {
    renderApp('/web', webRoutes({ name: 'bob', role: 'user' }))
    await screen.findByRole('link', { name: 'bakery.example' })
    expect(await screen.findByText('1 / 3')).toBeTruthy()
    expect(screen.getByText('1 / 15')).toBeTruthy()
    const tile = (label: string) => screen.getByText(label).closest('div')?.parentElement?.textContent
    expect(tile('SSL enabled')).toMatch(/SSL enabled1$/)
    expect(tile("Let's Encrypt")).toMatch(/Let's Encrypt0$/)
    expect(tile('Disk used')).toMatch(/Disk used312.00 MB$/)
    expect(tile('Suspended')).toMatch(/Suspended0$/)
  })
})

describe('row actions', () => {
  it('offers the full set on an active admin domain', async () => {
    const app = renderApp('/web', withOptions({}, {}))
    await screen.findByRole('link', { name: 'example.com' })
    open('example.com')
    const names = await items()
    expect(names).toEqual([
      'Statistics',
      'Visit',
      'Edit domain',
      'Download site',
      'View logs',
      'SSL certificate',
      'Quick install app',
      'Suspend',
      'Delete',
    ])
    expect(screen.getByRole('menuitem', { name: 'Statistics' }).getAttribute('href')).toBe(
      'https://example.com/vstats/',
    )
    expect(screen.getByRole('menuitem', { name: 'Visit' }).getAttribute('href')).toBe('http://example.com/')
    const download = screen.getByRole('menuitem', { name: 'Download site' })
    expect(download.getAttribute('href')).toBe('/api/v1/web/example.com/download')
    // a download, not a route of the panel
    fireEvent.click(download)
    expect(app.location()).toBe('/web')
  })

  it('hides Edit and Download site on a suspended domain, offering Unsuspend', async () => {
    renderApp('/web', webRoutes())
    await screen.findByRole('link', { name: 'example.net' })
    open('example.net')
    const names = await items()
    expect(names).toContain('Unsuspend')
    expect(names).toContain('View logs')
    expect(names).not.toContain('Edit domain')
    expect(names).not.toContain('Download site')
    expect(names).not.toContain('SSL certificate')
    expect(names).not.toContain('Statistics')
  })

  it('hides Quick install when the installer is off', async () => {
    renderApp('/web', withOptions({ appInstaller: false }))
    await screen.findByRole('link', { name: 'example.com' })
    open('example.com')
    expect(await items()).not.toContain('Quick install app')
  })

  it('offers Purge only for a caching domain on a server that purges', async () => {
    const caching = (purge: boolean) =>
      withOptions({ purge }, {}, {
        'GET /web': () => ({
          body: {
            domains: [webDomain('cache.test', { PROXY: 'caching' }), webDomain('plain.test')],
            displayIps: { '203.0.113.10': '203.0.113.10' },
            usage: { domains: '2', domainsLimit: '5', suspended: '0', aliases: '2', aliasesLimit: '15' },
          },
        }),
      })
    renderApp('/web', caching(true))
    await screen.findByRole('link', { name: 'cache.test' })
    open('cache.test')
    expect(await items()).toContain('Purge NGINX cache')
    await closeMenu()
    open('plain.test')
    expect(await items()).not.toContain('Purge NGINX cache')
    cleanup()
    renderApp('/web', caching(false))
    await screen.findByRole('link', { name: 'cache.test' })
    open('cache.test')
    expect(await items()).not.toContain('Purge NGINX cache')
  })

  it('offers Purge for a domain with no proxy template on an nginx-only server', async () => {
    renderApp(
      '/web',
      withOptions({ nginx: true, purge: true }, {}, {
        'GET /web': () => ({
          body: {
            domains: [webDomain('fast.test', { PROXY: '' })],
            displayIps: { '203.0.113.10': '203.0.113.10' },
            usage: { domains: '1', domainsLimit: '5', suspended: '0', aliases: '1', aliasesLimit: '15' },
          },
        }),
      }),
    )
    await screen.findByRole('link', { name: 'fast.test' })
    open('fast.test')
    expect(await items()).toContain('Purge NGINX cache')
  })

  it('hides Add web domain from a read-only user', async () => {
    renderApp('/web', webRoutes({ readOnly: true }))
    await screen.findByText('example.com')
    expect(screen.queryByRole('link', { name: 'Add web domain' })).toBeNull()
  })

  it('purges the cache through the API', async () => {
    const app = renderApp(
      '/web',
      withOptions({}, {}, {
        'GET /web': () => ({
          body: {
            domains: [webDomain('cache.test', { PROXY: 'caching-ext' })],
            displayIps: { '203.0.113.10': '203.0.113.10' },
            usage: { domains: '1', domainsLimit: '5', suspended: '0', aliases: '1', aliasesLimit: '15' },
          },
        }),
        'POST /web/cache.test/purge-cache': ok,
      }),
    )
    await screen.findByRole('link', { name: 'cache.test' })
    open('cache.test')
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Purge NGINX cache' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Purge cache' }))
    await waitFor(() => expect(app.calls.some((c) => c.key === 'POST /web/cache.test/purge-cache')).toBe(true))
    expect(await screen.findByText('NGINX cache has been purged successfully.')).toBeTruthy() // Hestia's msgid (ruling 19)
  })

  it('offers a non-admin Delete but no Suspend', async () => {
    renderApp('/web', webRoutes({ name: 'bob', role: 'user' }))
    await screen.findByRole('link', { name: 'bakery.example' })
    open('bakery.example')
    const names = await items()
    expect(names).toContain('Delete')
    expect(names).not.toContain('Suspend')
    expect(names).not.toContain('Unsuspend')
  })

  it('keeps only Statistics and Visit for a read-only user', async () => {
    renderApp('/web', webRoutes({ readOnly: true }))
    await screen.findByText('example.com')
    expect(screen.queryByRole('checkbox')).toBeNull()
    expect(screen.queryByRole('link', { name: 'example.com' })).toBeNull()
    open('example.com')
    expect(await items()).toEqual(['Statistics', 'Visit'])
  })

  it('suspends and unsuspends through the API', async () => {
    const app = renderApp(
      '/web',
      webRoutes({}, { 'POST /web/example.com/suspend': ok, 'POST /web/example.net/unsuspend': ok }),
    )
    await screen.findByRole('link', { name: 'example.com' })
    open('example.com')
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Suspend' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Suspend' }))
    await waitFor(() => expect(app.calls.some((c) => c.key === 'POST /web/example.com/suspend')).toBe(true))
    await waitFor(() => expect(app.calls.filter((c) => c.key === 'GET /web').length).toBeGreaterThan(1))
    expect(await screen.findByText('example.com suspended')).toBeTruthy()
    open('example.net')
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Unsuspend' }))
    fireEvent.click(within(await screen.findByRole('dialog', { name: 'Unsuspend web domain' })).getByRole('button', { name: 'Unsuspend' }))
    await waitFor(() => expect(app.calls.some((c) => c.key === 'POST /web/example.net/unsuspend')).toBe(true))
  })

  it('deletes after typing the name', async () => {
    const app = renderApp('/web', webRoutes({}, { 'DELETE /web/example.com': ok }))
    await screen.findByRole('link', { name: 'example.com' })
    open('example.com')
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.change(within(dialog).getByRole('textbox'), { target: { value: 'example.com' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete web domain' }))
    await waitFor(() => expect(app.calls.some((c) => c.key === 'DELETE /web/example.com')).toBe(true))
  })
})

describe('bulk', () => {
  const select = () => fireEvent.click(screen.getAllByRole('checkbox')[1])

  it('offers Rebuild only to a user who canRebuild, and Purge only when the server purges', async () => {
    renderApp('/web', withOptions({ canRebuild: false, purge: false }))
    await screen.findByRole('link', { name: 'example.com' })
    select()
    expect(await screen.findByRole('button', { name: 'Delete' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Rebuild' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Purge cache' })).toBeNull()
    cleanup()
    renderApp('/web', withOptions({ canRebuild: true, purge: true }))
    await screen.findByRole('link', { name: 'example.com' })
    select()
    expect(await screen.findByRole('button', { name: 'Rebuild' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Purge cache' })).toBeTruthy()
  })

  it('sends the action and the selected names', async () => {
    const app = renderApp('/web', withOptions({}, {}, { 'POST /web/bulk': () => ({ body: { status: 'ok', failed: [] } }) }))
    await screen.findByRole('link', { name: 'example.com' })
    select()
    fireEvent.click(await screen.findByRole('button', { name: 'Rebuild' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Rebuild' }))
    await waitFor(() => expect(app.calls.some((c) => c.key === 'POST /web/bulk')).toBe(true))
    expect(app.calls.find((c) => c.key === 'POST /web/bulk')?.body).toEqual({
      action: 'rebuild',
      names: ['example.com'],
    })
  })

  it('names the failed domain', async () => {
    renderApp(
      '/web',
      withOptions({}, {}, {
        'POST /web/bulk': () => ({
          body: { status: 'ok', failed: [{ name: 'example.com', message: 'Error: cache is gone' }] },
        }),
      }),
    )
    await screen.findByRole('link', { name: 'example.com' })
    select()
    fireEvent.click(await screen.findByRole('button', { name: 'Purge cache' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Purge cache' }))
        expect(await screen.findByText('Error: cache is gone')).toBeTruthy()
    // the toast title names the domain (the row link is the other match)
    expect(screen.getAllByText('example.com').length).toBeGreaterThan(1)
  })
})
