import { cleanup, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderApp } from '../../test/renderApp'
import { serverRoutes } from '../../test/serverFixtures'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('server routes', () => {
  it('serves /server/preview with the preview features list', async () => {
    const { calls } = renderApp('/server/preview', serverRoutes())
    expect(await screen.findByRole('heading', { name: 'Preview features' })).toBeTruthy()
    expect(await screen.findByText('Policy: Allow suspended users to log in with read-only access')).toBeTruthy()
    expect(screen.getByText('Partially implemented')).toBeTruthy()
    expect(calls.map((c) => c.key)).toContain('GET /server/preview')
  })
})
