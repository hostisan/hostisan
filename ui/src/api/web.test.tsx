import { QueryClient, QueryClientProvider, type Query } from '@tanstack/react-query'
import { cleanup, renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { JobStatus } from './types.gen'
import {
  useWebApp,
  useWebAppJob,
  useWebApps,
  useWebDomain,
  useWebDomains,
  useWebLog,
  useWebMutation,
  useWebOptions,
  webKeys,
  webLogDownloadUrl,
  webSiteDownloadUrl,
} from './web'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function wrap() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  )
  return { client, Wrapper }
}

const reply = (body: unknown) => new Response(JSON.stringify(body), { status: 200 })

function stubFetch() {
  const fetch = vi.fn(async (_url: string, _init?: RequestInit) => reply({ status: 'ok', failed: [] }))
  vi.stubGlobal('fetch', fetch)
  return fetch
}

/** A read hook under test, as renderHook sees it. */
type ReadHook = () => { isSuccess: boolean; fetchStatus: string }

const callsOf = (fetch: ReturnType<typeof vi.fn>) =>
  fetch.mock.calls.map((c) => `${(c[1] as RequestInit | undefined)?.method ?? 'GET'} ${c[0]}`)

const job = (state: string): JobStatus => ({
  id: 'a'.repeat(32),
  title: 'WordPress',
  state,
  step: '',
  steps: [],
  error: null,
  result: null,
  started: '2026-10-21T09:00:00Z',
  finished: state === 'running' ? '' : '2026-10-21T09:02:00Z',
})

describe('web read hooks', () => {
  it.each([
    ['useWebDomains', () => useWebDomains(), 'GET /api/v1/web'],
    ['useWebOptions', () => useWebOptions(), 'GET /api/v1/web/options'],
    ['useWebDomain', () => useWebDomain('example.com'), 'GET /api/v1/web/example.com'],
    ['useWebLog access', () => useWebLog('example.com', 'access'), 'GET /api/v1/web/example.com/logs?type=access'],
    ['useWebLog error', () => useWebLog('example.com', 'error'), 'GET /api/v1/web/example.com/logs?type=error'],
    ['useWebApps', () => useWebApps('example.com'), 'GET /api/v1/web/example.com/apps'],
    ['useWebApp', () => useWebApp('example.com', 'wordpress'), 'GET /api/v1/web/example.com/apps/wordpress'],
  ] as [string, ReadHook, string][])('%s calls its path', async (_name, hook, want) => {
    const fetch = stubFetch()
    const { Wrapper } = wrap()
    const { result } = renderHook(hook, { wrapper: Wrapper })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(callsOf(fetch)).toEqual([want])
  })

  it('encodes path parameters', async () => {
    const fetch = stubFetch()
    const { Wrapper } = wrap()
    const { result } = renderHook(() => useWebApp('a b/c', 'x/y'), { wrapper: Wrapper })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(callsOf(fetch)).toEqual(['GET /api/v1/web/a%20b%2Fc/apps/x%2Fy'])
  })

  it.each([
    ['useWebDomain', () => useWebDomain('')],
    ['useWebLog', () => useWebLog('', 'access')],
    ['useWebApps', () => useWebApps('')],
    ['useWebApp without a domain', () => useWebApp('', 'wordpress')],
    ['useWebApp without an app', () => useWebApp('example.com', '')],
    ['useWebAppJob without a domain', () => useWebAppJob('', 'a'.repeat(32))],
    ['useWebAppJob without a job', () => useWebAppJob('example.com', '')],
  ] as [string, ReadHook][])('%s stays idle for empty parameters', async (_name, hook) => {
    const fetch = stubFetch()
    const { Wrapper } = wrap()
    const { result } = renderHook(hook, { wrapper: Wrapper })
    expect(result.current.fetchStatus).toBe('idle')
    await new Promise((r) => setTimeout(r, 20))
    expect(fetch).not.toHaveBeenCalled()
  })

  it('useWebDomains can be turned off', () => {
    const fetch = stubFetch()
    const { Wrapper } = wrap()
    const { result } = renderHook(() => useWebDomains({ enabled: false }), { wrapper: Wrapper })
    expect(result.current.fetchStatus).toBe('idle')
    expect(fetch).not.toHaveBeenCalled()
  })

  it('every key starts with "web"', () => {
    for (const key of [
      webKeys.all,
      webKeys.list,
      webKeys.options,
      webKeys.domain('d'),
      webKeys.log('d', 'access'),
      webKeys.apps('d'),
      webKeys.app('d', 'a'),
    ]) {
      expect(key[0]).toBe('web')
    }
  })

  it('builds the download URLs', () => {
    expect(webLogDownloadUrl('example.com', 'error')).toBe('/api/v1/web/example.com/logs/download?type=error')
    expect(webLogDownloadUrl('a b', 'access')).toBe('/api/v1/web/a%20b/logs/download?type=access')
    expect(webSiteDownloadUrl('example.com')).toBe('/api/v1/web/example.com/download')
  })
})

