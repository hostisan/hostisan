import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render } from '@testing-library/react'
import { createMemoryRouter, RouterProvider, useLocation, type RouteObject } from 'react-router'
import { vi } from 'vitest'
import { appRoutes, Root } from '../App'
import { RootError } from '../layout/RootError'
import type { Database, DNSDomain, DNSRecord } from '../api/hestia.gen'
import { SessionProvider } from '../session/SessionProvider'

export type Route = (body: unknown) => { status?: number; body: unknown }

/**
 * Renders the whole app at `path` over a fake API. `routes` maps "METHOD /path" to a
 * reply; requests to anything else fail the test. `extraRoutes` adds test-only routes
 * next to the app's own, under the same root.
 */
export function renderApp(path: string, routes: Record<string, Route>, extraRoutes: RouteObject[] = []) {
  const calls: { key: string; body: unknown }[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      const key = `${init?.method ?? 'GET'} ${url.replace('/api/v1', '')}`
      const body = init?.body ? JSON.parse(String(init.body)) : undefined
      calls.push({ key, body })
      const route = routes[key]
      if (!route) return new Response(JSON.stringify({ error: { code: 'not_found', message: key } }), { status: 404 })
      const reply = route(body)
      return new Response(JSON.stringify(reply.body), { status: reply.status ?? 200 })
    }),
  )
  let location = path
  function Spy() {
    location = useLocation().pathname
    return null
  }
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const router = createMemoryRouter(
    [
      {
        element: (
          <>
            <Root />
            <Spy />
          </>
        ),
        errorElement: <RootError />,
        children: [...extraRoutes, ...appRoutes],
      },
    ],
    { initialEntries: [path] },
  )
  render(
    <QueryClientProvider client={client}>
      <SessionProvider>
        <RouterProvider router={router} />
      </SessionProvider>
    </QueryClientProvider>,
  )
  return { calls, location: () => location, router }
}

export const anonymous = {
  authenticated: false,
  csrfToken: 'tok',
  appName: 'Hostisan',
  version: '1.0.0',
  language: 'en',
  languages: ['en', 'fr'],
  theme: '',
  passwordReset: true,
  hideDocs: false,
  productUrl: 'https://example.test',
  securityEmail: 'security@example.test',
}

export function signedIn(user: Record<string, unknown>) {
  return {
    ...anonymous,
    authenticated: true,
    user: {
      name: 'admin',
      displayName: 'System Administrator',
      contact: 'admin@acme-hosting.net',
      role: 'admin',
      look: '',
      readOnly: false,
      landing: '/users',
      usage: { diskUsed: '7342', diskQuota: '20480', bandwidthUsed: '100', bandwidthQuota: 'unlimited' },
      sortOrder: 'name',
      features: {
        web: true,
        dns: true,
        db: true,
        mail: true,
        cron: true,
        backup: true,
        incremental: true,
        logs: true,
        loginHistory: true,
        accessKeys: true,
        server: true,
        firewall: true,
        firewallBans: true,
        terminal: true,
        fileManager: true,
      },
      canEditDetails: true,
      canChangeTheme: true,
      canDeleteLogs: true,
      notifications: false,
      package: 'system',
      ...user,
    },
  }
}

/** A zone as the API sends it; override any field. */
export function zone(name: string, over: Partial<DNSDomain> = {}): DNSDomain {
  return {
    NAME: name,
    IP: '203.0.113.24',
    TPL: 'default',
    TTL: '14400',
    EXP: '2027-01-01',
    SOA: 'ns1.example.net',
    SERIAL: '2026010101',
    DNSSEC: 'no',
    SRC: '',
    RECORDS: '2',
    SUSPENDED: 'no',
    TIME: '10:00:00',
    DATE: '2026-01-01',
    ...over,
  }
}

export function record(id: string, over: Partial<DNSRecord> = {}): DNSRecord {
  return {
    ID: id,
    RECORD: '@',
    TYPE: 'A',
    PRIORITY: '',
    VALUE: '203.0.113.24',
    SUSPENDED: 'no',
    TIME: '10:00:00',
    DATE: '2026-01-01',
    TTL: '',
    ...over,
  }
}

