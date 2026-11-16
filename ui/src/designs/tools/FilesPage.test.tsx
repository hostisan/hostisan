import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { StrictMode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { SessionProvider } from '../../session/SessionProvider'
import { FilesPage } from './FilesPage'
import { renderApp, signedIn } from '../../test/renderApp'
import { toolsRoutes } from '../../test/toolsFixtures'

const assign = vi.fn()
beforeEach(() => {
  assign.mockReset()
  vi.stubGlobal('location', { ...window.location, assign })
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('file manager handoff', () => {
  it('prepares the session once and goes to the file manager', async () => {
    const app = renderApp('/files', toolsRoutes())
    await waitFor(() => expect(assign).toHaveBeenCalledWith('/fm/'))
    expect(app.calls.filter((c) => c.key === 'POST /files/handoff')).toHaveLength(1)
    expect(assign).toHaveBeenCalledTimes(1)
  })

  it('still sends one request under StrictMode', async () => {
    const calls: string[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string, init?: RequestInit) => {
        calls.push(`${init?.method ?? 'GET'} ${url.replace('/api/v1', '')}`)
        const body = url.endsWith('/bootstrap') ? signedIn({}) : { url: '/fm/' }
        return new Response(JSON.stringify(body), { status: 200 })
      }),
    )
    const router = createMemoryRouter([{ path: '/', element: <FilesPage /> }])
    render(
      <StrictMode>
        <QueryClientProvider client={new QueryClient()}>
          <SessionProvider>
            <RouterProvider router={router} />
          </SessionProvider>
        </QueryClientProvider>
      </StrictMode>,
    )
    await waitFor(() => expect(assign).toHaveBeenCalledWith('/fm/'))
    expect(calls.filter((c) => c === 'POST /files/handoff')).toHaveLength(1)
  })

  it('shows the refusal with a retry', async () => {
    let n = 0
    const app = renderApp(
      '/files',
      toolsRoutes({
        'POST /files/handoff': () =>
          ++n === 1
            ? { status: 409, body: { error: { code: 'files_unavailable', message: 'The file manager is not available.' } } }
            : { body: { url: '/fm/' } },
      }),
    )
    expect(await screen.findByText('The file manager is not available.')).toBeTruthy()
    expect(assign).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    await waitFor(() => expect(assign).toHaveBeenCalledWith('/fm/'))
    expect(app.calls.filter((c) => c.key === 'POST /files/handoff')).toHaveLength(2)
  })

  it('shows an empty state and posts nothing when the file manager is off', async () => {
    const app = renderApp('/files', {
      ...toolsRoutes(),
      'GET /bootstrap': () => ({ body: signedIn({ features: { fileManager: false } }) }),
    })
    expect(await screen.findByText('The file manager is not available')).toBeTruthy()
    expect(app.calls.some((c) => c.key === 'POST /files/handoff')).toBe(false)
    expect(assign).not.toHaveBeenCalled()
  })

  it('says why a read-only session has no file manager', async () => {
    renderApp('/files', {
      ...toolsRoutes(),
      'GET /bootstrap': () => ({ body: signedIn({ readOnly: true, features: { fileManager: false } }) }),
    })
    expect(await screen.findByText("Read-only sessions can't use the file manager.")).toBeTruthy()
  })

  it('is unavailable for a read-only session even with the feature on', async () => {
    const app = renderApp('/files', {
      ...toolsRoutes(),
      'GET /bootstrap': () => ({ body: signedIn({ readOnly: true }) }),
    })
    expect(await screen.findByText("Read-only sessions can't use the file manager.")).toBeTruthy()
    expect(app.calls.some((c) => c.key === 'POST /files/handoff')).toBe(false)
  })

  it('only follows a path on this site', async () => {
    renderApp('/files', toolsRoutes({ 'POST /files/handoff': () => ({ body: { url: '//evil.example/x' } }) }))
    await waitFor(() => expect(screen.getByText("The file manager couldn't be opened.")).toBeTruthy())
    expect(assign).not.toHaveBeenCalled()
  })

  it('explains a refusal for a protected account', async () => {
    renderApp(
      '/files',
      toolsRoutes({ 'POST /files/handoff': () => ({ status: 403, body: { error: { code: 'forbidden', message: '' } } }) }),
    )
    expect(await screen.findByText("The file manager isn't available for this account.")).toBeTruthy()
  })
})
