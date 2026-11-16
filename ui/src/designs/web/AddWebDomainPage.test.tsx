import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderApp, type Route } from '../../test/renderApp'
import { webOptions, webRoutes } from '../../test/webFixtures'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const bob = { name: 'bob', role: 'user' }
const ok: Route = () => ({ body: { status: 'ok' } })
const type = (domain: string) =>
  fireEvent.change(screen.getByRole('textbox', { name: /Domain/ }), { target: { value: domain } })
const submit = () => fireEvent.click(screen.getByRole('button', { name: 'Add web domain' }))

describe('add web domain', () => {
  it('asks an admin to confirm before showing the form', async () => {
    renderApp('/web/new', webRoutes())
    expect(await screen.findByText('You are adding a web domain to the admin account')).toBeTruthy()
    expect((screen.getByRole('button', { name: 'Add web domain' }) as HTMLButtonElement).disabled).toBe(true)
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
    await waitFor(() => expect((screen.getByRole('textbox', { name: /Domain/ }) as HTMLInputElement).disabled).toBe(false))
  })

  it('shows a standard user the form straight away', async () => {
    renderApp('/web/new', webRoutes(bob))
    await screen.findByRole('textbox', { name: /Domain/ })
    expect(screen.queryByText('You are adding a web domain to the admin account')).toBeNull()
  })

  it('shows an inline empty state when the account has no IP address', async () => {
    renderApp(
      '/web/new',
      webRoutes(bob, { 'GET /web/options': () => ({ body: webOptions({ ips: [], adminGate: false }) }) }),
    )
    expect(await screen.findByText('No IP address available')).toBeTruthy()
    expect((screen.getByRole('button', { name: 'Add web domain' }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('labels the IP select with the display address and selects the first', async () => {
    renderApp('/web/new', webRoutes(bob))
    const select = await screen.findByRole('combobox', { name: 'IP address' })
    expect(select.textContent).toContain('198.51.100.7')
    expect(select.textContent).not.toContain('10.0.0.5')
  })

  it('offers the DNS and Mail boxes on, only when the server offers them, with no advanced block', async () => {
    renderApp('/web/new', webRoutes(bob))
    const dns = (await screen.findByRole('checkbox', { name: /DNS/ })) as HTMLInputElement
    expect(dns.getAttribute('aria-checked') ?? String(dns.checked)).toBe('true')
    expect(screen.getByRole('checkbox', { name: /Mail/ }).getAttribute('aria-checked')).toBe('true')
    expect(screen.queryByText('Advanced options')).toBeNull()
    cleanup()
    renderApp(
      '/web/new',
      webRoutes(bob, { 'GET /web/options': () => ({ body: webOptions({ dns: false, mail: false, adminGate: false }) }) }),
    )
    await screen.findByRole('textbox', { name: /Domain/ })
    expect(screen.queryByRole('checkbox', { name: /DNS/ })).toBeNull()
    expect(screen.queryByRole('checkbox', { name: /Mail/ })).toBeNull()
  })

  it('posts exactly {domain, ip, dns, mail} and links the success toast to the new domain', async () => {
    const app = renderApp('/web/new', webRoutes(bob, { 'POST /web': ok }))
    await screen.findByRole('textbox', { name: /Domain/ })
    type('Fresh.example')
    fireEvent.click(screen.getByRole('checkbox', { name: /Mail/ }))
    submit()
    await waitFor(() => expect(app.calls.some((c) => c.key === 'POST /web')).toBe(true))
    expect(app.calls.find((c) => c.key === 'POST /web')?.body).toEqual({
      domain: 'Fresh.example',
      ip: '10.0.0.5',
      dns: true,
      mail: false,
    })
    const link = await screen.findByRole('link', { name: /fresh\.example/ })
    expect(link.getAttribute('href')).toBe('/web/fresh.example')
    await waitFor(() => expect(app.location()).toBe('/web'))
  })

  it("sends false for a box that isn't offered", async () => {
    const app = renderApp(
      '/web/new',
      webRoutes(bob, {
        'GET /web/options': () => ({ body: webOptions({ dns: false, adminGate: false, mail: true }) }),
        'POST /web': ok,
      }),
    )
    await screen.findByRole('textbox', { name: /Domain/ })
    type('a.example')
    submit()
    await waitFor(() => expect(app.calls.some((c) => c.key === 'POST /web')).toBe(true))
    expect(app.calls.find((c) => c.key === 'POST /web')?.body).toMatchObject({ dns: false, mail: true })
  })

  it("shows Hestia's error and stays on the form", async () => {
    const app = renderApp(
      '/web/new',
      webRoutes(bob, {
        'POST /web': () => ({
          status: 400,
          body: { error: { code: 'invalid', message: 'Error: invalid ip format :: 10.0.0.9' } },
        }),
      }),
    )
    await screen.findByRole('textbox', { name: /Domain/ })
    type('a.example')
    submit()
    expect(await screen.findByText('Error: invalid ip format :: 10.0.0.9')).toBeTruthy()
    expect(app.location()).toBe('/web/new')
  })
})
