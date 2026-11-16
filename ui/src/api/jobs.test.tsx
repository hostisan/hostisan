import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useJobStatus } from './jobs'
import type { JobStatus } from './types.gen'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

const job = (state: string): JobStatus => ({
  id: 'a'.repeat(32),
  title: 'Install',
  state,
  step: '',
  steps: [],
  error: null,
  result: null,
  started: '2026-01-10T12:00:00Z',
  finished: state === 'running' ? '' : '2026-01-10T12:01:00Z',
})

describe('useJobStatus', () => {
  it('polls while running and stops when done', async () => {
    const states = ['running', 'running', 'done']
    const fetch = vi.fn(async () => new Response(JSON.stringify(job(states.shift() ?? 'done'))))
    vi.stubGlobal('fetch', fetch)
    const { result } = renderHook(() => useJobStatus('/server/jobs/x', 10), { wrapper })
    await waitFor(() => expect(result.current.data?.state).toBe('done'))
    expect(fetch).toHaveBeenCalledTimes(3)
    await new Promise((r) => setTimeout(r, 50))
    expect(fetch).toHaveBeenCalledTimes(3)
  })

  it('stops polling after an error', async () => {
    let n = 0
    const fetch = vi.fn(async () =>
      ++n === 1
        ? new Response(JSON.stringify(job('running')))
        : new Response(JSON.stringify({ error: { code: 'not_found', message: 'gone' } }), { status: 404 }),
    )
    vi.stubGlobal('fetch', fetch)
    const { result } = renderHook(() => useJobStatus('/server/jobs/x', 10), { wrapper })
    await waitFor(() => expect(result.current.error).toBeTruthy())
    const calls = fetch.mock.calls.length
    await new Promise((r) => setTimeout(r, 60))
    expect(fetch).toHaveBeenCalledTimes(calls)
    expect(calls).toBe(2)
  })

  it('does not fetch without a path', async () => {
    const fetch = vi.fn()
    vi.stubGlobal('fetch', fetch)
    renderHook(() => useJobStatus(undefined, 10), { wrapper })
    await new Promise((r) => setTimeout(r, 50))
    expect(fetch).not.toHaveBeenCalled()
  })
})
