import { cleanup, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderApp } from '../../test/renderApp'
import { webDomainDetail, webDomainView, webRoutes, webSsl } from '../../test/webFixtures'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('SSL certificate page', () => {
  it('shows the certificate details with the copy and download blocks', async () => {
    renderApp('/web/example.com/ssl', webRoutes())
    expect(await screen.findByText('Certificate files')).toBeTruthy()
    expect(screen.getByText('ecdsa-with-SHA384')).toBeTruthy()
    expect(screen.getAllByText('SSL certificate').length).toBeGreaterThan(1)
    expect(screen.getByText('SSL private key')).toBeTruthy()
    expect(screen.getByText('SSL certificate authority / intermediate')).toBeTruthy()
    expect(screen.getAllByRole('button', { name: /^example\.com\.(crt|key|csr|ca)$/ }).length).toBe(3)
    expect(screen.getByText(/MIIFdevLetsEncryptLeaf/)).toBeTruthy()
    expect(screen.getByText(/renews this certificate automatically/)).toBeTruthy()
  })

  it('shows the empty state when SSL is off', async () => {
    renderApp('/web/example.net/ssl', webRoutes())
    expect(await screen.findByText('SSL is not enabled for this domain')).toBeTruthy()
  })

  it('shows the empty state when the domain has no certificate file', async () => {
    const view = webDomainView(webDomainDetail('example.com'), { ssl: undefined })
    renderApp('/web/example.com/ssl', webRoutes({}, { 'GET /web/example.com': () => ({ body: view }) }))
    expect(await screen.findByText('SSL is not enabled for this domain')).toBeTruthy()
  })

  it('shows a domain that does not exist as not found', async () => {
    renderApp('/web/missing.example/ssl', webRoutes())
    expect(await screen.findByText('Web domain not found')).toBeTruthy()
  })

  it('marks an expired certificate', async () => {
    const view = webDomainView(
      webDomainDetail('example.com'),
      {},
      webSsl('example.com', { NOT_AFTER: 'Jan  1 00:00:00 2020 GMT' }),
    )
    renderApp('/web/example.com/ssl', webRoutes({}, { 'GET /web/example.com': () => ({ body: view }) }))
    expect(await screen.findByText('Expired')).toBeTruthy()
  })
})
