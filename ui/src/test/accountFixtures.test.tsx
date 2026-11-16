import { describe, expect, it } from 'vitest'
import {
  accessKey,
  authLogEntry,
  backup,
  backupRoutes,
  cronJob,
  cronRoutes,
  keysRoutes,
  logEntry,
  notification,
  pkg,
  profileRoutes,
  recoveryRoutes,
  snapshot,
  sshKey,
  statsMonth,
  statsRoutes,
  userDetail,
  userEntry,
  usersRoutes,
} from './accountFixtures'

const body = (routes: Record<string, (b: unknown) => { status?: number; body: unknown }>, key: string) => {
  expect(routes[key], key).toBeTypeOf('function')
  return routes[key](undefined)
}

describe('account fixtures', () => {
  it('builders take overrides', () => {
    expect(userEntry('bob', { SUSPENDED: 'yes' })).toMatchObject({ USER: 'bob', SUSPENDED: 'yes' })
    expect(userDetail('bob', { THEME: 'dark' })).toMatchObject({ USER: 'bob', THEME: 'dark' })
    expect(pkg('starter', { SHELL: 'bash' })).toMatchObject({ NAME: 'starter', SHELL: 'bash', RATE_LIMIT: '200' })
    expect(cronJob('3', { SUSPENDED: 'yes' })).toMatchObject({ JOB: '3', SUSPENDED: 'yes' })
    expect(backup('a.tar', { SIZE: '1' })).toMatchObject({ NAME: 'a.tar', SIZE: '1' })
    expect(snapshot('3c9b8a7e').id).toHaveLength(64)
    expect(sshKey('k', { KEY: 'x' })).toEqual({ ID: 'k', KEY: 'x' })
    expect(accessKey('aB3dEfGh1JkLmN0pQrSt', { USER: 'bob' })).toMatchObject({ USER: 'bob' })
    expect(notification('1', { ACK: 'yes' })).toMatchObject({ ID: '1', ACK: 'yes' })
    expect(logEntry('1', { LEVEL: 'Error' })).toMatchObject({ ID: '1', LEVEL: 'Error' })
    expect(authLogEntry('1', { STATUS: 'failed' })).toMatchObject({ ID: '1', STATUS: 'failed' })
    expect(statsMonth('2026-10-31', { uUsers: '4' })).toMatchObject({ date: '2026-10-31', uUsers: '4' })
  })

  it('serve the dev data', () => {
    const users = body(usersRoutes(), 'GET /users').body as { users: { USER: string }[]; rootUser: string }
    expect(users.users.map((u) => u.USER)).toEqual(['admin', 'bob', 'susp', 'ops'])
    expect(users.rootUser).toBe('admin')
    const pkgs = body(usersRoutes(), 'GET /packages').body as { packages: Record<string, string>[] }
    expect(pkgs.packages.map((p) => p.NAME)).toEqual(['starter-plus', 'starter', 'default', 'system'])
    expect(pkgs.packages[0]).not.toHaveProperty('RATE_LIMIT')
    expect(body(usersRoutes(), 'GET /packages/starter-plus').body).toMatchObject({ NAME: 'starter-plus' })
    expect(body(usersRoutes(), 'GET /packages/missing').status).toBe(404)

    expect(body(profileRoutes(), 'GET /account').body).toMatchObject({ user: { USER: 'admin' }, self: true })
    expect(body(profileRoutes(), 'GET /users/bob').body).toMatchObject({ twofa: true, self: false })
    expect(body(recoveryRoutes(), 'GET /bootstrap').body).toMatchObject({ authenticated: false, passwordReset: true })
    expect(body(recoveryRoutes({ passwordReset: false }), 'GET /bootstrap').body).toMatchObject({
      passwordReset: false,
    })

    const keys = keysRoutes()
    expect((body(keys, 'GET /account/ssh-keys').body as { keys: unknown[] }).keys).toHaveLength(2)
    expect((body(keys, 'GET /account/access-keys').body as { keys: unknown[] }).keys).toHaveLength(3)
    expect(body(keys, 'GET /account/access-keys/aB3dEfGh1JkLmN0pQrSt').body).toMatchObject({ USER: 'admin' })
    const notes = body(keys, 'GET /notifications').body as { items: { PRIORITY: string }[] }
    expect(notes.items.map((n) => n.PRIORITY)).toEqual(['1', '0'])
    const log = body(keys, 'GET /logs').body as { entries: { ID: string }[] }
    expect(log.entries.map((e) => e.ID)).toEqual(['41', '40', '39', '38', '37'])
    expect(body(keys, 'GET /logs?user=system').body).toMatchObject({ user: 'system' })
    expect((body(keys, 'GET /logs/auth').body as { entries: unknown[] }).entries).toHaveLength(3)
    expect(JSON.stringify(body(keys, 'GET /logs/auth').body)).not.toContain('SESSION')

    const stats = statsRoutes()
    expect(body(stats, 'GET /stats').body).toMatchObject({ overall: true, users: ['admin', 'bob', 'susp', 'ops'] })
    const bob = body(stats, 'GET /stats?user=bob').body as { months: { date: string; bandwidth: string }[] }
    expect(bob.months.map((m) => m.date)).toEqual(['2026-10-31', '2026-09-30', '2026-08-31'])
    expect(bob.months.some((m) => m.bandwidth === 'unlimited')).toBe(true)

    const cron = cronRoutes()
    expect((body(cron, 'GET /cron').body as { jobs: unknown[] }).jobs).toHaveLength(2)
    expect(body(cron, 'GET /cron/options').body).toMatchObject({ timezone: 'UTC' })
    expect(body(cron, 'GET /cron/99').status).toBe(404)

    const b = backupRoutes()
    expect((body(b, 'GET /backup').body as { backups: { NAME: string }[] }).backups.map((x) => x.NAME)).toEqual([
      'admin.2026-10-20_03-00-01.tar',
      'admin.2026-10-19_03-00-02.tar',
    ])
    expect(body(b, 'GET /backup/admin.2026-10-19_03-00-02.tar').body).toMatchObject({ backup: { SIZE: '312' } })
    expect(body(b, 'GET /backup/exclusions').body).toMatchObject({ text: { mail: 'example.com:info:support' } })
    expect((body(b, 'GET /backup/incremental').body as { snapshots: unknown[] }).snapshots).toHaveLength(2)
    expect(body(b, 'GET /backup/incremental/3c9b8a7e').body).toMatchObject({
      id: '3c9b8a7e',
      detail: { TYPE: 'restic' },
    })
    expect(body(b, 'GET /backup/incremental/3c9b8a7e/files?path=%2Fhome%2Fadmin').body).toMatchObject({ parent: '' })
  })

  it('take bootstrap and route overrides', () => {
    const r = backupRoutes(
      { role: 'user', name: 'bob' },
      { 'POST /backup': () => ({ body: { status: 'ok', message: 'm' } }) },
    )
    expect(body(r, 'GET /bootstrap').body).toMatchObject({ user: { role: 'user', name: 'bob' } })
    expect(body(r, 'POST /backup').body).toMatchObject({ message: 'm' })
    for (const routes of [usersRoutes(), profileRoutes(), keysRoutes(), statsRoutes(), cronRoutes()]) {
      const boot = body(routes, 'GET /bootstrap').body as { user: { features: Record<string, boolean> } }
      expect(Object.values(boot.user.features).every(Boolean)).toBe(true)
    }
  })
})
