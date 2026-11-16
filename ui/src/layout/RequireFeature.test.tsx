import { cleanup, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { usersRoutes } from '../test/accountFixtures'
import { renderApp, signedIn } from '../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const every = signedIn({}).user.features

describe('access keys without API access', () => {
  it.each(['/account/access-keys', '/account/access-keys/new', '/account/access-keys/abc'])(
    'redirects %s to the account page',
    async (path) => {
      const app = renderApp(path, usersRoutes({ name: 'bob', role: 'user', features: { ...every, accessKeys: false } }))
      await waitFor(() => expect(app.location()).toBe('/account'))
      expect(app.calls.some((c) => c.key.startsWith('GET /account/access-keys'))).toBe(false)
      expect(screen.queryByRole('link', { name: 'Add access key' })).toBeNull()
    },
  )
})
