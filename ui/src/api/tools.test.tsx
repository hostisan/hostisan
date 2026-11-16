import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useFilesHandoff, useRestoreFiles, useRestoreJob, useSearch, useTerminalToken } from './tools'

afterEach(() => vi.unstubAllGlobals())

function wrap() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  )
  return { client, Wrapper }
}

function stubFetch(body: unknown) {
  const fetch = vi.fn(async (_url: string, _init?: RequestInit) => new Response(JSON.stringify(body), { status: 200 }))
  vi.stubGlobal('fetch', fetch)
  return fetch
}

const callsOf = (fetch: ReturnType<typeof stubFetch>) =>
  fetch.mock.calls.map((c) => `${c[1]?.method ?? 'GET'} ${c[0]}${c[1]?.body ? ' ' + String(c[1].body) : ''}`)

describe('tools read hooks', () => {
  it('useSearch sends the query, and the owner filter when set', async () => {
    const fetch = stubFetch({ query: 'example', user: '', rows: [] })
    const { Wrapper } = wrap()
    const { result } = renderHook(() => useSearch('example', ''), { wrapper: Wrapper })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    const { result: owner } = renderHook(() => useSearch('a b|c', 'bob'), { wrapper: Wrapper })
    await waitFor(() => expect(owner.current.isSuccess).toBe(true))
    expect(callsOf(fetch)).toEqual(['GET /api/v1/search?q=example', 'GET /api/v1/search?q=a%20b%7Cc&u=bob'])
  })

  it('useSearch sends nothing for an empty query', async () => {
    const fetch = stubFetch({})
    const { Wrapper } = wrap()
    const { result } = renderHook(() => useSearch('', 'bob'), { wrapper: Wrapper })
    await new Promise((r) => setTimeout(r, 20))
    expect(result.current.fetchStatus).toBe('idle')
    expect(fetch).not.toHaveBeenCalled()
  })

  it('useRestoreJob polls /backup/restore-jobs/{id} (ruling R1), idle without an id', async () => {
    const fetch = stubFetch({ id: 'j', state: 'done', steps: [], error: null })
    const { Wrapper } = wrap()
    const { result } = renderHook(() => useRestoreJob('0123456789abcdef0123456789abcdef'), { wrapper: Wrapper })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    renderHook(() => useRestoreJob(''), { wrapper: Wrapper })
    await new Promise((r) => setTimeout(r, 20))
    expect(callsOf(fetch)).toEqual(['GET /api/v1/backup/restore-jobs/0123456789abcdef0123456789abcdef'])
  })
})

describe('tools mutations', () => {
  it('useTerminalToken posts and resolves to the token', async () => {
    const fetch = stubFetch({ token: 't', expires: '2026-10-28T10:00:30Z' })
    const { Wrapper } = wrap()
    const { result } = renderHook(() => useTerminalToken(), { wrapper: Wrapper })
    let reply: unknown
    await act(async () => {
      reply = await result.current.mutateAsync()
    })
    expect(reply).toEqual({ token: 't', expires: '2026-10-28T10:00:30Z' })
    expect(callsOf(fetch)).toEqual(['POST /api/v1/terminal/token'])
  })

  it('useFilesHandoff posts and resolves to the handoff', async () => {
    const fetch = stubFetch({ url: '/fm/' })
    const { Wrapper } = wrap()
    const { result } = renderHook(() => useFilesHandoff(), { wrapper: Wrapper })
    let reply: unknown
    await act(async () => {
      reply = await result.current.mutateAsync()
    })
    expect(reply).toEqual({ url: '/fm/' })
    expect(callsOf(fetch)).toEqual(['POST /api/v1/files/handoff'])
  })

  it('useRestoreFiles posts the paths to the snapshot and resolves to the job', async () => {
    const fetch = stubFetch({ jobId: 'a'.repeat(32) })
    const { Wrapper } = wrap()
    const { result } = renderHook(() => useRestoreFiles('3c9b8a7e'), { wrapper: Wrapper })
    let reply: unknown
    await act(async () => {
      reply = await result.current.mutateAsync({ paths: ['/home/bob/a', '/home/bob/b'] })
    })
    expect(reply).toEqual({ jobId: 'a'.repeat(32) })
    expect(callsOf(fetch)).toEqual([
      'POST /api/v1/backup/incremental/3c9b8a7e/restore-files {"paths":["/home/bob/a","/home/bob/b"]}',
    ])
  })
})
