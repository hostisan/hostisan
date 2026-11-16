import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { WebGenerateSSLRequest } from '../../api/types.gen'
import { renderApp, type Route } from '../../test/renderApp'
import { webRoutes } from '../../test/webFixtures'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const generated: Route = () => ({
  body: {
    crt: '-----BEGIN CERTIFICATE-----\nCRTBODY\n-----END CERTIFICATE-----',
    key: '-----BEGIN PRIVATE KEY-----\nKEYBODY\n-----END PRIVATE KEY-----',
    csr: '-----BEGIN CERTIFICATE REQUEST-----\nCSRBODY\n-----END CERTIFICATE REQUEST-----',
  },
})
const routes = (over: Record<string, Route> = {}) => webRoutes({}, { 'POST /web/generate-ssl': generated, ...over })
const field = (label: RegExp | string) => screen.findByLabelText(label) as Promise<HTMLInputElement>

describe('generate self-signed SSL certificate', () => {
  it('starts from the defaults and the domain name', async () => {
    renderApp('/web/example.com/ssl/generate', routes())
    expect((await field('Domain')).value).toBe('example.com')
    expect((await field('Country')).value).toBe('US')
    expect((await field(/^State/)).value).toBe('California')
    expect((await field(/^City/)).value).toBe('San Francisco')
    expect((await field('Organization')).value).toBe('MyCompany Inc')
    expect(screen.getByRole('combobox', { name: /Key type/ }).textContent).toContain('ECDSA P-256')
    await waitFor(() =>
      expect((screen.getByLabelText('Aliases') as HTMLTextAreaElement).value).toContain('www.example.com'),
    )
  })

  it('works without a web domain of that name', async () => {
    const app = renderApp('/web/new-site.example/ssl/generate', routes())
    expect((await field('Domain')).value).toBe('new-site.example')
    fireEvent.click(screen.getByRole('button', { name: 'Generate' }))
    expect(await screen.findByText(/CRTBODY/)).toBeTruthy()
    const sent = app.calls.find((c) => c.key === 'POST /web/generate-ssl')!.body as WebGenerateSSLRequest
    expect(sent).toMatchObject({
      domain: 'new-site.example',
      country: 'US',
      state: 'California',
      locality: 'San Francisco',
      org: 'MyCompany Inc',
      keyAlgo: 'ecdsa-256',
      email: '',
    })
  })

  it('shows the certificate, key and CSR with downloads', async () => {
    renderApp('/web/example.com/ssl/generate', routes())
    fireEvent.click(await screen.findByRole('button', { name: 'Generate' }))
    expect(await screen.findByText(/CRTBODY/)).toBeTruthy()
    expect(screen.getByText(/KEYBODY/)).toBeTruthy()
    expect(screen.getByText(/CSRBODY/)).toBeTruthy()
    expect(screen.getAllByRole('button', { name: /^example\.com\.(crt|key|csr|ca)$/ }).length).toBe(3)
    expect(await screen.findByText('Certificate has been generated successfully.')).toBeTruthy() // Hestia's msgid (ruling 19)
  })

  it('sets the back link from ?back=', async () => {
    renderApp('/web/example.com/ssl/generate?back=/mail/example.com', routes())
    await field('Domain')
    const back = screen.getAllByRole('link').find((a) => a.getAttribute('href') === '/mail/example.com')
    expect(back).toBeTruthy()
  })

  it('ignores a back target outside the panel', async () => {
    renderApp('/web/example.com/ssl/generate?back=//evil.example', routes())
    await field('Domain')
    expect(screen.getAllByRole('link').some((a) => a.getAttribute('href')?.includes('evil'))).toBe(false)
  })

  it('shows the API error and keeps the form', async () => {
    renderApp(
      '/web/example.com/ssl/generate',
      routes({
        'POST /web/generate-ssl': () => ({
          status: 400,
          body: { error: { code: 'invalid', message: 'Invalid %s', args: ['Organization'] } },
        }),
      }),
    )
    fireEvent.click(await screen.findByRole('button', { name: 'Generate' }))
    expect((await screen.findAllByText('Invalid Organization')).length).toBeGreaterThan(0)
    expect((await field('Domain')).value).toBe('example.com')
  })

  it('marks the field the API names', async () => {
    renderApp(
      '/web/example.com/ssl/generate',
      routes({
        'POST /web/generate-ssl': () => ({
          status: 400,
          body: { error: { code: 'invalid', message: 'Invalid %s', args: ['Organization'] } },
        }),
      }),
    )
    fireEvent.click(await screen.findByRole('button', { name: 'Generate' }))
    // once in the banner and once on the field the API named, and nowhere else
    expect(await screen.findAllByText('Invalid Organization')).toHaveLength(2)
  })
})
