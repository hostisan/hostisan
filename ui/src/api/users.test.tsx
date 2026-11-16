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
import type { PackageRequest } from './types.gen'
import {
  USER_BULK_ACTIONS,
  usePackage,
  usePackageMutation,
  usePackageOptions,
  usePackages,
  useUserMutation,
  useUsers,
  usersKeys,
} from './users'

afterEach(() => vi.unstubAllGlobals())

describe('users read hooks', () => {
  it.each([
    ['useUsers', () => useUsers(), 'GET /api/v1/users'],
    ['usePackages', () => usePackages(), 'GET /api/v1/packages'],
    ['usePackageOptions', () => usePackageOptions(), 'GET /api/v1/packages/options'],
    ['usePackage', () => usePackage('starter-plus'), 'GET /api/v1/packages/starter-plus'],
  ] as [string, AcctReadHook, string][])('%s calls its path', async (_name, hook, want) => {
    const fetch = acctStubFetch()
    const { Wrapper } = acctWrap()
    const { result } = renderHook(hook, { wrapper: Wrapper })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(acctCalls(fetch)).toEqual([want])
  })

  it('encodes the package name and stays idle without one', async () => {
    const fetch = acctStubFetch()
    const { Wrapper } = acctWrap()
    const idle = renderHook(() => usePackage(''), { wrapper: Wrapper })
    expect(idle.result.current.fetchStatus).toBe('idle')
    const { result } = renderHook(() => usePackage('a b/c'), { wrapper: Wrapper })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(acctCalls(fetch)).toEqual(['GET /api/v1/packages/a%20b%2Fc'])
  })

  it('every users key starts with "users" and every package key with "packages"', () => {
    expect(usersKeys.users[0]).toBe('users')
    expect(usersKeys.list[0]).toBe('users')
    expect(usersKeys.packages[0]).toBe('packages')
    expect(usersKeys.packageList[0]).toBe('packages')
    expect(usersKeys.packageOptions[0]).toBe('packages')
    expect(usersKeys.pkg('x')[0]).toBe('packages')
  })

  it('lists the bulk actions the server accepts', () => {
    expect(USER_BULK_ACTIONS).toEqual([
      'rebuild',
      'rebuild-user',
      'rebuild-web',
      'rebuild-dns',
      'rebuild-mail',
      'rebuild-db',
      'rebuild-cron',
      'update-counters',
      'suspend',
      'unsuspend',
      'delete',
    ])
  })
})

describe('useUserMutation', () => {
  it('hits each route and invalidates ["users"]', async () => {
    const fetch = acctStubFetch()
    const { client, Wrapper } = acctWrap()
    const invalidate = vi.spyOn(client, 'invalidateQueries')
    const { result } = renderHook(() => useUserMutation(), { wrapper: Wrapper })
    const m = result.current
    expect(Object.keys(m).sort()).toEqual(['bulk', 'remove', 'suspend', 'unsuspend'])
    const cases: AcctMutationCase[] = [
      [() => m.suspend('bob'), 'POST /api/v1/users/bob/suspend', undefined],
      [() => m.unsuspend('susp'), 'POST /api/v1/users/susp/unsuspend', undefined],
      [() => m.remove('bob'), 'DELETE /api/v1/users/bob', undefined],
      [
        () => m.bulk({ action: 'update-counters', names: ['bob', 'susp'] }),
        'POST /api/v1/users/bulk',
        { action: 'update-counters', names: ['bob', 'susp'] },
      ],
    ]
    await acctRunMutations(fetch, invalidate, cases, [['users']])
  })
})

describe('usePackageMutation', () => {
  it('hits each route and invalidates ["packages"] and ["users"]', async () => {
    const fetch = acctStubFetch({ status: 'ok', name: 'starter' })
    const { client, Wrapper } = acctWrap()
    const invalidate = vi.spyOn(client, 'invalidateQueries')
    const { result } = renderHook(() => usePackageMutation(), { wrapper: Wrapper })
    const m = result.current
    expect(Object.keys(m).sort()).toEqual(['add', 'bulk', 'copy', 'edit', 'remove'])
    const body: PackageRequest = {
      name: 'starter',
      webTemplate: 'default',
      backendTemplate: 'default',
      proxyTemplate: 'default',
      dnsTemplate: 'default',
      webDomains: '1',
      webAliases: '5',
      dnsDomains: '1',
      dnsRecords: 'unlimited',
      mailDomains: '1',
      mailAccounts: '5',
      rateLimit: '200',
      databases: '1',
      cronJobs: '1',
      diskQuota: '1000',
      bandwidth: '1000',
      cpuQuota: 'unlimited',
      cpuQuotaPeriod: 'unlimited',
      memoryLimit: 'unlimited',
      swapLimit: 'unlimited',
      backups: '1',
      backupsIncremental: 'no',
      shell: 'nologin',
      nameservers: ['ns1.example.tld', 'ns2.example.tld'],
    }
    const cases: AcctMutationCase[] = [
      [() => m.add(body), 'POST /api/v1/packages', body],
      [
        () => m.edit('starter', { ...body, name: 'starter-2' }),
        'PUT /api/v1/packages/starter',
        { ...body, name: 'starter-2' },
      ],
      [() => m.copy('starter'), 'POST /api/v1/packages/starter/copy', undefined],
      [() => m.remove('starter'), 'DELETE /api/v1/packages/starter', undefined],
      [
        () => m.bulk({ action: 'delete', names: ['starter'] }),
        'POST /api/v1/packages/bulk',
        { action: 'delete', names: ['starter'] },
      ],
    ]
    await acctRunMutations(fetch, invalidate, cases, [['packages'], ['users']])
  })

  it('returns the saved name', async () => {
    acctStubFetch({ status: 'ok', name: 'starter-2' })
    const { Wrapper } = acctWrap()
    const { result } = renderHook(() => usePackageMutation(), { wrapper: Wrapper })
    await expect(result.current.copy('starter')).resolves.toMatchObject({ status: 'ok' })
  })
})
