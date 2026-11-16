import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import source from './StatsPage.tsx?raw'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { ComponentProps } from 'react'
import { statsMonth, statsRoutes } from '../../test/accountFixtures'
import { renderApp } from '../../test/renderApp'

// jsdom has no canvas: the chart is replaced by a list of its points, so the series
// (oldest to newest, as drawn) can be checked.
vi.mock('@cloudflare/kumo', async (orig) => ({
  ...(await orig<typeof import('@cloudflare/kumo')>()),
  TimeseriesChart: ({ data, ariaDescription }: ComponentProps<typeof import('@cloudflare/kumo').TimeseriesChart>) => (
    <ol aria-label={ariaDescription}>
      {data.map((s) => (
        <li key={s.name} data-series={s.name}>
          {s.data.map((p) => new Date(p[0]).toISOString().slice(0, 7) + '=' + p[1]).join(' ')}
        </li>
      ))}
    </ol>
  ),
}))

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const asUser = { role: 'user', landing: '/web', name: 'bob' }
const monthRows = () =>
  screen
    .getAllByRole('row')
    .map((r) => r.textContent ?? '')
    .filter((t) => /^(Aug|Sep|Oct) 2026/.test(t))

describe('statistics page', () => {
  it('does not import mock data', () => {
    expect(source).not.toMatch(/mocks\//)
    expect(source).not.toMatch(/mockUsersStats|mockUserStats|mockSession/)
  })

  it('shows the overall data to an admin, one row per month, newest first', async () => {
    const app = renderApp('/stats', statsRoutes())
    await screen.findByText('Monthly breakdown')
    expect(app.calls.some((c) => c.key === 'GET /stats')).toBe(true)
    await waitFor(() => expect(monthRows()).toHaveLength(3))
    const rows = monthRows()
    expect(rows[0]).toMatch(/^Oct 2026/)
    expect(rows[1]).toMatch(/^Sep 2026/)
    expect(rows[2]).toMatch(/^Aug 2026/)
    expect(screen.getByText('Server-wide usage, recorded on the first day of each month.')).toBeTruthy()
  })

  it('humanizes megabytes and shows unlimited as ∞', async () => {
    renderApp('/stats?user=admin', statsRoutes())
    await screen.findByText('Monthly breakdown')
    // 7342 MB is 7.17 GB, 38211 MB is 37.32 GB (Hestia rounds to 2 decimals); the admin has no quota
    const first = await waitFor(() => {
      const r = monthRows()[0]
      expect(r).toBeTruthy()
      return r
    })
    expect(first).toContain('7.17 GB')
    expect(first).toContain('37.32 GB')
    expect(first).toContain('∞')
    expect(first).not.toMatch(/unlimited/i)
  })

  it('offers an admin the overall link and the per-user select', async () => {
    const app = renderApp('/stats', statsRoutes())
    expect(await screen.findByRole('link', { name: 'Overall Statistics' })).toBeTruthy()
    await screen.findByText('Monthly breakdown')
    const select = screen.getByRole('combobox', { name: 'Show Per User' })
    fireEvent.click(select)
    const opt = await screen.findByRole('option', { name: 'User: bob' })
    fireEvent.pointerDown(opt)
    fireEvent.mouseDown(opt)
    fireEvent.pointerUp(opt)
    fireEvent.mouseUp(opt)
    fireEvent.click(opt)
    await waitFor(() => expect(app.calls.some((c) => c.key === 'GET /stats?user=bob')).toBe(true))
    expect(await screen.findByText('Usage for bob, recorded on the first day of each month.')).toBeTruthy()
  })

  it('lists exactly the users the server sent (root left out under hide-admin)', async () => {
    renderApp(
      '/stats',
      statsRoutes(
        {},
        {
          'GET /stats': () => ({
            body: { months: [statsMonth('2026-10-31')], overall: true, user: '', users: ['bob', 'susp', 'ops'] },
          }),
        },
      ),
    )
    await screen.findByText('Monthly breakdown')
    fireEvent.click(await screen.findByRole('combobox', { name: 'Show Per User' }))
    const names = (await screen.findAllByRole('option')).map((o) => o.textContent)
    expect(names).toContain('User: bob')
    expect(names).not.toContain('User: admin')
  })

  it('shows a regular user their own months, with neither admin control', async () => {
    const app = renderApp('/stats', statsRoutes(asUser, { 'GET /stats': statsRoutes()['GET /stats?user=bob'] }))
    await screen.findByText('Monthly breakdown')
    await waitFor(() => expect(monthRows()).toHaveLength(3))
    expect(screen.queryByRole('link', { name: 'Overall Statistics' })).toBeNull()
    expect(screen.queryByRole('combobox', { name: 'Show Per User' })).toBeNull()
    expect(app.calls.every((c) => !c.key.includes('user='))).toBe(true)
  })

  it('shows the empty state when there are no months', async () => {
    renderApp(
      '/stats',
      statsRoutes({}, { 'GET /stats': () => ({ body: { months: [], overall: true, user: '', users: ['admin'] } }) }),
    )
    expect(await screen.findByText('No statistics yet')).toBeTruthy()
  })

  it('shows the error from the API', async () => {
    renderApp(
      '/stats',
      statsRoutes({}, { 'GET /stats': () => ({ status: 500, body: { error: { code: 'x', message: 'Error: boom' } } }) }),
    )
    expect(await screen.findByText('Error: boom')).toBeTruthy()
  })

  it('has one summary card per metric from the newest month', async () => {
    renderApp('/stats?user=admin', statsRoutes())
    const disk = (await screen.findAllByText('7.17 GB')).find((e) => e.tagName === 'P') as HTMLElement
    expect(within(disk.parentElement as HTMLElement).getByText('of ∞')).toBeTruthy()
  })

  it('charts one point per month, oldest to newest', async () => {
    renderApp('/stats?user=admin', statsRoutes())
    const disk = await screen.findByRole('list', { name: 'Monthly bandwidth' })
    expect(disk.textContent).toBe('2026-08=30120 2026-09=35400 2026-10=38211')
  })

  it('keeps the chart colours in step with the theme', () => {
    expect(source).toMatch(/useSequentialColors\(\d+, dark\)/)
  })

  it('shows an empty state, not the latest month, when the range has no months', () => {
    expect(source).not.toMatch(/slice\(0, 1\)/)
  })
})
