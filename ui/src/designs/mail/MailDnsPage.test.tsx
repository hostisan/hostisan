import { cleanup, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { mailRoutes } from '../../test/mailFixtures'
import { renderApp } from '../../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('mail DNS page', () => {
  it('lists the records from the API with copy buttons', async () => {
    renderApp('/mail/example.com/dns', mailRoutes())
    expect(await screen.findByText('mail.example.com')).toBeTruthy()
    expect(screen.getByText('webmail.example.com')).toBeTruthy()
    expect(screen.getByText('mail.example.com.')).toBeTruthy() // the MX value
    expect(screen.getByText('v=spf1 a mx ip4:203.0.113.11 -all')).toBeTruthy()
    expect(screen.getByText('v=DMARC1; p=quarantine; pct=100')).toBeTruthy()
    expect(screen.getByText('mail._domainkey')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Copy mail.example.com' })).toBeTruthy()
    expect(screen.getAllByRole('button', { name: 'Copy TXT value' }).length).toBe(3)
    expect(screen.getByText('10')).toBeTruthy() // the MX priority
    expect(screen.getByText('3600')).toBeTruthy() // the DKIM TTL
  })

  it('says DKIM is disabled when the domain has it off', async () => {
    renderApp('/mail/example.net/dns', mailRoutes())
    expect(await screen.findByText(/DKIM is disabled for this domain/)).toBeTruthy()
    cleanup()
    renderApp('/mail/example.com/dns', mailRoutes())
    await screen.findByText('mail._domainkey')
    expect(screen.queryByText(/DKIM is disabled for this domain/)).toBeNull()
  })

  it('never fetches the domain detail, which carries the SSL key', async () => {
    const app = renderApp('/mail/example.net/dns', mailRoutes())
    expect(await screen.findByText(/DKIM is disabled for this domain/)).toBeTruthy()
    expect(app.calls.map((c) => c.key)).not.toContain('GET /mail/example.net')
  })

  it('shows the zone banner when the server hosts the DNS', async () => {
    const routes = mailRoutes(
      {},
      {
        'GET /dns': () => ({
          body: {
            zones: [
              {
                NAME: 'example.com',
                IP: '203.0.113.24',
                TPL: 'default',
                TTL: '14400',
                EXP: '2027-01-01',
                SOA: 'ns1.example.net',
                SERIAL: '1',
                DNSSEC: 'no',
                SRC: '',
                RECORDS: '2',
                SUSPENDED: 'no',
                TIME: '10:00:00',
                DATE: '2026-01-01',
              },
            ],
            usage: { zones: '1', zonesLimit: '5', records: '2', recordsLimit: '250', suspended: '0' },
          },
        }),
      },
    )
    renderApp('/mail/example.com/dns', routes)
    expect(await screen.findByText("example.com uses this server's DNS")).toBeTruthy()
    cleanup()
    renderApp('/mail/example.com/dns', mailRoutes())
    expect(await screen.findByText('DNS is hosted elsewhere')).toBeTruthy()
  })

  it('shows the error for an unknown domain', async () => {
    renderApp('/mail/missing.example/dns', mailRoutes())
    expect(await screen.findByText(/doesn't exist/)).toBeTruthy()
  })
})
