import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderApp } from '../../test/renderApp'
import { serverRoutes } from '../../test/serverFixtures'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('updates page', () => {
  it('lists the packages by display name with their update state', async () => {
    renderApp('/server/updates', serverRoutes())
    expect(await screen.findByText('hostisan-php')).toBeTruthy()
    expect(screen.getByText('hostisan')).toBeTruthy()
    expect(screen.getByText('Hostisan internal php interpreter')).toBeTruthy()
    expect(screen.getAllByText('Package up-to-date')).toHaveLength(2)
    expect(screen.getAllByText('Update available')).toHaveLength(1)
    expect(screen.queryByText(/hestia/i)).toBeNull()
    expect(document.body.textContent).not.toMatch(/hestia/i)
  })

  it('posts the automatic updates switch', async () => {
    const { calls } = renderApp('/server/updates', serverRoutes({ 'POST /server/updates/autoupdate': () => ({ body: { status: 'ok' } }) }))
    const toggle = await screen.findByRole('switch', { name: 'Automatic updates' })
    await waitFor(() => expect(toggle.getAttribute('aria-checked')).toBe('true'))
    fireEvent.click(toggle)
    await waitFor(() => expect(calls.find((c) => c.key === 'POST /server/updates/autoupdate')?.body).toEqual({ enabled: false }))
  })

  it('shows an error from the switch', async () => {
    renderApp(
      '/server/updates',
      serverRoutes({ 'POST /server/updates/autoupdate': () => ({ status: 500, body: { error: { code: 'command_failed', message: 'Error: cron failed' } } }) }),
    )
    const toggle = await screen.findByRole('switch', { name: 'Automatic updates' })
    await waitFor(() => expect(toggle.getAttribute('aria-checked')).toBe('true'))
    fireEvent.click(toggle)
    expect(await screen.findByText('Error: cron failed')).toBeTruthy()
  })

  it('shows a failed package list as an error, not an empty list', async () => {
    renderApp(
      '/server/updates',
      serverRoutes({ 'GET /server/updates': () => ({ status: 500, body: { error: { code: 'command_failed', message: 'Error: apt failed' } } }) }),
    )
    expect(await screen.findByText('Error: apt failed')).toBeTruthy()
    expect(screen.queryByText('No packages found')).toBeNull()
  })
})
