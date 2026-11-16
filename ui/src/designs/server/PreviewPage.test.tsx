import { cleanup, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderApp } from '../../test/renderApp'
import { serverRoutes } from '../../test/serverFixtures'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('preview features page', () => {
  it('shows the static row', async () => {
    renderApp('/server/preview', serverRoutes())
    expect(await screen.findByText('Policy: Allow suspended users to log in with read-only access')).toBeTruthy()
    expect(screen.getByText('System')).toBeTruthy()
    expect(screen.getByText('Partially implemented')).toBeTruthy()
    expect(document.body.textContent).not.toMatch(/Hestia/)
  })

  it('shows the error when the list fails', async () => {
    renderApp(
      '/server/preview',
      serverRoutes({ 'GET /server/preview': () => ({ status: 403, body: { error: { code: 'forbidden', message: 'Forbidden' } } }) }),
    )
    expect(await screen.findByText('Forbidden')).toBeTruthy()
  })
})