describe('useWebAppJob', () => {
  it('calls the poll path, polls while running and stops when done', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    try {
      const states = ['running', 'running', 'done']
      const fetch = vi.fn(async (_url: string) => reply(job(states.shift() ?? 'done')))
      vi.stubGlobal('fetch', fetch)
      const { Wrapper } = wrap()
      const id = 'a'.repeat(32)
      const { result } = renderHook(() => useWebAppJob('example.com', id), { wrapper: Wrapper })
      await waitFor(() => expect(result.current.data?.state).toBe('running'))
      expect(callsOf(fetch)).toEqual([`GET /api/v1/web/example.com/apps/jobs/${id}`])
      await vi.advanceTimersByTimeAsync(1900)
      expect(fetch).toHaveBeenCalledTimes(1)
      await vi.advanceTimersByTimeAsync(200)
      await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2))
      await vi.advanceTimersByTimeAsync(2100)
      await waitFor(() => expect(result.current.data?.state).toBe('done'))
      expect(fetch).toHaveBeenCalledTimes(3)
      await vi.advanceTimersByTimeAsync(6000)
      expect(fetch).toHaveBeenCalledTimes(3)
    } finally {
      vi.useRealTimers()
    }
  })
})

describe('useWebMutation', () => {
  it('each mutation hits its route with its body and invalidates ["web"]', async () => {
    const fetch = stubFetch()
    const { client, Wrapper } = wrap()
    const invalidate = vi.spyOn(client, 'invalidateQueries')
    const { result } = renderHook(() => useWebMutation(), { wrapper: Wrapper })
    const m = result.current
    const cases: [() => Promise<unknown>, string, unknown][] = [
      [
        () => m.addDomain({ domain: 'new.example', ip: '203.0.113.10', dns: true, mail: false }),
        'POST /api/v1/web',
        { domain: 'new.example', ip: '203.0.113.10', dns: true, mail: false },
      ],
      [
        () =>
          m.editDomain('example.com', {
            ip: '203.0.113.10',
            aliases: 'www.example.com\nblog.example.com',
            stats: 'awstats',
            statsAuth: false,
            statsUser: '',
            statsPassword: '',
            redirect: false,
            redirectTarget: '',
            redirectCode: '301',
            docroot: false,
            docrootDomain: '',
            docrootFolder: '',
            ftp: [],
          }),
        'PUT /api/v1/web/example.com',
        { stats: 'awstats', ftp: [] },
      ],
      [
        () =>
          m.saveSsl('example.com', {
            ssl: true,
            letsencrypt: true,
            keyAlgo: 'ecdsa-256',
            forceSsl: true,
            hsts: false,
            crt: '',
            key: '',
            ca: '',
            reissue: true,
          }),
        'PUT /api/v1/web/example.com/ssl',
        { reissue: true },
      ],
      [() => m.deleteDomain('example.com'), 'DELETE /api/v1/web/example.com', undefined],
      [() => m.suspendDomain('example.com'), 'POST /api/v1/web/example.com/suspend', undefined],
      [() => m.unsuspendDomain('example.com'), 'POST /api/v1/web/example.com/unsuspend', undefined],
      [() => m.purgeCache('example.com'), 'POST /api/v1/web/example.com/purge-cache', undefined],
      [
        () => m.bulkDomains({ action: 'purge', names: ['example.com'] }),
        'POST /api/v1/web/bulk',
        { action: 'purge', names: ['example.com'] },
      ],
      [
        () =>
          m.generateSsl({
            domain: 'example.com',
            aliases: '',
            email: '',
            country: 'US',
            state: 'California',
            locality: 'San Francisco',
            org: 'MyCompany Inc',
            keyAlgo: 'ecdsa-256',
          }),
        'POST /api/v1/web/generate-ssl',
        { domain: 'example.com', locality: 'San Francisco' },
      ],
      [
        () => m.installApp('example.com', 'wordpress', { values: { site_name: 'Blog' }, phpVersion: '8.3' }),
        'POST /api/v1/web/example.com/apps/wordpress',
        { values: { site_name: 'Blog' }, phpVersion: '8.3' },
      ],
    ]
    expect(Object.keys(m).sort()).toEqual(
      [
        'addDomain',
        'editDomain',
        'saveSsl',
        'deleteDomain',
        'suspendDomain',
        'unsuspendDomain',
        'purgeCache',
        'bulkDomains',
        'generateSsl',
        'installApp',
      ].sort(),
    )
    for (const [run, want, body] of cases) {
      fetch.mockClear()
      invalidate.mockClear()
      await run()
      expect(callsOf(fetch)).toEqual([want])
      const sent = fetch.mock.calls[0][1]?.body
      if (body === undefined) expect(sent).toBeUndefined()
      else expect(JSON.parse(String(sent))).toEqual(expect.objectContaining(body as object))
      await waitFor(() => expect(invalidate).toHaveBeenCalledWith(expect.objectContaining({ queryKey: ['web'] })))
    }
  })

  it("a delete refetches every web query but the deleted domain's own", async () => {
    stubFetch()
    const { client, Wrapper } = wrap()
    const invalidate = vi.spyOn(client, 'invalidateQueries')
    const { result } = renderHook(() => useWebMutation(), { wrapper: Wrapper })
    await result.current.deleteDomain('example.com')
    await waitFor(() => expect(invalidate).toHaveBeenCalled())
    const filters = invalidate.mock.calls[0][0]!
    expect(filters.queryKey).toEqual(['web'])
    const keep = (key: unknown[]) => filters.predicate!({ queryKey: key } as unknown as Query)
    expect(keep(['web', 'list'])).toBe(true)
    expect(keep(['web', 'domain', 'example.net'])).toBe(true)
    expect(keep(['web', 'domain', 'example.com'])).toBe(false)
    expect(keep(['web', 'domain', 'example.com', 'log', 'access'])).toBe(false)
    expect(keep(['web', 'domain', 'example.com', 'apps'])).toBe(false)
  })

  it('returns the reply: a JobRef from installApp, the PEMs from generateSsl', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string) =>
        url.endsWith('/generate-ssl')
          ? reply({ crt: 'C', key: 'K', csr: 'R' })
          : new Response(JSON.stringify({ jobId: 'b'.repeat(32) }), { status: 202 }),
      ),
    )
    const { Wrapper } = wrap()
    const { result } = renderHook(() => useWebMutation(), { wrapper: Wrapper })
    await expect(result.current.installApp('example.com', 'grav', { values: {}, phpVersion: '8.3' })).resolves.toEqual({
      jobId: 'b'.repeat(32),
    })
    await expect(
      result.current.generateSsl({
        domain: 'x.example',
        aliases: '',
        email: '',
        country: 'US',
        state: 'CA',
        locality: 'SF',
        org: 'Org',
        keyAlgo: 'rsa',
      }),
    ).resolves.toEqual({ crt: 'C', key: 'K', csr: 'R' })
  })

  it('invalidates ["web"] when a write fails too', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response(JSON.stringify({ error: { code: 'forbidden', message: '' } }), { status: 403 })),
    )
    const { client, Wrapper } = wrap()
    const invalidate = vi.spyOn(client, 'invalidateQueries')
    const { result } = renderHook(() => useWebMutation(), { wrapper: Wrapper })
    await expect(result.current.suspendDomain('example.com')).rejects.toMatchObject({ status: 403 })
    await waitFor(() => expect(invalidate).toHaveBeenCalledWith({ queryKey: ['web'] }))
  })
})
