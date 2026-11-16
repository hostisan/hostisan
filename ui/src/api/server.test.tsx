import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  serverKeys,
  useBans,
  useEditor,
  useFirewall,
  useFirewallRule,
  useGraph,
  useGraphs,
  useIp,
  useIpOptions,
  useIps,
  useIpsets,
  useJob,
  usePreview,
  useServerMutation,
  useServices,
  useSettings,
  useStatus,
  useUpdates,
  useWhiteLabel,
} from './server'
import type { JobStatus } from './types.gen'

afterEach(() => vi.unstubAllGlobals())

function wrap() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  )
  return { client, Wrapper }
}

const reply = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status })

function stubFetch(body: unknown = { jobId: 'a'.repeat(32) }) {
  const fetch = vi.fn(async (_url: string, _init?: RequestInit) => reply(body))
  vi.stubGlobal('fetch', fetch)
  return fetch
}

type ReadHook = () => { isSuccess: boolean; fetchStatus: string }

const callsOf = (fetch: ReturnType<typeof stubFetch>) => fetch.mock.calls.map((c) => `${c[1]?.method ?? 'GET'} ${c[0]}`)

describe('server read hooks', () => {
  it.each([
    ['useServices', () => useServices(), 'GET /api/v1/server/services'],
    ['useEditor', () => useEditor('nginx'), 'GET /api/v1/server/editors/nginx'],
    ['useSettings', () => useSettings(), 'GET /api/v1/server/settings'],
    ['useStatus', () => useStatus('mem'), 'GET /api/v1/server/status/mem'],
    ['useGraphs', () => useGraphs(), 'GET /api/v1/server/graphs'],
    ['useGraph', () => useGraph('net_eth0', 'weekly'), 'GET /api/v1/server/graphs/net_eth0?period=weekly'],
    ['useUpdates', () => useUpdates(), 'GET /api/v1/server/updates'],
    ['useWhiteLabel', () => useWhiteLabel(), 'GET /api/v1/server/white-label'],
    ['usePreview', () => usePreview(), 'GET /api/v1/server/preview'],
    ['useIps', () => useIps(), 'GET /api/v1/server/ips'],
    ['useIpOptions', () => useIpOptions(), 'GET /api/v1/server/ips/options'],
    ['useIp', () => useIp('203.0.113.10'), 'GET /api/v1/server/ips/203.0.113.10'],
    ['useFirewall', () => useFirewall(), 'GET /api/v1/server/firewall'],
    ['useFirewallRule', () => useFirewallRule('10'), 'GET /api/v1/server/firewall/rules/10'],
    ['useBans', () => useBans(), 'GET /api/v1/server/firewall/bans'],
    ['useIpsets', () => useIpsets(), 'GET /api/v1/server/firewall/ipsets'],
  ] as [string, ReadHook, string][])('%s calls its path', async (_name, hook, want) => {
    const fetch = stubFetch({})
    const { Wrapper } = wrap()
    const { result } = renderHook(hook, { wrapper: Wrapper })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(callsOf(fetch)).toEqual([want])
  })

  it('encodes path parameters', async () => {
    const fetch = stubFetch({})
    const { Wrapper } = wrap()
    const { result } = renderHook(() => useEditor('a b/c'), { wrapper: Wrapper })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(callsOf(fetch)).toEqual(['GET /api/v1/server/editors/a%20b%2Fc'])
  })

  it.each([
    ['useEditor', () => useEditor('')],
    ['useStatus', () => useStatus('')],
    ['useGraph without an id', () => useGraph('', 'daily')],
    ['useGraph without a period', () => useGraph('la', '')],
    ['useIp', () => useIp('')],
    ['useFirewallRule', () => useFirewallRule('')],
  ] as [string, ReadHook][])('%s stays idle for empty parameters', (_name, hook) => {
    const fetch = stubFetch({})
    const { Wrapper } = wrap()
    const { result } = renderHook(hook, { wrapper: Wrapper })
    expect(result.current.fetchStatus).toBe('idle')
    expect(fetch).not.toHaveBeenCalled()
  })

  it('every key starts with "server"', () => {
    for (const key of [
      serverKeys.all,
      serverKeys.services,
      serverKeys.editor('x'),
      serverKeys.settings,
      serverKeys.status('cpu'),
      serverKeys.graphs,
      serverKeys.graph('la', 'daily'),
      serverKeys.updates,
      serverKeys.whiteLabel,
      serverKeys.preview,
      serverKeys.ips,
      serverKeys.ipOptions,
      serverKeys.ip('x'),
      serverKeys.firewall,
      serverKeys.rule('1'),
      serverKeys.bans,
      serverKeys.ipsets,
    ]) {
      expect(key[0]).toBe('server')
    }
  })
})

