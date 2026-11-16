import { screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { WebAppForm, WebApps, WebDomains, WebDomainView, WebLog, WebOptions } from '../api/types.gen'
import { renderApp } from './renderApp'
import {
  webApp,
  webAppForm,
  webDomain,
  webDomainDetail,
  webDomainView,
  webOptions,
  webRoutes,
  webSsl,
} from './webFixtures'

afterEach(() => vi.unstubAllGlobals())

describe('web fixtures', () => {
  it('builders take overrides', () => {
    expect(webDomain('a.example', { SUSPENDED: 'yes' })).toMatchObject({ NAME: 'a.example', SUSPENDED: 'yes' })
    expect(webDomain('a.example').DOCUMENT_ROOT).toBe('/home/admin/web/a.example/public_html/')
    expect(webDomainDetail('a.example', { REDIRECT_CODE: '302' })).toMatchObject({
      NAME: 'a.example',
      REDIRECT_CODE: '302',
      SSL_KEY_ALGO: 'ecdsa-256',
    })
    expect(webSsl('a.example').SUBJECT).toBe('a.example')
    expect(webOptions({ nginx: true })).toMatchObject({
      nginx: true,
      keyAlgos: ['ecdsa-256', 'ecdsa-384', 'rsa'],
      statsTypes: ['none', 'awstats'],
    })
    expect(webApp('grav', { installable: false })).toMatchObject({ id: 'grav', installable: false })
    expect(webAppForm('grav', { database: null }).database).toBeNull()
    const view = webDomainView(webDomainDetail('a.example', { ALIAS: 'www.a.example,b.a.example' }))
    expect(view.aliases).toEqual(['www.a.example', 'b.a.example'])
  })

  it('serve every web GET in the contract with the dev data', () => {
    const r = webRoutes()
    const get = (key: string) => {
      expect(r[key], key).toBeTypeOf('function')
      return r[key](undefined)
    }
    expect((get('GET /bootstrap').body as { user: { features: { web: boolean } } }).user.features.web).toBe(true)
    const list = get('GET /web').body as WebDomains
    expect(list.domains.map((d) => [d.NAME, d.SUSPENDED, d.SSL])).toEqual([
      ['example.com', 'no', 'yes'],
      ['example.net', 'yes', 'no'],
    ])
    expect(list.displayIps).toEqual({ '203.0.113.10': '203.0.113.10' })
    expect(list.usage).toMatchObject({ domains: '2', suspended: '1', aliasesLimit: 'unlimited' })
    expect((get('GET /web/options').body as WebOptions).ips[0]).toEqual({ ip: '203.0.113.10', display: '203.0.113.10' })

    const com = get('GET /web/example.com').body as WebDomainView
    expect(com).toMatchObject({
      domain: { NAME: 'example.com', LETSENCRYPT: 'yes' },
      aliases: ['www.example.com', 'blog.example.com'],
      keyAlgo: 'ecdsa-256',
      ftp: [{ name: 'dev', path: '/public_html' }],
      redirect: { enabled: false, code: '301' },
    })
    expect(com.ssl?.SUBJECT).toBe('example.com')
    expect((get('GET /web/example.net').body as WebDomainView).ssl).toBeUndefined()

    expect((get('GET /web/example.com/logs?type=access').body as WebLog).lines.length).toBeGreaterThan(0)
    expect(get('GET /web/example.com/logs?type=error').body).toMatchObject({ type: 'error' })
    expect(get('GET /web/example.net/logs?type=access').body).toEqual({ type: 'access', lines: [] })

    const apps = (get('GET /web/example.com/apps').body as WebApps).apps
    expect(apps.length).toBeGreaterThan(1)
    expect(apps.at(-1)?.id).toBe('osticket')
    expect((get('GET /web/example.com/apps/wordpress').body as WebAppForm).app.name).toBe('WordPress')

    for (const key of ['GET /web/missing.example', 'GET /web/missing.example/apps']) {
      expect(get(key).status).toBe(404)
    }
  })

  it("serves bob's view with the NAT address", () => {
    const r = webRoutes({ name: 'bob', role: 'user' })
    const list = r['GET /web'](undefined).body as WebDomains
    expect(list.domains.map((d) => d.NAME)).toEqual(['bakery.example'])
    expect(list.displayIps).toEqual({ '10.0.0.5': '198.51.100.7' })
    const view = r['GET /web/bakery.example'](undefined).body as WebDomainView
    expect(view).toMatchObject({
      displayIp: '198.51.100.7',
      keyAlgo: 'rsa',
      docroot: { enabled: true, domain: 'bakery.example', folder: 'shop' },
      redirect: { enabled: true, mode: 'www', target: 'www.bakery.example', code: '301' },
    })
    expect((r['GET /web/options'](undefined).body as WebOptions).canEditTemplates).toBe(false)
  })

  it('overrides replace or add routes', () => {
    const r = webRoutes({ role: 'user' }, { 'GET /web/options': () => ({ body: webOptions({ purge: false }) }) })
    expect(r['GET /web/options'](undefined).body).toMatchObject({ purge: false })
    expect((r['GET /bootstrap'](undefined).body as { user: { role: string } }).user.role).toBe('user')
  })
})

describe('sidebar web entry', () => {
  it('shows Web with the web feature on', async () => {
    renderApp('/dns', webRoutes())
    expect(await screen.findByRole('link', { name: 'Web' })).toBeTruthy()
  })
})
