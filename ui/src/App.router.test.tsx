import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { useBlocker } from 'react-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { dnsRoutes, renderApp, signedIn } from './test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function Crash(): never {
  throw new Error('render failed')
}

function BlockerProbe() {
  const blocker = useBlocker(false)
  return <p>Blocker {blocker.state}</p>
}

describe('app router', () => {
  it('the app routes run inside a data router', async () => {
    renderApp('/blocker-probe', dnsRoutes(), [{ path: '/blocker-probe', element: <BlockerProbe /> }])
    expect(await screen.findByText('Blocker unblocked')).toBeTruthy()
  })

  it('deep links still render the landing page for a signed-in user', async () => {
    const deep = renderApp('/dns', dnsRoutes())
    expect(await screen.findByRole('link', { name: 'example.com' })).toBeTruthy()
    expect(deep.location()).toBe('/dns')
    cleanup()

    const index = renderApp('/', dnsRoutes({ landing: '/dns' }))
    await waitFor(() => expect(index.location()).toBe('/dns'))
    expect(await screen.findByRole('link', { name: 'example.com' })).toBeTruthy()
  })

  it('a crash shows a translated error page with a reload button', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const reload = vi.fn()
    vi.stubGlobal('location', { ...window.location, reload })
    renderApp(
      '/crash',
      {
        'GET /bootstrap': () => ({ body: { ...signedIn({}), language: 'fr' } }),
        'GET /i18n/fr?v=1.0.0': () => ({
          body: { 'An internal error occurred': 'Une erreur interne est survenue', Reload: 'Recharger' },
        }),
      },
      [{ path: '/crash', element: <Crash /> }],
    )
    expect(await screen.findByRole('heading', { name: 'Une erreur interne est survenue' })).toBeTruthy()
    expect(screen.queryByText(/Unexpected Application Error/)).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Recharger' }))
    expect(reload).toHaveBeenCalled()
    vi.restoreAllMocks()
  })
})
