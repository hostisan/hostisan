import { describe, expect, it } from 'vitest'
import {
  RESTORE_JOB_ID,
  restoreJobDone,
  restoreJobFailed,
  searchAccountRow,
  searchHit,
  searchRecordRow,
  searchResults,
  terminalToken,
  toolsRoutes,
} from './toolsFixtures'

describe('tools fixtures', () => {
  it('builders take overrides', () => {
    expect(searchHit({ user: 'admin' })).toMatchObject({ user: 'admin', linked: true })
    expect(terminalToken().token).toMatch(/^[0-9a-f]{32}$/)
    expect(restoreJobDone(['/home/bob/a'])).toMatchObject({ state: 'done', result: { restored: ['/home/bob/a'] } })
    // A failed job has no result: its steps are every path tried, the last one failed.
    expect(restoreJobFailed('/home/bob/b', ['/home/bob/a'])).toMatchObject({
      state: 'failed',
      steps: ['/home/bob/a', '/home/bob/b'],
      error: { code: 'command_failed', args: ['/home/bob/b'] },
      result: null,
    })
  })

  it('mirrors the dev search fixture for "example": the core order, no record or account rows', () => {
    const r = searchResults()
    expect(r.rows.map((x) => `${x.user} ${x.type} ${x.key} ${x.result}`)).toEqual([
      'admin web DOMAIN example.net',
      'bob web DOMAIN bakery.example',
      'bob dns DOMAIN bakery.example',
      'bob mail DOMAIN bakery.example',
      'bob cron JOB wget -q -O /dev/null https://bakery.example/',
    ])
    expect(r.rows[0]).toMatchObject({ suspended: true, route: '/web/example.net', loginAs: false })
    expect(r.rows[1].alias).toBe('www.bakery.example')
    expect(r.rows[4].route).toBe('/cron/2')
    expect(r.rows.filter((x) => x.loginAs).map((x) => x.user)).toEqual(Array(4).fill('bob'))
  })

  it('has the record and account rows of the "www" and "info" searches', () => {
    // The core takes a record's zone from the line parsed before it (SR33): bob's web
    // domain bakery.example, whose record 3 isn't "www", so the row is unverified.
    expect(searchRecordRow()).toMatchObject({
      key: 'RECORD',
      result: 'www.bakery.example',
      link: '3',
      parent: 'bakery.example',
      linked: false,
    })
    expect(searchRecordRow({ result: 'www.example.com', parent: 'example.com', linked: true })).toMatchObject({
      route: '/dns/example.com/records/3',
      linked: true,
    })
    expect(searchAccountRow()).toMatchObject({ key: 'ACCOUNT', result: 'info@bakery.example', route: '/mail/bakery.example/accounts/info' })
  })

  it('serves every tools route, and overrides win', () => {
    const r = toolsRoutes({ 'POST /files/handoff': () => ({ status: 409, body: {} }) })
    for (const p of [
      'GET /bootstrap',
      'GET /search?q=example',
      'POST /terminal/token',
      'POST /files/handoff',
      `GET /backup/restore-jobs/${RESTORE_JOB_ID}`,
      'GET /search?q=www',
      'GET /search?q=info',
    ])
      expect(r[p], p).toBeTypeOf('function')
    expect(r['POST /files/handoff'](undefined).status).toBe(409)
    const boot = r['GET /bootstrap'](undefined).body as { user: { features: Record<string, boolean>; package: string } }
    expect(boot.user.features.terminal && boot.user.features.fileManager).toBe(true)
    expect(boot.user.package).toBe('system')
  })
})
