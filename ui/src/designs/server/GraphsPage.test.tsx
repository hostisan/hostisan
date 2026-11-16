import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderApp } from '../../test/renderApp'
import { serverRoutes } from '../../test/serverFixtures'

vi.mock('@cloudflare/kumo', async (importOriginal) => {
  const real = await importOriginal<typeof import('@cloudflare/kumo')>()
  return {
    ...real,
    TimeseriesChart: (p: { yAxisName?: string; data: { name: string; data: unknown[] }[]; ariaDescription?: string }) => (
      <div data-testid="chart" data-yname={p.yAxisName} data-series={p.data.map((s) => s.name).join('|')} data-points={p.data[0]?.data.length}>
        {p.ariaDescription}
      </div>
    ),
  }
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

// The first case renders the whole app cold (1.4–1.8 s at load 6, over the 3 s waitFor
// default at the load 15–18 parallel runs reach); it gets the time a busy machine needs.
const slow = { timeout: 10000 }

describe('graphs page', () => {
  it('draws one card per chart with its unit on the y-axis', async () => {
    renderApp('/server/graphs', serverRoutes())
    await waitFor(() => expect(screen.getAllByTestId('chart')).toHaveLength(7), slow)
    const mem = screen.getByRole('heading', { name: 'Memory usage' }).closest('section, div')!.parentElement!
    expect(within(mem).getByTestId('chart').getAttribute('data-yname')).toBe('MB')
    expect(within(mem).getByTestId('chart').getAttribute('data-series')).toBe('Used|Swap|Free')
    expect(screen.getByRole('heading', { name: 'Load average' })).toBeTruthy()
  })

  it('loads every chart for the chosen period', async () => {
    const { calls } = renderApp('/server/graphs', serverRoutes())
    await waitFor(() => expect(screen.getAllByTestId('chart')).toHaveLength(7))
    expect(calls.filter((c) => c.key.endsWith('?period=daily'))).toHaveLength(7)
    fireEvent.click(screen.getByRole('tab', { name: 'Weekly' }))
    await waitFor(() => expect(calls.filter((c) => c.key.endsWith('?period=weekly'))).toHaveLength(7))
    for (const p of ['Daily', 'Monthly', 'Yearly', 'Biennially', 'Triennially']) {
      expect(screen.getByRole('tab', { name: p })).toBeTruthy()
    }
  })

  it('shows one failing chart as its own error while the others draw', async () => {
    renderApp(
      '/server/graphs',
      serverRoutes({
        'GET /server/graphs/ssh?period=daily': () => ({ status: 500, body: { error: { code: 'command_failed', message: 'Error: rrd broke' } } }),
      }),
    )
    expect(await screen.findByText('Error: rrd broke')).toBeTruthy()
    await waitFor(() => expect(screen.getAllByTestId('chart')).toHaveLength(6))
  })

  it('shows the catalogue error instead of an empty page', async () => {
    renderApp(
      '/server/graphs',
      serverRoutes({ 'GET /server/graphs': () => ({ body: { charts: [], error: 'Error: no rrd' } }) }),
    )
    expect(await screen.findByText('Error: no rrd')).toBeTruthy()
  })

  it('titles cards from the chart type and name', async () => {
    renderApp('/server/graphs', serverRoutes())
    for (const title of ['Bandwidth usage eth0', 'NGINX usage', 'Exim usage', 'MySQL usage on localhost', 'SSH usage']) {
      expect(await screen.findByRole('heading', { name: title })).toBeTruthy()
    }
  })

  it('shows an empty state for an empty catalogue', async () => {
    renderApp('/server/graphs', serverRoutes({ 'GET /server/graphs': () => ({ body: { charts: [] } }) }))
    expect(await screen.findByText('No graphs yet')).toBeTruthy()
  })
})
