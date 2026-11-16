import { renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  acctCalls,
  acctRunMutations,
  acctStubFetch,
  acctWrap,
  type AcctMutationCase,
  type AcctReadHook,
} from '../test/acctHooks'
import {
  accountKeys,
  useAccessKey,
  useAccessKeys,
  useAccount,
  useAccountMutation,
  useAccountOptions,
  useAuthLog,
  useKeyMutation,
  useLogMutation,
  useLogs,
  useNotificationMutation,
  useNotifications,
  useRecovery,
  useSshKeys,
  useStats,
  useUser,
} from './account'
import type { AccountRequest } from './types.gen'

afterEach(() => vi.unstubAllGlobals())

describe('account read hooks', () => {
  it.each([
    ['useAccount', () => useAccount(), 'GET /api/v1/account'],
    ['useAccountOptions', () => useAccountOptions(), 'GET /api/v1/account/options'],
    ['useUser', () => useUser('bob'), 'GET /api/v1/users/bob'],
    ['useSshKeys', () => useSshKeys(), 'GET /api/v1/account/ssh-keys'],
    ['useAccessKeys', () => useAccessKeys(), 'GET /api/v1/account/access-keys'],
    [
      'useAccessKey',
      () => useAccessKey('aB3dEfGh1JkLmN0pQrSt'),
      'GET /api/v1/account/access-keys/aB3dEfGh1JkLmN0pQrSt',
    ],
    ['useNotifications', () => useNotifications(), 'GET /api/v1/notifications'],
    ['useLogs', () => useLogs(), 'GET /api/v1/logs'],
    ['useLogs for system', () => useLogs('system'), 'GET /api/v1/logs?user=system'],
    ['useAuthLog', () => useAuthLog(), 'GET /api/v1/logs/auth'],
    ['useAuthLog for bob', () => useAuthLog('bob'), 'GET /api/v1/logs/auth?user=bob'],
    ['useStats', () => useStats(), 'GET /api/v1/stats'],
    ['useStats for bob', () => useStats('bob'), 'GET /api/v1/stats?user=bob'],
  ] as [string, AcctReadHook, string][])('%s calls its path', async (_name, hook, want) => {
    const fetch = acctStubFetch()
    const { Wrapper } = acctWrap()
    const { result } = renderHook(hook, { wrapper: Wrapper })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(acctCalls(fetch)).toEqual([want])
  })

  it.each([
    ['useUser', () => useUser('')],
    ['useAccessKey', () => useAccessKey('')],
    ['useNotifications turned off', () => useNotifications({ enabled: false })],
  ] as [string, AcctReadHook][])('%s stays idle', (_name, hook) => {
    const fetch = acctStubFetch()
    const { Wrapper } = acctWrap()
    const { result } = renderHook(hook, { wrapper: Wrapper })
    expect(result.current.fetchStatus).toBe('idle')
    expect(fetch).not.toHaveBeenCalled()
  })

  it('encodes parameters', async () => {
    const fetch = acctStubFetch()
    const { Wrapper } = acctWrap()
    const { result } = renderHook(() => [useUser('a/b'), useLogs('a&b')], { wrapper: Wrapper })
    await waitFor(() => expect(result.current.every((q) => q.isSuccess)).toBe(true))
    expect(acctCalls(fetch).sort()).toEqual(['GET /api/v1/logs?user=a%26b', 'GET /api/v1/users/a%2Fb'])
  })

  it('keys group by section', () => {
    expect(accountKeys.account[0]).toBe('account')
    expect(accountKeys.options[0]).toBe('account')
    expect(accountKeys.sshKeys[0]).toBe('account')
    expect(accountKeys.accessKeys[0]).toBe('account')
    expect(accountKeys.accessKey('x')[0]).toBe('account')
    expect(accountKeys.user('bob')[0]).toBe('users')
    expect(accountKeys.notifications[0]).toBe('notifications')
    expect(accountKeys.logs('')[0]).toBe('logs')
    expect(accountKeys.authLog('')[0]).toBe('logs')
    expect(accountKeys.stats('')[0]).toBe('stats')
    expect(accountKeys.logs('system')).not.toEqual(accountKeys.logs(''))
  })
})

