// Helpers for the Plan 3 data-layer hook tests (api/{users,account,cron,backup}.test.tsx):
// a fresh query client, a fetch stub that records "METHOD /api/v1/path" and its body,
// and a runner that checks each mutation's route, body and invalidated keys.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { expect, vi } from 'vitest'

export function acctWrap() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  )
  return { client, Wrapper }
}

/** Stubs fetch with a 200 JSON reply (body) for every request. */
export function acctStubFetch(body: unknown = { status: 'ok', failed: [] }) {
  const fetch = vi.fn(async (_url: string, _init?: RequestInit) => new Response(JSON.stringify(body), { status: 200 }))
  vi.stubGlobal('fetch', fetch)
  return fetch
}

export type AcctFetch = ReturnType<typeof acctStubFetch>

export const acctCalls = (fetch: AcctFetch) => fetch.mock.calls.map((c) => `${c[1]?.method ?? 'GET'} ${c[0]}`)

/** A read hook under test, as renderHook sees it. */
export type AcctReadHook = () => { isSuccess: boolean; fetchStatus: string }

/** [call, "METHOD /api/v1/path", expected body (undefined: none)] */
export type AcctMutationCase = [() => Promise<unknown>, string, unknown]

/**
 * Runs each case and checks it sent exactly one request to its route with its body,
 * then invalidated every key in keys.
 */
export async function acctRunMutations(
  fetch: AcctFetch,
  invalidate: { mock: { calls: unknown[][] }; mockClear: () => void },
  cases: AcctMutationCase[],
  keys: unknown[][],
) {
  for (const [run, want, body] of cases) {
    fetch.mockClear()
    invalidate.mockClear()
    await run()
    expect(acctCalls(fetch), want).toEqual([want])
    const sent = fetch.mock.calls[0][1]?.body
    if (body === undefined) expect(sent, want).toBeUndefined()
    else expect(JSON.parse(String(sent)), want).toEqual(body)
    for (const key of keys) {
      await waitFor(() => expect(invalidate.mock.calls, want).toContainEqual([{ queryKey: key }]))
    }
  }
}
