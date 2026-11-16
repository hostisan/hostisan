import { cleanup, fireEvent, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderApp } from '../../test/renderApp'
import { webRoutes } from '../../test/webFixtures'
import { webCommands } from './web'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const features = {
  web: true,
  dns: true,
  db: true,
  mail: true,
  cron: true,
  backup: true,
  incremental: true,
  logs: true,
  accessKeys: true,
  server: true,
  firewall: true,
  firewallBans: true,
}
const openPalette = () => fireEvent.keyDown(window, { key: 'k', ctrlKey: true })

describe('web commands', () => {
  it('lists the API web domains and offers Add web domain', async () => {
    const app = renderApp('/web/new', webRoutes({ features }))
    await screen.findByText('Hosting')
    expect(app.calls.some((c) => c.key === 'GET /web')).toBe(false) // nothing before the palette opens
    openPalette()
    expect(await screen.findByRole('option', { name: /Add web domain/ })).toBeTruthy()
    fireEvent.change(await screen.findByPlaceholderText(/Search pages/), { target: { value: 'example.net' } })
    expect(await screen.findByRole('option', { name: /^example\.net/ })).toBeTruthy()
    expect(app.calls.some((c) => c.key === 'GET /web')).toBe(true)
  })

  it('offers neither the action nor the domains without the web feature', async () => {
    const app = renderApp('/web/new', webRoutes({ features: { ...features, web: false } }))
    await screen.findByText('Hosting')
    openPalette()
    await screen.findByPlaceholderText(/Search pages/)
    expect(screen.queryByRole('option', { name: /Add web domain/ })).toBeNull()
    expect(app.calls.some((c) => c.key === 'GET /web')).toBe(false)
  })
})

describe('webCommands', () => {
  it('has no page entries, and no Add action for a read-only user', () => {
    expect(webCommands(true, false, undefined).pages).toEqual([])
    expect(webCommands(true, false, undefined).actions.map((a) => a.title)).toEqual(['Add web domain'])
    expect(webCommands(true, true, undefined).actions).toEqual([])
    expect(webCommands(false, false, undefined)).toEqual({ actions: [], pages: [], groups: [] })
  })
})
