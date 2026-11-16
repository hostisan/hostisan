import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '../api/client'
import { SessionProvider, useSession } from './SessionProvider'

function Probe() {
  const { bootstrap } = useSession()
  return <p>{bootstrap ? `${bootstrap.appName}:${bootstrap.authenticated}` : 'loading'}</p>
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('SessionProvider', () => {
  it('loads bootstrap and uses its CSRF token for later writes', async () => {
    const fetch = vi.fn(async (url: string) =>
      new Response(
        JSON.stringify(
          url.endsWith('/bootstrap')
            ? { authenticated: false, csrfToken: 'boot-token', appName: 'Hostisan', version: 'x', language: 'en', languages: ['en'], theme: '' }
            : {},
        ),
      ),
    )
    vi.stubGlobal('fetch', fetch)
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={client}>
        <SessionProvider>
          <Probe />
        </SessionProvider>
      </QueryClientProvider>,
    )
    expect(await screen.findByText('Hostisan:false')).toBeTruthy()
    await api.post('/auth/logout')
    const init = (fetch.mock.calls.at(-1) as unknown as [string, RequestInit])[1]
    expect(new Headers(init.headers).get('X-CSRF-Token')).toBe('boot-token')
  })
})
