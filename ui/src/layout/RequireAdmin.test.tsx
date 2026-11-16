import { cleanup, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { usersRoutes } from '../test/accountFixtures'
import { renderApp, signedIn } from '../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const every = signedIn({}).user.features

// The first case renders the whole app cold (4.6 s on a loaded machine, over the
// 3 s waitFor default); the redirects get the time a busy machine needs.
const slow = { timeout: 10000 }

// Owner ruling 23: one route-level guard sends anyone who isn't an admin (or an admin
// logged in as another user) from an admin-only page to their own landing page.
describe('admin-only pages', () => {
  it.each(['/users', '/users/new', '/users/admin', '/packages', '/packages/new', '/server/services', '/server/config'])(
    'redirects a user from %s to their landing page',
    async (path) => {
      const app = renderApp(path, usersRoutes({ name: 'bob', role: 'user', landing: '/cron', features: every }))
      await waitFor(() => expect(app.location()).toBe('/cron'), slow)
      expect(app.calls.some((c) => c.key.startsWith('GET /users') || c.key.startsWith('GET /packages'))).toBe(false)
    },
  )

  it('redirects an admin logged in as another user from the users and server pages', async () => {
    for (const path of ['/users', '/packages', '/server/services']) {
      const app = renderApp(path, usersRoutes({ look: 'bob', landing: '/web', features: every }))
      await waitFor(() => expect(app.location()).toBe('/web'), slow)
      cleanup()
    }
  })

  it('redirects an admin without the server feature from the server pages only', async () => {
    const user = { landing: '/web', features: { ...every, server: false } }
    const app = renderApp('/server/services', usersRoutes(user))
    await waitFor(() => expect(app.location()).toBe('/web'), slow)
    cleanup()
    const users = renderApp('/users', usersRoutes(user))
    expect(await screen.findAllByText('bob', {}, slow)).toBeTruthy()
    expect(users.location()).toBe('/users')
  })

  it('lets an admin in', async () => {
    const app = renderApp('/packages', usersRoutes({ features: every }))
    await screen.findByRole('table', {}, slow)
    expect(app.location()).toBe('/packages')
  })
})