/** Fake DNS API routes: two zones, example.com (active) and example.org (suspended). */
export function dnsRoutes(user: Record<string, unknown> = {}): Record<string, Route> {
  const com = zone('example.com', { DNSSEC: 'yes' })
  const org = zone('example.org', { SUSPENDED: 'yes', DATE: '2026-03-01' })
  return {
    'GET /bootstrap': () => ({ body: signedIn(user) }),
    'GET /dns': () => ({
      body: {
        zones: [com, org],
        usage: { zones: '2', zonesLimit: '5', suspended: '1', records: '13', recordsLimit: '250' },
      },
    }),
    'GET /dns/options': () => ({
      body: {
        templates: ['default'],
        ips: ['203.0.113.24'],
        nameservers: ['ns1.example.net', 'ns2.example.net'],
        defaultTemplate: 'default',
        defaultTTL: '14400',
        defaultExpiry: '2027-01-01',
        canEditTemplates: true,
        supportsDnssec: true,
        dnsSystem: 'bind9',
        adminGate: false,
      },
    }),
    'GET /dns/example.com/records': () => ({
      body: { zone: com, records: [record('1'), record('2', { RECORD: 'www', TYPE: 'CNAME', VALUE: '@' })] },
    }),
    'GET /dns/example.org/records': () => ({ body: { zone: org, records: [record('1')] } }),
    'GET /dns/example.com/dnssec': () => ({
      body: {
        zone: com,
        key: {
          RECORD: 'example.com. IN DNSKEY 257 3 13 abc',
          KEYTAG: '12345',
          FLAG: '257',
          ALGORITHM: '13',
          KEY: 'abc',
          DS: 'example.com. IN DS 12345 13 2 DEADBEEF',
        },
        algorithmLabel: '13 - ECDSAP256/SHA256',
        flagLabel: 'KSK (257)',
      },
    }),
    'GET /dns/missing.example/records': () => ({
      status: 404,
      body: { error: { code: 'not_found', message: "Error: dns domain missing.example doesn't exist" } },
    }),
  }
}

/** A database as the API sends it; override any field. */
export function database(name: string, over: Partial<Database> = {}): Database {
  return {
    DATABASE: name,
    DBUSER: 'admin_user',
    HOST: 'localhost',
    TYPE: 'mysql',
    CHARSET: 'utf8mb4',
    U_DISK: '12',
    SUSPENDED: 'no',
    TIME: '10:00:00',
    DATE: '2026-01-01',
    ...over,
  }
}

/** Fake database API routes: admin_wordpress (MySQL), admin_reports (PostgreSQL, suspended). */
export function dbRoutes(
  user: Record<string, unknown> = {},
  options: Record<string, unknown> = {},
): Record<string, Route> {
  const wp = database('admin_wordpress', { DATE: '2026-06-01' })
  const reports = database('admin_reports', { TYPE: 'pgsql', CHARSET: 'UTF8', SUSPENDED: 'yes', DATE: '2026-03-01' })
  return {
    'GET /bootstrap': () => ({ body: signedIn(user) }),
    'GET /db': () => ({
      body: { databases: [wp, reports], usage: { databases: '2', databasesLimit: '5', suspended: '1' } },
    }),
    'GET /db/options': () => ({
      body: {
        types: ['mysql', 'pgsql'],
        hosts: [{ host: 'localhost', type: 'mysql', charsets: [] }],
        mysqlCharsets: ['utf8mb4'],
        defaultCharset: { mysql: 'utf8mb4', pgsql: 'UTF8' },
        prefix: 'admin_',
        maxUserLength: 32,
        phpMyAdminUrl: 'https://panel.example/phpmyadmin/',
        phpPgAdminUrl: 'https://panel.example/phppgadmin/',
        ssoEnabled: true,
        adminGate: false,
        ssoHint: '',
        ...options,
      },
    }),
  }
}