const accountBody: AccountRequest = {
  name: 'Bob Baker',
  email: 'bob@acme-bakery.com',
  password: '',
  currentPassword: '',
  language: 'en',
  theme: 'dark',
  sortOrder: 'name',
  useIpList: false,
  allowedIps: [],
}

describe('useAccountMutation', () => {
  it('hits each route and invalidates ["account"], ["users"] and ["bootstrap"]', async () => {
    const fetch = acctStubFetch()
    const { client, Wrapper } = acctWrap()
    const invalidate = vi.spyOn(client, 'invalidateQueries')
    const { result } = renderHook(() => useAccountMutation(), { wrapper: Wrapper })
    const m = result.current
    expect(Object.keys(m).sort()).toEqual(
      ['addUser', 'editUser', 'save', 'twofaConfirm', 'twofaDisable', 'twofaSetup', 'userTwofaDisable'].sort(),
    )
    const addUser = {
      username: 'carol',
      password: 'Secr3tPass',
      email: 'carol@example.com',
      name: 'Carol',
      language: 'fr',
      role: 'user',
      package: 'default',
      loginDisabled: false,
      notify: 'carol@example.com',
    }
    const cases: AcctMutationCase[] = [
      [() => m.save(accountBody), 'PUT /api/v1/account', accountBody],
      [() => m.addUser(addUser), 'POST /api/v1/users', addUser],
      [
        () => m.editUser('bob', { ...accountBody, role: 'admin', nameservers: ['ns1.example.tld', 'ns2.example.tld'] }),
        'PUT /api/v1/users/bob',
        { ...accountBody, role: 'admin', nameservers: ['ns1.example.tld', 'ns2.example.tld'] },
      ],
      [() => m.twofaSetup(), 'POST /api/v1/account/2fa/setup', undefined],
      [
        () => m.twofaConfirm({ code: '123456', currentPassword: 'pw' }),
        'POST /api/v1/account/2fa/confirm',
        { code: '123456', currentPassword: 'pw' },
      ],
      [() => m.twofaDisable({ currentPassword: 'pw' }), 'DELETE /api/v1/account/2fa', { currentPassword: 'pw' }],
      [() => m.userTwofaDisable('bob'), 'DELETE /api/v1/users/bob/2fa', undefined],
    ]
    await acctRunMutations(fetch, invalidate, cases, [['account'], ['users'], ['bootstrap']])
  })
})

describe('useRecovery', () => {
  it('posts each step anonymously', async () => {
    const fetch = acctStubFetch({ status: 'sent' })
    const { client, Wrapper } = acctWrap()
    const invalidate = vi.spyOn(client, 'invalidateQueries')
    const { result } = renderHook(() => useRecovery(), { wrapper: Wrapper })
    const m = result.current
    expect(Object.keys(m).sort()).toEqual(['confirmReset', 'confirmUnlock', 'requestReset', 'requestUnlock'])
    const cases: AcctMutationCase[] = [
      [
        () => m.requestReset({ user: 'bob', email: 'bob@acme-bakery.com' }),
        'POST /api/v1/auth/reset/request',
        { user: 'bob', email: 'bob@acme-bakery.com' },
      ],
      [
        () => m.confirmReset({ user: 'bob', code: 'C', password: 'Secr3tPass', passwordConfirm: 'Secr3tPass' }),
        'POST /api/v1/auth/reset/confirm',
        { user: 'bob', code: 'C', password: 'Secr3tPass', passwordConfirm: 'Secr3tPass' },
      ],
      [
        () => m.requestUnlock({ user: 'bob', password: 'pw' }),
        'POST /api/v1/auth/reset-2fa/request',
        { user: 'bob', password: 'pw' },
      ],
      [
        () => m.confirmUnlock({ user: 'bob', code: 'C' }),
        'POST /api/v1/auth/reset-2fa/confirm',
        { user: 'bob', code: 'C' },
      ],
    ]
    await acctRunMutations(fetch, invalidate, cases, [])
    await expect(m.requestReset({ user: 'x', email: 'y' })).resolves.toEqual({ status: 'sent' })
  })
})

