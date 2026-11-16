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
  backupKeys,
  fileUrl,
  useBackup,
  useBackupExclusions,
  useBackupMutation,
  useBackups,
  useSnapshot,
  useSnapshotFiles,
  useSnapshots,
} from './backup'

afterEach(() => vi.unstubAllGlobals())

const name = 'admin.2026-10-19_03-00-02.tar'

describe('backup read hooks', () => {
  it.each([
    ['useBackups', () => useBackups(), 'GET /api/v1/backup'],
    ['useBackup', () => useBackup(name), `GET /api/v1/backup/${name}`],
    ['useBackupExclusions', () => useBackupExclusions(), 'GET /api/v1/backup/exclusions'],
    ['useSnapshots', () => useSnapshots(), 'GET /api/v1/backup/incremental'],
    ['useSnapshot', () => useSnapshot('3c9b8a7e'), 'GET /api/v1/backup/incremental/3c9b8a7e'],
    [
      'useSnapshotFiles',
      () => useSnapshotFiles('3c9b8a7e', '/home/admin/My Docs'),
      'GET /api/v1/backup/incremental/3c9b8a7e/files?path=%2Fhome%2Fadmin%2FMy%20Docs',
    ],
  ] as [string, AcctReadHook, string][])('%s calls its path', async (_name, hook, want) => {
    const fetch = acctStubFetch()
    const { Wrapper } = acctWrap()
    const { result } = renderHook(hook, { wrapper: Wrapper })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(acctCalls(fetch)).toEqual([want])
  })

  it.each([
    ['useBackup', () => useBackup('')],
    ['useSnapshot', () => useSnapshot('')],
    ['useSnapshotFiles without an id', () => useSnapshotFiles('', '/home/admin')],
    ['useSnapshotFiles without a path', () => useSnapshotFiles('3c9b8a7e', '')],
  ] as [string, AcctReadHook][])('%s stays idle', (_name, hook) => {
    const fetch = acctStubFetch()
    const { Wrapper } = acctWrap()
    const { result } = renderHook(hook, { wrapper: Wrapper })
    expect(result.current.fetchStatus).toBe('idle')
    expect(fetch).not.toHaveBeenCalled()
  })

  it('every key starts with "backup"', () => {
    for (const key of [
      backupKeys.all,
      backupKeys.list,
      backupKeys.one('x'),
      backupKeys.exclusions,
      backupKeys.snapshots,
      backupKeys.snapshot('x'),
      backupKeys.files('x', '/'),
    ])
      expect(key[0]).toBe('backup')
  })

  it('fileUrl is the GET download path, not /backup/<name>/file', () => {
    expect(fileUrl(name)).toBe(`/api/v1/backup/file/${name}`)
    expect(fileUrl('a b')).toBe('/api/v1/backup/file/a%20b')
  })
})

describe('useBackupMutation', () => {
  it('hits each route and invalidates ["backup"]', async () => {
    const fetch = acctStubFetch({ status: 'ok', message: '' })
    const { client, Wrapper } = acctWrap()
    const invalidate = vi.spyOn(client, 'invalidateQueries')
    const { result } = renderHook(() => useBackupMutation(), { wrapper: Wrapper })
    const m = result.current
    expect(Object.keys(m).sort()).toEqual(
      [
        'schedule',
        'prepareDownload',
        'remove',
        'bulk',
        'restore',
        'saveExclusions',
        'scheduleSnapshot',
        'restoreSnapshot',
        'bulkSnapshots',
      ].sort(),
    )
    const selection = { all: false, web: ['example.com'], dns: [], mail: [], db: ['admin_wp'], udir: [], cron: true }
    const exclusions = { web: 'example.com:public_html/cache', mail: '', db: '', userdir: 'Downloads' }
    const cases: AcctMutationCase[] = [
      [() => m.schedule(), 'POST /api/v1/backup', undefined],
      [() => m.prepareDownload(name), `POST /api/v1/backup/${name}/download`, undefined],
      [() => m.remove(name), `DELETE /api/v1/backup/${name}`, undefined],
      [
        () => m.bulk({ action: 'delete', names: [name] }),
        'POST /api/v1/backup/bulk',
        { action: 'delete', names: [name] },
      ],
      [() => m.restore(name, selection), `POST /api/v1/backup/${name}/restore`, selection],
      [() => m.saveExclusions(exclusions), 'PUT /api/v1/backup/exclusions', exclusions],
      [() => m.scheduleSnapshot(), 'POST /api/v1/backup/incremental', undefined],
      [() => m.restoreSnapshot('3c9b8a7e', selection), 'POST /api/v1/backup/incremental/3c9b8a7e/restore', selection],
      [
        () => m.bulkSnapshots({ action: 'delete', names: ['3c9b8a7e'] }),
        'POST /api/v1/backup/incremental/bulk',
        { action: 'delete', names: ['3c9b8a7e'] },
      ],
    ]
    await acctRunMutations(fetch, invalidate, cases, [['backup']])
  })
})
