import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderApp, type Route } from '../../test/renderApp'
import { JOB_ID, serverRoutes } from '../../test/serverFixtures'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const job: Route = () => ({ status: 202, body: { jobId: JOB_ID } })
const ok: Route = () => ({ body: { status: 'ok' } })
const field = (label: RegExp | string) => screen.findByLabelText(label) as Promise<HTMLInputElement>
const body = (app: { calls: { key: string; body: unknown }[] }, key: string) =>
  app.calls.find((c) => c.key === key)?.body

describe('add IP address', () => {
  it('adds a shared address as a job, then toasts a link to the edit page', async () => {
    const app = renderApp('/server/ip/new', serverRoutes({ 'POST /server/ips': job }))
    fireEvent.change(await field(/^IP address/), { target: { value: '203.0.113.20' } })
    fireEvent.change(await field(/^Netmask/), { target: { value: '255.255.255.0' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add IP address' }))
    await waitFor(() => expect(body(app, 'POST /server/ips')).toBeTruthy())
    // Shared sends the root user as owner (decision 45)
    expect(body(app, 'POST /server/ips')).toEqual({
      ip: '203.0.113.20',
      netmask: '255.255.255.0',
      interface: 'eth0',
      shared: true,
      owner: 'admin',
      name: '',
      nat: '',
    })
    await waitFor(() => expect(app.location()).toBe('/server/ip'))
    const link = await screen.findByRole('link', { name: '203.0.113.20' })
    expect(link.getAttribute('href')).toBe('/server/ip/203.0.113.20')
    expect(link.parentElement?.textContent).toContain('IP address 203.0.113.20 has been created successfully.')
  })

  it('stays on the form with the failure when the job fails', async () => {
    const app = renderApp(
      '/server/ip/new',
      serverRoutes({
        'POST /server/ips': job,
        [`GET /server/jobs/${JOB_ID}`]: () => ({
          body: {
            id: JOB_ID,
            title: 'Add IP address',
            state: 'failed',
            step: 'Add IP',
            steps: ['Add IP'],
            error: { code: 'command_failed', message: 'Error: 203.0.113.20 already exists' },
            result: null,
            started: '',
            finished: '',
          },
        }),
      }),
    )
    fireEvent.change(await field(/^IP address/), { target: { value: '203.0.113.20' } })
    fireEvent.change(await field(/^Netmask/), { target: { value: '255.255.255.0' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add IP address' }))
    expect(await screen.findByText('Error: 203.0.113.20 already exists')).toBeTruthy()
    expect(app.location()).toBe('/server/ip/new')
  })

  it('asks for an assigned user only when the address is not shared', async () => {
    const app = renderApp('/server/ip/new', serverRoutes({ 'POST /server/ips': job }))
    await field(/^IP address/)
    expect(screen.queryByRole('combobox', { name: /Assigned user/ })).toBeNull()
    fireEvent.click(screen.getByRole('checkbox', { name: 'Shared' }))
    expect(await screen.findByRole('combobox', { name: /Assigned user/ })).toBeTruthy()
    fireEvent.change(await field(/^IP address/), { target: { value: '203.0.113.21' } })
    fireEvent.change(await field(/^Netmask/), { target: { value: '255.255.255.0' } })
    fireEvent.change(await field(/NAT IP/), { target: { value: '10.1.1.1' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add IP address' }))
    await waitFor(() => expect(body(app, 'POST /server/ips')).toBeTruthy())
    expect(body(app, 'POST /server/ips')).toMatchObject({ shared: false, owner: 'admin', nat: '10.1.1.1' })
  })
})

describe('edit IP address', () => {
  it('locks the address, netmask and interface and saves the rest', async () => {
    const app = renderApp('/server/ip/10.0.0.5', serverRoutes({ 'PUT /server/ips/10.0.0.5': ok }))
    expect((await field(/^IP address/)).disabled).toBe(true)
    expect((await field(/^Netmask/)).disabled).toBe(true)
    expect(((await field(/^Interface/)) as HTMLInputElement).disabled).toBe(true)
    expect((await field(/NAT IP/)).value).toBe('198.51.100.7')
    fireEvent.change(await field(/Assigned domain/), { target: { value: 'shop.example.com' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(body(app, 'PUT /server/ips/10.0.0.5')).toBeTruthy())
    expect(body(app, 'PUT /server/ips/10.0.0.5')).toMatchObject({
      shared: false,
      owner: 'bob',
      name: 'shop.example.com',
      nat: '198.51.100.7',
    })
    await waitFor(() => expect(app.location()).toBe('/server/ip'))
    expect(await screen.findByText('Changes have been saved.')).toBeTruthy()
  })

  it('deletes the address from the form', async () => {
    const app = renderApp('/server/ip/203.0.113.10', serverRoutes({ 'DELETE /server/ips/203.0.113.10': job }))
    fireEvent.click(await screen.findByRole('button', { name: 'Delete' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(app.calls.some((c) => c.key === 'DELETE /server/ips/203.0.113.10')).toBe(true))
    await waitFor(() => expect(app.location()).toBe('/server/ip'))
  })

  it('shows the API error for an unknown address', async () => {
    renderApp(
      '/server/ip/9.9.9.9',
      serverRoutes({
        'GET /server/ips/9.9.9.9': () => ({
          status: 404,
          body: { error: { code: 'not_found', message: "Error: IP 9.9.9.9 doesn't exist" } },
        }),
      }),
    )
    expect(await screen.findByText("Error: IP 9.9.9.9 doesn't exist")).toBeTruthy()
    expect(document.body.textContent).not.toMatch(/Hestia/)
  })
})
