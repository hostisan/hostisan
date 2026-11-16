import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { downloadUrl, phpMyAdminSsoUrl, useDatabase, useDatabases, useDbMutation } from './db'

afterEach(() => vi.unstubAllGlobals())

function wrap() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  )
  return Wrapper
}

const reply = (body: unknown) => new Response(JSON.stringify(body), { status: 200 })

describe('db link helpers', () => {
  it('build the download and SSO paths, encoding the name', () => {
    expect(downloadUrl('admin_wp')).toBe('/api/v1/db/admin_wp/download')
    expect(phpMyAdminSsoUrl('admin_wp')).toBe('/api/v1/db/admin_wp/phpmyadmin')
    expect(downloadUrl('a b/c')).toBe('/api/v1/db/a%20b%2Fc/download')
  })
})

describe('db hooks', () => {
  it('useDatabase stays idle for an empty name', () => {
    const fetch = vi.fn(async () => reply({}))
    vi.stubGlobal('fetch', fetch)
    const { result } = renderHook(() => useDatabase(''), { wrapper: wrap() })
    expect(result.current.fetchStatus).toBe('idle')
    expect(fetch).not.toHaveBeenCalled()
  })

  it('useDatabases returns databases and usage', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => reply({ databases: [], usage: { databases: '0' } })),
    )
    const { result } = renderHook(() => useDatabases(), { wrapper: wrap() })
    await waitFor(() => expect(result.current.data?.usage.databases).toBe('0'))
  })

  it('mutations hit the right routes and refetch the list', async () => {
    const fetch = vi.fn(async (_url: string, _init?: RequestInit) => reply({ status: 'ok', failed: [] }))
    vi.stubGlobal('fetch', fetch)
    const { result } = renderHook(() => ({ list: useDatabases(), m: useDbMutation() }), { wrapper: wrap() })
    await waitFor(() => expect(result.current.list.isSuccess).toBe(true))
    await result.current.m.suspendDb('admin_wp')
    await result.current.m.unsuspendDb('admin_wp')
    await result.current.m.deleteDb('admin_wp')
    await result.current.m.bulkDb({ action: 'delete', names: ['a'] })
    const calls = fetch.mock.calls.map((c) => `${c[1]?.method ?? 'GET'} ${c[0]}`)
    expect(calls).toEqual(
      expect.arrayContaining([
        'POST /api/v1/db/admin_wp/suspend',
        'POST /api/v1/db/admin_wp/unsuspend',
        'DELETE /api/v1/db/admin_wp',
        'POST /api/v1/db/bulk',
      ]),
    )
    await waitFor(() =>
      expect(
        fetch.mock.calls.filter((c) => c[0] === '/api/v1/db' && (c[1]?.method ?? 'GET') === 'GET').length,
      ).toBeGreaterThan(1),
    )
  })
})