describe('useKeyMutation', () => {
  it('hits each route and invalidates ["account"]', async () => {
    const fetch = acctStubFetch()
    const { client, Wrapper } = acctWrap()
    const invalidate = vi.spyOn(client, 'invalidateQueries')
    const { result } = renderHook(() => useKeyMutation(), { wrapper: Wrapper })
    const m = result.current
    expect(Object.keys(m).sort()).toEqual(
      ['addAccessKey', 'addSshKey', 'bulkAccessKeys', 'removeAccessKey', 'removeSshKey'].sort(),
    )
    const cases: AcctMutationCase[] = [
      [
        () => m.addSshKey({ key: 'ssh-ed25519 AAAA bob@laptop' }),
        'POST /api/v1/account/ssh-keys',
        { key: 'ssh-ed25519 AAAA bob@laptop' },
      ],
      [() => m.removeSshKey('deploy key 2024'), 'DELETE /api/v1/account/ssh-keys/deploy%20key%202024', undefined],
      [
        () => m.addAccessKey({ permissions: ['mail-accounts'], comment: 'CI' }),
        'POST /api/v1/account/access-keys',
        { permissions: ['mail-accounts'], comment: 'CI' },
      ],
      [
        () => m.removeAccessKey('aB3dEfGh1JkLmN0pQrSt'),
        'DELETE /api/v1/account/access-keys/aB3dEfGh1JkLmN0pQrSt',
        undefined,
      ],
      [
        () => m.bulkAccessKeys({ action: 'delete', names: ['aB3dEfGh1JkLmN0pQrSt'] }),
        'POST /api/v1/account/access-keys/bulk',
        { action: 'delete', names: ['aB3dEfGh1JkLmN0pQrSt'] },
      ],
    ]
    await acctRunMutations(fetch, invalidate, cases, [['account']])
  })
})

describe('useNotificationMutation', () => {
  it('hits each route and invalidates ["notifications"]', async () => {
    const fetch = acctStubFetch()
    const { client, Wrapper } = acctWrap()
    const invalidate = vi.spyOn(client, 'invalidateQueries')
    const { result } = renderHook(() => useNotificationMutation(), { wrapper: Wrapper })
    const m = result.current
    expect(Object.keys(m).sort()).toEqual(['ack', 'ackAll', 'remove', 'removeAll'])
    const cases: AcctMutationCase[] = [
      [() => m.remove('2'), 'DELETE /api/v1/notifications/2', undefined],
      [() => m.removeAll(), 'DELETE /api/v1/notifications', undefined],
      [() => m.ack('2'), 'POST /api/v1/notifications/2/ack', undefined],
      [() => m.ackAll(), 'POST /api/v1/notifications/ack', undefined],
    ]
    await acctRunMutations(fetch, invalidate, cases, [['notifications']])
  })
})

describe('useLogMutation', () => {
  it('hits each route and invalidates ["logs"]', async () => {
    const fetch = acctStubFetch()
    const { client, Wrapper } = acctWrap()
    const invalidate = vi.spyOn(client, 'invalidateQueries')
    const { result } = renderHook(() => useLogMutation(), { wrapper: Wrapper })
    const m = result.current
    expect(Object.keys(m).sort()).toEqual(['clear', 'clearAuth'])
    const cases: AcctMutationCase[] = [
      [() => m.clear(), 'DELETE /api/v1/logs', undefined],
      [() => m.clear('system'), 'DELETE /api/v1/logs?user=system', undefined],
      [() => m.clearAuth(), 'DELETE /api/v1/logs/auth', undefined],
      [() => m.clearAuth('bob'), 'DELETE /api/v1/logs/auth?user=bob', undefined],
    ]
    await acctRunMutations(fetch, invalidate, cases, [['logs']])
  })
})
