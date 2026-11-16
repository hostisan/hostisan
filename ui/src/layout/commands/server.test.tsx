import { renderHook } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { SessionProvider } from '../../session/SessionProvider'
import { signedIn } from '../../test/renderApp'
import { noCommands } from './types'
import { useServerCommands } from './server'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const features = {
  web: true, dns: true, db: true, mail: true, cron: true, backup: true, incremental: true,
  logs: true, accessKeys: true, server: true, firewall: true, firewallBans: true,
}

async function contribution(user: Record<string, unknown>) {
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify(signedIn(user)), { status: 200 })))
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>
      <SessionProvider>{children}</SessionProvider>
    </QueryClientProvider>
  )
  const { result } = renderHook(() => useServerCommands(true), { wrapper })
  await waitFor(() => expect(client.getQueryData(['bootstrap'])).toBeTruthy())
  return result
}

describe('useServerCommands', () => {
  it('offers Add IP address and Add firewall rule to a server admin', async () => {
    const result = await contribution({ features })
    await waitFor(() => expect(result.current.actions.map((a) => [a.title, a.href])).toEqual([
      ['Add IP address', '/server/ip/new'],
      ['Add firewall rule', '/server/firewall/new'],
    ]))
    expect(result.current.pages).toEqual([])
    expect(result.current.groups).toEqual([])
  })

  it('drops the firewall rule without the firewall feature', async () => {
    const result = await contribution({ features: { ...features, firewall: false } })
    await waitFor(() => expect(result.current.actions.map((a) => a.title)).toEqual(['Add IP address']))
  })

  it('returns nothing without the server feature, or for a non-admin', async () => {
    const off = await contribution({ features: { ...features, server: false } })
    await new Promise((r) => setTimeout(r, 20))
    expect(off.current).toBe(noCommands)
    cleanup()
    const user = await contribution({ features, role: 'user' })
    await new Promise((r) => setTimeout(r, 20))
    expect(user.current).toBe(noCommands)
  })
})
