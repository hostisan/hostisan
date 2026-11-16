import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderApp, signedIn } from '../../test/renderApp'
import { searchAccountRow, searchRecordRow, searchResults, toolsRoutes } from '../../test/toolsFixtures'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const rowOf = (text: string | RegExp, nth = 0) =>
  screen.getAllByText(text)[nth].closest('tr') as HTMLElement

describe('search page', () => {
  it('searches for q and lists the rows with their columns', async () => {
    const { calls } = renderApp('/search?q=example', toolsRoutes())
    await screen.findByRole('table')
    expect(calls.filter((c) => c.key.startsWith('GET /search'))[0].key).toBe('GET /search?q=example')
    for (const h of ['Status', 'Search Results', 'Date', 'Owner', 'Type']) {
      expect(screen.getByRole('columnheader', { name: h })).toBeTruthy()
    }
    expect(screen.getByText('5 objects')).toBeTruthy()
    expect(screen.queryByText(/Hestia/)).toBeNull()
    expect(calls.some((c) => c.key.includes('token'))).toBe(false)
  })

  it('labels each type with Hestia wording and gives the mail account its own icon', async () => {
    renderApp('/search?q=info', toolsRoutes())
    const row = await screen.findByText('info@bakery.example')
    expect(within(row.closest('tr') as HTMLElement).getByText('Account')).toBeTruthy()
    cleanup()
    renderApp('/search?q=example', toolsRoutes())
    await screen.findByRole('table')
    for (const label of ['Web Domain', 'DNS Domain', 'Mail Domain', 'Cron Jobs']) {
      expect(screen.getAllByText(label).length).toBeGreaterThan(0)
    }
    const icons = (row: HTMLElement) => row.querySelector('svg[data-type-icon]')?.getAttribute('data-type-icon')
    expect(icons(rowOf('Mail Domain'))).not.toBe(icons(rowOf('DNS Domain')))
  })

  it('marks a suspended row with the selected variant and a warning badge', async () => {
    renderApp('/search?q=example', toolsRoutes())
    await screen.findByRole('table')
    const row = screen.getByText('Suspended').closest('tr') as HTMLElement
    expect(within(row).getByText('example.net')).toBeTruthy()
    expect(row.className).toMatch(/selected|bg-kumo-(tint|info)/)
    expect(screen.getAllByText('Active').length).toBe(4)
  })

  it('sorts by date by default and by name from the sort menu', async () => {
    renderApp('/search?q=example', toolsRoutes())
    await screen.findByRole('table')
    expect(screen.getByRole('button', { name: /Sort by: Date/ })).toBeTruthy()
    const first = () => within(screen.getAllByRole('row')[1]).getAllByRole('cell')[2].textContent
    // newest first: the 2026-10-19 rows lead
    expect(first()).toMatch(/bakery\.example/)
    fireEvent.click(screen.getByRole('button', { name: /Sort by/ }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Name' }))
    await waitFor(() => expect(screen.getByRole('button', { name: /Sort by: Name/ })).toBeTruthy())
    expect(first()).toMatch(/bakery\.example/)
    const names = screen
      .getAllByRole('row')
      .slice(1)
      .map((r) => within(r).getAllByRole('cell')[2].firstElementChild?.firstElementChild?.textContent ?? '')
    expect(names).toEqual([...names].sort((a, b) => a.localeCompare(b, undefined, { numeric: true })))
  })

  it('links your own row to its route', async () => {
    renderApp('/search?q=example', toolsRoutes())
    await screen.findByRole('table')
    const own = within(rowOf('example.net')).getByRole('link', { name: 'example.net' })
    expect(own.getAttribute('href')).toBe('/web/example.net')
  })

  it("logs in as the owner, then lands on the row's route", async () => {
    const { calls, location } = renderApp('/search?q=example', toolsRoutes())
    await screen.findByRole('table')
    const bakery = within(rowOf('Web Domain', 0)).getByRole('button', { name: /bakery\.example/ })
    fireEvent.click(bakery)
    await waitFor(() => expect(location()).toBe('/web/bakery.example'))
    expect(calls.find((c) => c.key === 'POST /auth/login-as')?.body).toEqual({ user: 'bob' })
  })

  it('gives a login-as row no link a modified click could open without logging in', async () => {
    const { calls, location } = renderApp('/search?q=example', toolsRoutes())
    await screen.findByRole('table')
    const row = within(rowOf('Web Domain', 0))
    expect(row.queryByRole('link', { name: /bakery\.example/ })).toBeNull()
    fireEvent.click(row.getByRole('button', { name: /bakery\.example/ }), { ctrlKey: true })
    await waitFor(() => expect(location()).toBe('/web/bakery.example'))
    expect(calls.find((c) => c.key === 'POST /auth/login-as')?.body).toEqual({ user: 'bob' })
  })

  it('names a direction in each sort menu item', async () => {
    renderApp('/search?q=example', toolsRoutes())
    await screen.findByRole('table')
    const open = () => fireEvent.click(screen.getByRole('button', { name: /Sort by/ }))
    open()
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Name' }))
    await waitFor(() => expect(screen.getByRole('button', { name: /Sort by: Name/ })).toBeTruthy())
    // choosing Name again keeps A to Z instead of flipping
    open()
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Name' }))
    const first = () => within(screen.getAllByRole('row')[1]).getAllByRole('cell')[2].textContent
    expect(first()).toMatch(/^bakery\.example/)
    open()
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Date' }))
    await waitFor(() => expect(screen.getByRole('button', { name: /Sort by: Date/ })).toBeTruthy())
  })

  it('shows an unverified row as plain text', async () => {
    renderApp('/search?q=www', toolsRoutes())
    await screen.findByRole('table')
    const record = within(rowOf('Record'))
    expect(record.getByText('www.bakery.example')).toBeTruthy()
    expect(record.queryByRole('link', { name: 'www.bakery.example' })).toBeNull()
    expect(searchRecordRow().linked).toBe(false)
  })

  it('links the owner to the owner filter for an admin only', async () => {
    renderApp('/search?q=example', toolsRoutes())
    await screen.findByRole('table')
    const owner = within(rowOf('Web Domain', 0)).getByRole('link', { name: 'bob' })
    expect(owner.getAttribute('href')).toBe('/search?q=example&u=bob')
    cleanup()
    renderApp('/search?q=info', {
      ...toolsRoutes(),
      'GET /bootstrap': () => ({ body: signedIn({ name: 'bob', role: 'user' }) }),
      'GET /search?q=info': () => ({ body: searchResults({ query: 'info', rows: [searchAccountRow({ loginAs: false })] }) }),
    })
    await screen.findByRole('table')
    expect(within(screen.getByRole('table')).queryByRole('link', { name: 'bob' })).toBeNull()
  })

  it('passes the owner filter to the API', async () => {
    const { calls } = renderApp('/search?q=example&u=bob', {
      ...toolsRoutes(),
      'GET /search?q=example&u=bob': () => ({ body: searchResults({ user: 'bob' }) }),
    })
    await screen.findByRole('table')
    expect(calls.some((c) => c.key === 'GET /search?q=example&u=bob')).toBe(true)
  })

  it('shows a failed search in a banner', async () => {
    renderApp('/search?q=unreadable', {
      ...toolsRoutes(),
      'GET /search?q=unreadable': () => ({
        status: 422,
        body: { error: { code: 'search_failed', message: 'Error: Invalid key value format [x]' } },
      }),
    })
    expect(await screen.findByText('Error: Invalid key value format [x]')).toBeTruthy()
    expect(screen.queryByRole('table')).toBeNull()
  })

  it('shows an empty state and sends nothing without a query', async () => {
    const { calls } = renderApp('/search', toolsRoutes())
    expect(await screen.findByText('Type something to search for')).toBeTruthy()
    expect(calls.some((c) => c.key.startsWith('GET /search'))).toBe(false)
  })

  it('shows a loading state while the search is pending', async () => {
    renderApp('/search?q=slow', { ...toolsRoutes(), 'GET /search?q=slow': () => ({ body: searchResults() }) })
    const real = globalThis.fetch
    vi.stubGlobal('fetch', (url: string, init?: RequestInit) =>
      url.includes('/search') ? new Promise((r) => setTimeout(() => r(real(url, init)), 300)) : real(url, init),
    )
    expect((await screen.findAllByRole('status', { name: 'Loading' })).length).toBeGreaterThan(0)
    await screen.findByRole('table')
  })

  it('shows 0 objects for a search with no rows', async () => {
    renderApp('/search?q=none', {
      ...toolsRoutes(),
      'GET /search?q=none': () => ({ body: searchResults({ query: 'none', rows: [] }) }),
    })
    expect(await screen.findByText('No objects found')).toBeTruthy()
  })
})