describe('useJob', () => {
  const job = (state: string): JobStatus => ({
    id: 'a'.repeat(32),
    title: 'Restart nginx',
    state,
    step: 'Restart nginx',
    steps: ['Restart nginx'],
    error: null,
    result: null,
    started: '2026-10-21T12:00:00Z',
    finished: state === 'running' ? '' : '2026-10-21T12:00:03Z',
  })

  it('polls the server job route every second and stops when the job is done', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    try {
      const states = ['running', 'done']
      const fetch = vi.fn(async (_url: string) => reply(job(states.shift() ?? 'done')))
      vi.stubGlobal('fetch', fetch)
      const { Wrapper } = wrap()
      const { result } = renderHook(() => useJob('a'.repeat(32)), { wrapper: Wrapper })
      await waitFor(() => expect(result.current.data?.state).toBe('running'))
      expect(fetch.mock.calls[0][0]).toBe(`/api/v1/server/jobs/${'a'.repeat(32)}`)
      await vi.advanceTimersByTimeAsync(1000)
      await waitFor(() => expect(result.current.data?.state).toBe('done'))
      await vi.advanceTimersByTimeAsync(5000)
      expect(fetch).toHaveBeenCalledTimes(2)
    } finally {
      vi.useRealTimers()
    }
  })

  it('does nothing without an id', async () => {
    const fetch = stubFetch()
    const { Wrapper } = wrap()
    const { result } = renderHook(() => useJob(''), { wrapper: Wrapper })
    expect(result.current.fetchStatus).toBe('idle')
    expect(fetch).not.toHaveBeenCalled()
  })
})

