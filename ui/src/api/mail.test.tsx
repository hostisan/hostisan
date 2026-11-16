import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  mailKeys,
  useMailAccount,
  useMailAccounts,
  useMailDns,
  useMailDomain,
  useMailDomains,
  useMailMutation,
  useMailOptions,
} from './mail'

afterEach(() => vi.unstubAllGlobals())

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

const callsOf = (fetch: ReturnType<typeof stubFetch>) => fetch.mock.calls.map((c) => `${c[1]?.method ?? 'GET'} ${c[0]}`)

describe('mail read hooks', () => {
  it.each([
    ['useMailDomains', () => useMailDomains(), 'GET /api/v1/mail'],
    ['useMailOptions', () => useMailOptions(), 'GET /api/v1/mail/options'],
    ['useMailDomain', () => useMailDomain('example.com'), 'GET /api/v1/mail/example.com'],
    ['useMailDns', () => useMailDns('example.com'), 'GET /api/v1/mail/example.com/dns'],
    ['useMailAccounts', () => useMailAccounts('example.com'), 'GET /api/v1/mail/example.com/accounts'],
    ['useMailAccount', () => useMailAccount('example.com', 'info'), 'GET /api/v1/mail/example.com/accounts/info'],
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
    const { result } = renderHook(() => useMailAccount('a b/c', 'x/y'), { wrapper: Wrapper })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(callsOf(fetch)).toEqual(['GET /api/v1/mail/a%20b%2Fc/accounts/x%2Fy'])
  })

  it.each([
    ['useMailDomain', () => useMailDomain('')],
    ['useMailDns', () => useMailDns('')],
    ['useMailAccounts', () => useMailAccounts('')],
    ['useMailAccount without a domain', () => useMailAccount('', 'info')],
    ['useMailAccount without an account', () => useMailAccount('example.com', '')],
  ] as [string, ReadHook][])('%s stays idle for empty parameters', (_name, hook) => {
    const fetch = stubFetch()
    const { Wrapper } = wrap()
    const { result } = renderHook(hook, { wrapper: Wrapper })
    expect(result.current.fetchStatus).toBe('idle')
    expect(fetch).not.toHaveBeenCalled()
  })

  it('useMailDomains can be turned off', () => {
    const fetch = stubFetch()
    const { Wrapper } = wrap()
    const { result } = renderHook(() => useMailDomains({ enabled: false }), { wrapper: Wrapper })
    expect(result.current.fetchStatus).toBe('idle')
    expect(fetch).not.toHaveBeenCalled()
  })

  it('every key starts with "mail"', () => {
    for (const key of [
      mailKeys.all,
      mailKeys.options,
      mailKeys.domain('d'),
      mailKeys.dns('d'),
      mailKeys.accounts('d'),
      mailKeys.account('d', 'a'),
    ]) {
      expect(key[0]).toBe('mail')
    }
  })
})

describe('useMailMutation', () => {
  it('each mutation hits its route with its body and invalidates ["mail"]', async () => {
    const fetch = stubFetch()
    const { client, Wrapper } = wrap()
    const invalidate = vi.spyOn(client, 'invalidateQueries')
    const { result } = renderHook(() => useMailMutation(), { wrapper: Wrapper })
    const m = result.current
    const relay = { host: 'smtp.example.net', port: '587', user: 'relay', password: 'pw' }
    const cases: [() => Promise<unknown>, string, unknown][] = [
      [
        () =>
          m.addDomain({
            domain: 'new.example',
            webmail: 'roundcube',
            antispam: true,
            reject: true,
            antivirus: true,
            dkim: true,
            dkimSelector: 'mail',
            smtpRelay: relay,
          }),
        'POST /api/v1/mail',
        { domain: 'new.example' },
      ],
      [
        () => m.editDomain('example.com', { catchall: '', dkim: true, dkimSelector: 'mail', smtpRelay: null }),
        'PUT /api/v1/mail/example.com',
        { smtpRelay: null },
      ],
      [
        () =>
          m.saveSsl('example.com', { ssl: true, letsencrypt: true, keyAlgo: 'ecdsa-256', crt: '', key: '', ca: '' }),
        'PUT /api/v1/mail/example.com/ssl',
        { letsencrypt: true },
      ],
      [() => m.deleteDomain('example.com'), 'DELETE /api/v1/mail/example.com', undefined],
      [() => m.suspendDomain('example.com'), 'POST /api/v1/mail/example.com/suspend', undefined],
      [() => m.unsuspendDomain('example.com'), 'POST /api/v1/mail/example.com/unsuspend', undefined],
      [
        () => m.bulkDomains({ action: 'delete', names: ['example.net'] }),
        'POST /api/v1/mail/bulk',
        { action: 'delete', names: ['example.net'] },
      ],
      [
        () =>
          m.addAccount('example.com', {
            account: 'new',
            password: 'Secr3tPass',
            quota: 'unlimited',
            aliases: 'a, b',
            forwards: '',
            discard: false,
            forwardOnly: false,
            sendTo: '',
          }),
        'POST /api/v1/mail/example.com/accounts',
        { account: 'new' },
      ],
      [
        () =>
          m.editAccount('example.com', 'info', {
            password: '',
            quota: '1024',
            aliases: '',
            forwards: '',
            discard: false,
            forwardOnly: false,
            autoreply: true,
            autoreplyMessage: 'Away',
            sendTo: '',
          }),
        'PUT /api/v1/mail/example.com/accounts/info',
        { autoreplyMessage: 'Away' },
      ],
      [() => m.deleteAccount('example.com', 'info'), 'DELETE /api/v1/mail/example.com/accounts/info', undefined],
      [() => m.suspendAccount('example.com', 'info'), 'POST /api/v1/mail/example.com/accounts/info/suspend', undefined],
      [
        () => m.unsuspendAccount('example.com', 'info'),
        'POST /api/v1/mail/example.com/accounts/info/unsuspend',
        undefined,
      ],
      [
        () => m.bulkAccounts('example.com', { action: 'delete', names: ['info', 'sales'] }),
        'POST /api/v1/mail/example.com/accounts/bulk',
        { names: ['info', 'sales'] },
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
        'bulkDomains',
        'addAccount',
        'editAccount',
        'deleteAccount',
        'suspendAccount',
        'unsuspendAccount',
        'bulkAccounts',
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
      await waitFor(() => expect(invalidate).toHaveBeenCalledWith({ queryKey: ['mail'] }))
    }
  })

  it('invalidates ["mail"] when a write fails too', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response(JSON.stringify({ error: { code: 'forbidden', message: '' } }), { status: 403 })),
    )
    const { client, Wrapper } = wrap()
    const invalidate = vi.spyOn(client, 'invalidateQueries')
    const { result } = renderHook(() => useMailMutation(), { wrapper: Wrapper })
    await expect(result.current.suspendDomain('example.com')).rejects.toMatchObject({ status: 403 })
    await waitFor(() => expect(invalidate).toHaveBeenCalledWith({ queryKey: ['mail'] }))
  })
})
