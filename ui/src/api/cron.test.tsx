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
import { cronKeys, useCronJob, useCronJobs, useCronMutation, useCronOptions } from './cron'

afterEach(() => vi.unstubAllGlobals())

describe('cron read hooks', () => {
  it.each([
    ['useCronJobs', () => useCronJobs(), 'GET /api/v1/cron'],
    ['useCronOptions', () => useCronOptions(), 'GET /api/v1/cron/options'],
    ['useCronJob', () => useCronJob('2'), 'GET /api/v1/cron/2'],
  ] as [string, AcctReadHook, string][])('%s calls its path', async (_name, hook, want) => {
    const fetch = acctStubFetch()
    const { Wrapper } = acctWrap()
    const { result } = renderHook(hook, { wrapper: Wrapper })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(acctCalls(fetch)).toEqual([want])
  })

  it('useCronJob stays idle without an id', () => {
    const fetch = acctStubFetch()
    const { Wrapper } = acctWrap()
    const { result } = renderHook(() => useCronJob(''), { wrapper: Wrapper })
    expect(result.current.fetchStatus).toBe('idle')
    expect(fetch).not.toHaveBeenCalled()
  })

  it('every key starts with "cron"', () => {
    for (const key of [cronKeys.all, cronKeys.list, cronKeys.options, cronKeys.job('1')]) expect(key[0]).toBe('cron')
  })
})

describe('useCronMutation', () => {
  it('hits each route and invalidates ["cron"]', async () => {
    const fetch = acctStubFetch()
    const { client, Wrapper } = acctWrap()
    const invalidate = vi.spyOn(client, 'invalidateQueries')
    const { result } = renderHook(() => useCronMutation(), { wrapper: Wrapper })
    const m = result.current
    expect(Object.keys(m).sort()).toEqual(['add', 'bulk', 'edit', 'remove', 'setReports', 'suspend', 'unsuspend'])
    const job = { min: '*/5', hour: '*', day: '*', month: '*', wday: '*', cmd: 'echo "hi" \\ there' }
    const cases: AcctMutationCase[] = [
      [() => m.add(job), 'POST /api/v1/cron', job],
      [() => m.edit('2', job), 'PUT /api/v1/cron/2', job],
      [() => m.remove('2'), 'DELETE /api/v1/cron/2', undefined],
      [() => m.suspend('2'), 'POST /api/v1/cron/2/suspend', undefined],
      [() => m.unsuspend('2'), 'POST /api/v1/cron/2/unsuspend', undefined],
      [
        () => m.bulk({ action: 'delete', names: ['1', '2'] }),
        'POST /api/v1/cron/bulk',
        { action: 'delete', names: ['1', '2'] },
      ],
      [() => m.setReports(true), 'POST /api/v1/cron/reports', { enabled: true }],
      [() => m.setReports(false), 'POST /api/v1/cron/reports', { enabled: false }],
    ]
    await acctRunMutations(fetch, invalidate, cases, [['cron']])
  })
})
