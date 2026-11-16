import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderApp } from '../../test/renderApp'
import { webRoutes } from '../../test/webFixtures'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('web logs', () => {
  it('shows the access log by default', async () => {
    renderApp('/web/example.com/logs', webRoutes())
    expect(await screen.findByText(/GET \/css\/site\.css/)).toBeTruthy()
    expect(screen.getByRole('tab', { name: 'Access log' }).getAttribute('aria-selected')).toBe('true')
    expect(screen.getByText('example.com.access.log')).toBeTruthy()
  })

  it('shows the error log for ?type=error and switches tabs', async () => {
    const app = renderApp('/web/example.com/logs?type=error', webRoutes())
    expect(await screen.findByText(/AH00128/)).toBeTruthy()
    expect(screen.getByRole('tab', { name: 'Error log' }).getAttribute('aria-selected')).toBe('true')
    fireEvent.click(screen.getByRole('tab', { name: 'Access log' }))
    expect(await screen.findByText(/GET \/css\/site\.css/)).toBeTruthy()
    expect(app.calls.some((c) => c.key === 'GET /web/example.com/logs?type=access')).toBe(true)
  })

  it('treats any other type as access', async () => {
    renderApp('/web/example.com/logs?type=bogus', webRoutes())
    expect(await screen.findByText(/GET \/css\/site\.css/)).toBeTruthy()
  })

  it('Refresh fetches the log again', async () => {
    const app = renderApp('/web/example.com/logs', webRoutes())
    await screen.findByText(/GET \/css\/site\.css/)
    const count = () => app.calls.filter((c) => c.key === 'GET /web/example.com/logs?type=access').length
    expect(count()).toBe(1)
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }))
    await waitFor(() => expect(count()).toBe(2))
  })

  it('downloads the log from the API', async () => {
    renderApp('/web/example.com/logs?type=error', webRoutes())
    await screen.findByText(/AH00128/)
    const link = screen.getByText(/Download error log/).closest('a') as HTMLAnchorElement
    expect(link.getAttribute('href')).toBe('/api/v1/web/example.com/logs/download?type=error')
  })

  it('shows an empty state for an empty log', async () => {
    renderApp('/web/example.net/logs', webRoutes())
    expect(await screen.findByText('The access log is empty')).toBeTruthy()
  })

  it('shows not found for a domain missing from the list, without loading the edit view', async () => {
    // The logs API answers 200 with no lines for an unknown domain
    const app = renderApp(
      '/web/missing.example/logs',
      webRoutes({}, {
        'GET /web/missing.example/logs?type=access': () => ({ body: { type: 'access', lines: [] } }),
      }),
    )
    expect(await screen.findByText('Web domain not found')).toBeTruthy()
    expect(screen.queryByText('The access log is empty')).toBeNull()
    expect(app.calls.some((c) => c.key === 'GET /web/missing.example')).toBe(false)
  })

  it('shows the error when the log fails for another reason', async () => {
    renderApp(
      '/web/example.com/logs',
      webRoutes({}, { 'GET /web/example.com/logs?type=access': () => ({ status: 500, body: { error: { code: 'x', message: 'Error: boom' } } }) }),
    )
    expect(await screen.findByText('Error: boom')).toBeTruthy()
  })
})