describe('useServerMutation', () => {
  it('each mutation hits its route with its body and invalidates ["server"]', async () => {
    const fetch = stubFetch()
    const { client, Wrapper } = wrap()
    const invalidate = vi.spyOn(client, 'invalidateQueries')
    const { result } = renderHook(() => useServerMutation(), { wrapper: Wrapper })
    const m = result.current
    const rule = { action: 'ACCEPT', protocol: 'TCP', port: '80', ip: '0.0.0.0/0', comment: 'WEB' }
    const ip = {
      ip: '203.0.113.12',
      netmask: '255.255.255.0',
      interface: 'eth0',
      shared: true,
      owner: '',
      name: '',
      nat: '',
    }
    const cases: [() => Promise<unknown>, string, unknown][] = [
      [() => m.serviceAction('nginx', 'restart'), 'POST /api/v1/server/services/nginx/restart', {}],
      [
        () => m.serviceAction('ssh', 'stop', { confirm: 'ssh' }),
        'POST /api/v1/server/services/ssh/stop',
        { confirm: 'ssh' },
      ],
      [
        () => m.bulkServices({ action: 'restart', names: ['nginx', 'apache2'] }),
        'POST /api/v1/server/services/bulk',
        { action: 'restart', names: ['nginx', 'apache2'] },
      ],
      [() => m.reboot({ hostname: 'server1' }), 'POST /api/v1/server/reboot', { hostname: 'server1' }],
      [
        () => m.saveEditor('nginx', { files: [{ id: 'nginx', content: 'x', hash: 'h' }], restart: true }),
        'PUT /api/v1/server/editors/nginx',
        { restart: true },
      ],
      [() => m.saveSettings({ hostname: 'server2' }), 'PUT /api/v1/server/settings', { hostname: 'server2' }],
      [() => m.setAutoupdate({ enabled: false }), 'POST /api/v1/server/updates/autoupdate', { enabled: false }],
      [
        () =>
          m.saveWhiteLabel({
            appName: 'Acme',
            title: '',
            fromName: '',
            fromEmail: '',
            subjectEmail: '',
            hideDocs: false,
            updateLogo: true,
          }),
        'PUT /api/v1/server/white-label',
        { appName: 'Acme', updateLogo: true },
      ],
      [() => m.addIp(ip), 'POST /api/v1/server/ips', { ip: '203.0.113.12', shared: true }],
      [() => m.editIp('203.0.113.10', ip), 'PUT /api/v1/server/ips/203.0.113.10', { name: '' }],
      [() => m.deleteIp('203.0.113.11'), 'DELETE /api/v1/server/ips/203.0.113.11', undefined],
      [
        () => m.bulkIps({ action: 'delete', names: ['203.0.113.11'] }),
        'POST /api/v1/server/ips/bulk',
        { names: ['203.0.113.11'] },
      ],
      [() => m.refreshIps(), 'POST /api/v1/server/ips/refresh', undefined],
      [() => m.addRule(rule), 'POST /api/v1/server/firewall/rules', { port: '80' }],
      [() => m.editRule('9', { ...rule, expect: 'e9' }), 'PUT /api/v1/server/firewall/rules/9', { expect: 'e9' }],
      [
        () => m.ruleAction('9', 'move-up', { expect: 'e9' }),
        'POST /api/v1/server/firewall/rules/9/move-up',
        { expect: 'e9' },
      ],
      [
        () => m.bulkRules({ action: 'delete', items: [{ id: '3', expect: 'e3' }] }),
        'POST /api/v1/server/firewall/rules/bulk',
        { action: 'delete' },
      ],
      [() => m.toggleFirewall({ enabled: false }), 'POST /api/v1/server/firewall/toggle', { enabled: false }],
      [
        () => m.addBan({ ip: '198.51.100.7', chain: 'SSH' }),
        'POST /api/v1/server/firewall/bans',
        { ip: '198.51.100.7', chain: 'SSH' },
      ],
      [
        () => m.deleteBans({ items: [{ ip: '203.0.113.9', chain: 'MAIL' }] }),
        'POST /api/v1/server/firewall/bans/delete',
        { items: [{ ip: '203.0.113.9', chain: 'MAIL' }] },
      ],
      [
        () =>
          m.addIpset({ name: 'country-nl', source: 'https://example.net/nl.txt', ipVersion: 'v4', autoupdate: true }),
        'POST /api/v1/server/firewall/ipsets',
        { name: 'country-nl' },
      ],
      [
        () => m.bulkIpsets({ action: 'delete', names: ['blacklist'] }),
        'POST /api/v1/server/firewall/ipsets/bulk',
        { names: ['blacklist'] },
      ],
      [() => m.deleteIpset('blacklist'), 'DELETE /api/v1/server/firewall/ipsets/blacklist', undefined],
    ]
    expect(Object.keys(m).sort()).toEqual(
      [
        'serviceAction',
        'bulkServices',
        'reboot',
        'saveEditor',
        'saveSettings',
        'setAutoupdate',
        'saveWhiteLabel',
        'addIp',
        'editIp',
        'deleteIp',
        'bulkIps',
        'refreshIps',
        'addRule',
        'editRule',
        'ruleAction',
        'bulkRules',
        'toggleFirewall',
        'addBan',
        'deleteBans',
        'addIpset',
        'bulkIpsets',
        'deleteIpset',
      ].sort(),
    )
    for (const [call, want, body] of cases) {
      fetch.mockClear()
      invalidate.mockClear()
      await call()
      expect(callsOf(fetch)).toEqual([want])
      const sent = fetch.mock.calls[0][1]?.body
      if (body === undefined) expect(sent).toBeUndefined()
      else expect(JSON.parse(String(sent))).toMatchObject(body as object)
      await waitFor(() => expect(invalidate).toHaveBeenCalledWith({ queryKey: ['server'] }))
    }
  })

  it('a job route resolves to its JobRef', async () => {
    stubFetch({ jobId: 'b'.repeat(32) })
    const { Wrapper } = wrap()
    const { result } = renderHook(() => useServerMutation(), { wrapper: Wrapper })
    await expect(result.current.refreshIps()).resolves.toEqual({ jobId: 'b'.repeat(32) })
  })

  it('rejects with the API error and still invalidates', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => reply({ error: { code: 'busy', message: 'Another server task is still running.' } }, 409)),
    )
    const { client, Wrapper } = wrap()
    const invalidate = vi.spyOn(client, 'invalidateQueries')
    const { result } = renderHook(() => useServerMutation(), { wrapper: Wrapper })
    await expect(result.current.reboot({ hostname: 'x' })).rejects.toMatchObject({ status: 409, code: 'busy' })
    await waitFor(() => expect(invalidate).toHaveBeenCalledWith({ queryKey: ['server'] }))
  })
})
