import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { dnsRoutes, renderApp, zone, type Route } from '../../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const optionsWith = (over: Record<string, unknown>): Route => {
  const base = dnsRoutes()['GET /dns/options']
  return (b) => {
    const r = base(b)
    return { body: { ...(r.body as object), ...over } }
  }
}

async function fillDomain(value: string) {
  fireEvent.change(await screen.findByLabelText(/^Domain/), { target: { value } })
}

describe('add DNS zone', () => {
  it('posts the defaults from the options, then goes to the list with the normalised name', async () => {
    const routes = dnsRoutes()
    routes['POST /dns'] = () => ({ body: { status: 'ok' } })
    const app = renderApp('/dns/new', routes)
    await fillDomain('WWW.Acme.COM')
    fireEvent.click(screen.getByRole('button', { name: 'Add DNS domain' }))
    await waitFor(() => expect(app.location()).toBe('/dns'))
    expect(app.calls.find((c) => c.key === 'POST /dns')?.body).toEqual({
      domain: 'WWW.Acme.COM',
      ip: '203.0.113.24',
      template: 'default',
      dnssec: false,
      exp: '2027-01-01',
      ttl: '14400',
      nameservers: ['ns1.example.net', 'ns2.example.net'],
    })
    expect(await screen.findByText('DNS zone acme.com has been created successfully.')).toBeTruthy()
  })

  it('sends the domain without surrounding whitespace', async () => {
    const routes = dnsRoutes()
    routes['POST /dns'] = () => ({ body: { status: 'ok' } })
    const app = renderApp('/dns/new', routes)
    await fillDomain('  acme.com ')
    fireEvent.click(screen.getByRole('button', { name: 'Add DNS domain' }))
    await waitFor(() => expect(app.location()).toBe('/dns'))
    expect(app.calls.find((c) => c.key === 'POST /dns')?.body).toMatchObject({ domain: 'acme.com' })
  })

  it('fetches no zone on the add page', async () => {
    const app = renderApp('/dns/new', dnsRoutes())
    await screen.findByLabelText(/^Domain/)
    expect(app.calls.map((c) => c.key)).not.toContain('GET /dns/')
  })

  it("shows Hestia's message in the banner and keeps the typed values on failure", async () => {
    const routes = dnsRoutes()
    routes['POST /dns'] = () => ({
      status: 422,
      body: { error: { code: 'command_failed', message: 'Error: zone created\nError: record failed' } },
    })
    const app = renderApp('/dns/new', routes)
    await fillDomain('acme.com')
    fireEvent.click(screen.getByRole('button', { name: 'Add DNS domain' }))
    const banner = await screen.findByText(/Error: record failed/)
    expect(banner.closest('.whitespace-pre-line')).toBeTruthy()
    expect((screen.getByLabelText(/^Domain/) as HTMLInputElement).value).toBe('acme.com')
    expect(app.location()).toBe('/dns/new')
  })

  it('shows the admin gate first and the form after Continue', async () => {
    renderApp('/dns/new', { ...dnsRoutes(), 'GET /dns/options': optionsWith({ adminGate: true }) })
    expect(await screen.findByText("You're signed in as admin")).toBeTruthy()
    expect(screen.queryByLabelText(/^Domain/)).toBeNull()
    expect(screen.queryByRole('button', { name: 'Add DNS domain' })).toBeNull()
    expect(screen.getByRole('link', { name: 'Add user' })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
    expect(await screen.findByLabelText(/^Domain/)).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Add DNS domain' })).toBeTruthy()
  })

  it('hides the template select without canEditTemplates and sends no template', async () => {
    const routes: Record<string, Route> = {
      ...dnsRoutes(),
      'GET /dns/options': optionsWith({ canEditTemplates: false }),
    }
    routes['POST /dns'] = () => ({ body: { status: 'ok' } })
    const app = renderApp('/dns/new', routes)
    await fillDomain('acme.com')
    expect(screen.queryByText(/^Template/)).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Add DNS domain' }))
    await waitFor(() => expect(app.location()).toBe('/dns'))
    expect(app.calls.find((c) => c.key === 'POST /dns')?.body).not.toHaveProperty('template')
  })

  it('hides the DNSSEC switch without supportsDnssec and sends dnssec false', async () => {
    const routes: Record<string, Route> = { ...dnsRoutes(), 'GET /dns/options': optionsWith({ supportsDnssec: false }) }
    routes['POST /dns'] = () => ({ body: { status: 'ok' } })
    const app = renderApp('/dns/new', routes)
    await fillDomain('acme.com')
    fireEvent.click(screen.getByRole('button', { name: 'Advanced options' }))
    await screen.findByLabelText('TTL')
    expect(screen.queryByText('Enable DNSSEC')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Add DNS domain' }))
    await waitFor(() => expect(app.location()).toBe('/dns'))
    expect(app.calls.find((c) => c.key === 'POST /dns')?.body).toMatchObject({ dnssec: false })
  })
})

describe('edit DNS zone', () => {
  const editRoutes = (over: Record<string, Route> = {}) => ({
    ...dnsRoutes(),
    'GET /dns/example.com': () => ({ body: zone('example.com', { SOA: 'ns9.example.net', TTL: '300' }) }),
    'PUT /dns/example.com': () => ({ body: { status: 'ok' } }),
    ...over,
  })

  it('prefills from the zone and sends only the edit fields', async () => {
    const app = renderApp('/dns/example.com/edit', editRoutes())
    expect(((await screen.findByLabelText('SOA')) as HTMLInputElement).value).toBe('ns9.example.net')
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(app.location()).toBe('/dns'))
    expect(app.calls.find((c) => c.key === 'PUT /dns/example.com')?.body).toEqual({
      ip: '203.0.113.24',
      template: 'default',
      soa: 'ns9.example.net',
      exp: '2027-01-01',
      ttl: '300',
      dnssec: false,
    })
    expect(await screen.findByText('Changes have been saved.')).toBeTruthy()
  })

  it('omits dnssec from the body when the switch is hidden', async () => {
    const app = renderApp('/dns/example.com/edit', {
      ...editRoutes(),
      'GET /dns/options': optionsWith({ supportsDnssec: false }),
    })
    await screen.findByLabelText('SOA')
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(app.location()).toBe('/dns'))
    expect(app.calls.find((c) => c.key === 'PUT /dns/example.com')?.body).not.toHaveProperty('dnssec')
  })

  it('shows the blank-field error when the server rejects a cleared IP', async () => {
    const app = renderApp(
      '/dns/example.com/edit',
      editRoutes({
        'PUT /dns/example.com': () => ({
          status: 400,
          body: { error: { code: 'invalid', message: 'Field "%s" can not be blank.', args: ['IP Address'] } },
        }),
      }),
    )
    await screen.findByLabelText('SOA')
    fireEvent.change(screen.getByRole('combobox', { name: /IP address/ }), { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText('Field "IP Address" can not be blank.')).toBeTruthy()
    expect(app.calls.find((c) => c.key === 'PUT /dns/example.com')?.body).toMatchObject({ ip: '' })
    expect(app.location()).toBe('/dns/example.com/edit')
  })

  it('marks SOA and TTL required, so a cleared one sends no PUT', async () => {
    const app = renderApp('/dns/example.com/edit', editRoutes())
    const soa = (await screen.findByLabelText('SOA')) as HTMLInputElement
    expect(soa.required).toBe(true)
    expect((screen.getByLabelText('TTL') as HTMLInputElement).required).toBe(true)
    fireEvent.change(soa, { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await new Promise((r) => setTimeout(r, 50))
    expect(app.calls.some((c) => c.key === 'PUT /dns/example.com')).toBe(false)
  })

  it('is read-only for a read-only user', async () => {
    renderApp('/dns/example.com/edit', {
      ...editRoutes(),
      'GET /bootstrap': () => ({ body: dnsRoutes({ readOnly: true })['GET /bootstrap']({}).body }),
    })
    await screen.findByLabelText('SOA')
    expect((screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(true)
  })
})
