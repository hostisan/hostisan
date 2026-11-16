import { describe, expect, it } from 'vitest'
import {
  firewallRule,
  firewallRules,
  ipRow,
  JOB_ID,
  jobDone,
  jobFailed,
  serverRoutes,
  service,
  settingsView,
  sysInfo,
} from './serverFixtures'

describe('server fixtures', () => {
  it('builders take overrides', () => {
    expect(sysInfo({ RELEASE: 'beta' })).toMatchObject({ HOSTNAME: 'server1.acme-hosting.net', RELEASE: 'beta' })
    expect(service('ssh', { state: 'stopped' })).toMatchObject({ name: 'ssh', editor: 'ssh', state: 'stopped' })
    expect(settingsView({ canEditProtection: false }).canEditProtection).toBe(false)
    expect(firewallRule('3', { suspended: true })).toMatchObject({ id: '3', suspended: true, expect: 'expect-3' })
    expect(ipRow('10.0.0.5', { nat: '198.51.100.7' }).display).toBe('198.51.100.7')
    expect(ipRow('203.0.113.10').display).toBe('203.0.113.10')
    expect(jobDone({ n: 1 })).toMatchObject({ state: 'done', result: { n: 1 }, error: null })
    expect(jobFailed('Error: boom')).toMatchObject({ state: 'failed', error: { message: 'Error: boom' } })
  })

  it('lists the default rules in evaluation order with the protected ones marked', () => {
    const rules = firewallRules()
    expect(rules.map((r) => r.id)).toEqual(['10', '9', '8', '7', '6', '5', '4', '3', '2', '1'])
    expect(rules.filter((r) => r.protected).map((r) => [r.id, r.protected])).toEqual([
      ['10', 'ssh'],
      ['2', 'panel'],
    ])
  })

  it('serve every server GET the hooks call, and overrides win', () => {
    const r = serverRoutes({ 'GET /server/preview': () => ({ body: { rows: [] } }) })
    const paths = [
      'GET /bootstrap',
      'GET /server/services',
      'GET /server/editors/nginx',
      'GET /server/settings',
      'GET /server/status/cpu',
      'GET /server/status/db',
      'GET /server/graphs',
      'GET /server/graphs/net_eth0?period=weekly',
      'GET /server/updates',
      'GET /server/white-label',
      'GET /server/preview',
      'GET /server/ips',
      'GET /server/ips/options',
      'GET /server/ips/10.0.0.5',
      'GET /server/firewall',
      'GET /server/firewall/rules/2',
      'GET /server/firewall/bans',
      'GET /server/firewall/ipsets',
      `GET /server/jobs/${JOB_ID}`,
    ]
    for (const p of paths) expect(r[p], p).toBeTypeOf('function')
    expect(r['GET /server/preview'](undefined).body).toEqual({ rows: [] })
    const bans = r['GET /server/firewall/bans'](undefined).body as { rows: { IP: string; CHAIN: string }[] }
    expect(bans.rows.filter((b) => b.IP === '203.0.113.9').map((b) => b.CHAIN)).toEqual(['MAIL', 'SSH'])
    const bootstrap = r['GET /bootstrap'](undefined).body as { user: { features: Record<string, boolean> } }
    expect(bootstrap.user.features).toMatchObject({ server: true, firewall: true, firewallBans: true })
  })
})
