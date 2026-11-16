import { screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { mailAccount, mailAccountDetail, mailDomain, mailDomainDetail, mailOptions, mailRoutes } from './mailFixtures'
import { renderApp } from './renderApp'

afterEach(() => vi.unstubAllGlobals())

describe('mail fixtures', () => {
  it('builders take overrides', () => {
    expect(mailDomain('a.example', { SUSPENDED: 'yes' })).toMatchObject({ NAME: 'a.example', SUSPENDED: 'yes' })
    expect(mailDomainDetail('a.example').WEBMAIL_ALIAS).toBe('webmail.a.example')
    expect(mailAccount('info', { QUOTA: 'unlimited' })).toMatchObject({ NAME: 'info', QUOTA: 'unlimited' })
    expect(mailAccountDetail('info', 'a.example').WEBMAIL_ALIAS).toBe('webmail.a.example')
    expect(mailOptions({ canSetRate: false })).toMatchObject({
      canSetRate: false,
      webmailClients: ['roundcube', 'snappymail'],
      keyAlgos: ['ecdsa-256', 'ecdsa-384', 'rsa'],
    })
  })

  it('serve every mail GET in the contract with the dev data', () => {
    const r = mailRoutes()
    const get = (key: string) => {
      expect(r[key], key).toBeTypeOf('function')
      return r[key](undefined)
    }
    expect((get('GET /bootstrap').body as { user: { features: { mail: boolean } } }).user.features.mail).toBe(true)
    const list = get('GET /mail').body as { domains: { NAME: string; SUSPENDED: string }[] }
    expect(list.domains.map((d) => [d.NAME, d.SUSPENDED])).toEqual([
      ['example.com', 'no'],
      ['example.net', 'yes'],
    ])
    expect(get('GET /mail/options').body).toMatchObject({ webmailAlias: 'webmail', antispam: true })
    expect(get('GET /mail/example.com').body).toMatchObject({ domain: { NAME: 'example.com', SSL: 'yes' } })
    expect((get('GET /mail/example.com').body as { ssl?: unknown }).ssl).toBeTruthy()
    expect((get('GET /mail/example.net').body as { ssl?: unknown }).ssl).toBeUndefined()
    const accts = get('GET /mail/example.com/accounts').body as { accounts: { NAME: string }[] }
    expect(accts.accounts.map((a) => a.NAME)).toEqual(['info', 'sales'])
    expect(get('GET /mail/example.net/accounts').body).toMatchObject({ domain: { NAME: 'example.net' } })
    expect(get('GET /mail/example.com/accounts/info').body).toMatchObject({ autoreplyMessage: '' })
    expect(
      (get('GET /mail/example.com/accounts/sales').body as { autoreplyMessage: string }).autoreplyMessage,
    ).not.toBe('')
    const dns = get('GET /mail/example.com/dns').body as { records: { record: string }[] }
    expect(dns.records.map((x) => x.record)).toEqual([
      'mail.example.com',
      'webmail.example.com',
      'example.com',
      'example.com',
      '_dmarc',
      'mail._domainkey',
    ])
    expect(get('GET /mail/missing.example').status).toBe(404)
  })

  it('overrides replace or add routes', () => {
    const r = mailRoutes({ role: 'user' }, { 'GET /mail/options': () => ({ body: mailOptions({ webmail: false }) }) })
    expect(r['GET /mail/options'](undefined).body).toMatchObject({ webmail: false })
    expect((r['GET /bootstrap'](undefined).body as { user: { role: string } }).user.role).toBe('user')
  })
})

describe('sidebar mail entry', () => {
  it('hides Mail when the package or server has no mail', async () => {
    renderApp('/web', mailRoutes({ features: { dns: true, db: true, mail: false } }))
    await screen.findByText('Hosting')
    expect(screen.queryByRole('link', { name: 'Mail' })).toBeNull()
  })
  it('shows Mail otherwise', async () => {
    renderApp('/web', mailRoutes())
    expect(await screen.findByRole('link', { name: 'Mail' })).toBeTruthy()
  })
})
