// A save that does not happen leaves the form guarded: a client-side check that stops
// it, a raw request that fails, and a job that starts but fails.
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { keysRoutes, profileRoutes, usersRoutes } from '../test/accountFixtures'
import { renderApp, type Route } from '../test/renderApp'
import { JOB_ID, jobFailed, serverRoutes } from '../test/serverFixtures'
import { webRoutes } from '../test/webFixtures'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const job: Route = () => ({ status: 202, body: { jobId: JOB_ID } })
const failedJob: Route = () => ({ body: jobFailed('Error: the job failed') })
const unsaved = () => screen.queryByRole('dialog', { name: 'Unsaved changes' })

/** Clicks the sidebar's Web link and reports whether the page asked. */
async function leaveAsks(app: ReturnType<typeof renderApp>, from: string): Promise<boolean> {
  const sidebar = document.querySelector('[data-sidebar="sidebar"]') as HTMLElement
  fireEvent.click(within(sidebar).getByRole('link', { name: 'Web' }))
  await waitFor(() => expect(unsaved() !== null || app.location() !== from).toBe(true))
  return unsaved() !== null
}

describe('a failed save keeps asking before leaving', () => {
  it('UserForm: a blank name server list', async () => {
    const app = renderApp('/users/bob', { ...usersRoutes(), ...profileRoutes(), 'PUT /users/bob': () => ({ body: {} }) })
    await screen.findByRole('textbox', { name: 'Contact name' })
    fireEvent.click(screen.getByRole('button', { name: /advanced options/i }))
    for (const ns of ['ns1.acme-hosting.net', 'ns2.acme-hosting.net']) {
      fireEvent.click(await screen.findByRole('button', { name: `Remove ${ns}` }))
    }
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText('Field "Nameserver 1" can not be blank.')).toBeTruthy()
    expect(await leaveAsks(app, '/users/bob')).toBe(true)
  }, 20000)

  it('GenerateSslPage: a country that is not two letters', async () => {
    const app = renderApp('/web/example.com/ssl/generate', webRoutes())
    fireEvent.change(await screen.findByLabelText(/^Country/), { target: { value: 'USA' } })
    expect(await screen.findByText('Use a two-letter code, like US or DE.')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Generate' }))
    expect(await leaveAsks(app, '/web/example.com/ssl/generate')).toBe(true)
    expect(app.calls.some((c) => c.key === 'POST /web/generate-ssl')).toBe(false)
  }, 20000)

  it('AccessKeysPage: the raw request fails', async () => {
    const app = renderApp(
      '/account/access-keys/new',
      keysRoutes(
        {},
        {
          'POST /account/access-keys': () => ({
            status: 400,
            body: { error: { code: 'invalid', message: 'API %s not allowed', args: ['billing'] } },
          }),
        },
      ),
    )
    fireEvent.click(await screen.findByRole('checkbox', { name: /billing/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Add access key' }))
    expect(await screen.findByText('API billing not allowed')).toBeTruthy()
    expect(await leaveAsks(app, '/account/access-keys/new')).toBe(true)
  }, 20000)

  it('ServerConfigPage: the save job fails', async () => {
    const app = renderApp(
      '/server/config',
      serverRoutes({ 'PUT /server/settings': job, [`GET /server/jobs/${JOB_ID}`]: failedJob }),
    )
    await screen.findByRole('tab', { name: 'General' })
    const form = document.querySelector('[data-shortcut="save"]')!.closest('form') as HTMLElement
    const input = [...form.querySelectorAll<HTMLInputElement>('input:not([type]), input[type=text]')].find(
      (el) => !el.disabled && !el.readOnly && el.getAttribute('role') !== 'combobox',
    )!
    fireEvent.change(input, { target: { value: `${input.value}x` } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText('Error: the job failed')).toBeTruthy()
    expect(await leaveAsks(app, '/server/config')).toBe(true)
  }, 20000)

  it('FirewallIpsetFormPage: the add job fails', async () => {
    const app = renderApp(
      '/server/firewall/ipsets/new',
      serverRoutes({ 'POST /server/firewall/ipsets': job, [`GET /server/jobs/${JOB_ID}`]: failedJob }),
    )
    fireEvent.change(await screen.findByLabelText(/^IP list name/), { target: { value: 'x' } })
    fireEvent.change(screen.getByLabelText(/^Data source/), { target: { value: 'https://example.net/list.txt' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add IP list' }))
    expect(await screen.findByText('Error: the job failed')).toBeTruthy()
    expect(await leaveAsks(app, '/server/firewall/ipsets/new')).toBe(true)
  }, 20000)

  it('IpFormPage: the add job fails', async () => {
    const app = renderApp(
      '/server/ip/new',
      serverRoutes({ 'POST /server/ips': job, [`GET /server/jobs/${JOB_ID}`]: failedJob }),
    )
    fireEvent.change(await screen.findByLabelText(/^IP address/), { target: { value: '203.0.113.20' } })
    fireEvent.change(await screen.findByLabelText(/^Netmask/), { target: { value: '255.255.255.0' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add IP address' }))
    expect(await screen.findByText('Error: the job failed')).toBeTruthy()
    expect(await leaveAsks(app, '/server/ip/new')).toBe(true)
  }, 20000)
})
