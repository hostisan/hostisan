import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderApp, type Route } from '../../test/renderApp'
import { JOB_ID, jobDone, serverRoutes } from '../../test/serverFixtures'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const job: Route = () => ({ status: 202, body: { jobId: JOB_ID } })
const row = async (ip: string) => (await screen.findByRole('link', { name: ip })).closest('tr') as HTMLElement
const find = (app: { calls: { key: string; body: unknown }[] }, key: string) => app.calls.find((c) => c.key === key)

describe('IP list', () => {
  it('shows the columns, the NAT arrow and hides the owner of shared addresses', async () => {
    renderApp('/server/ip', serverRoutes())
    const dedicated = await row('10.0.0.5')
    for (const h of ['IP address', 'Netmask', 'Interface', 'Status', 'Domains', 'Owner']) {
      expect(screen.getByRole('columnheader', { name: h })).toBeTruthy()
    }
    // NAT: IP, arrow, then the public address
    expect(within(dedicated).getByLabelText('NAT to')).toBeTruthy()
    expect(within(dedicated).getByText('198.51.100.7')).toBeTruthy()
    expect(within(dedicated).getByText('bob')).toBeTruthy()
    expect(within(dedicated).getByText('Dedicated')).toBeTruthy()
    const shared = await row('203.0.113.10')
    expect(within(shared).getByText('Shared')).toBeTruthy()
    expect(within(shared).queryByText('admin')).toBeNull()
    expect(within(shared).getByText('main')).toBeTruthy()
    // a dedicated address shows its owner
    expect(within(await row('203.0.113.11')).getByText('admin')).toBeTruthy()
    expect(document.body.textContent).not.toMatch(/Hestia/)
  })

  it('refreshes the addresses without a selection and follows the job', async () => {
    const app = renderApp('/server/ip', serverRoutes({ 'POST /server/ips/refresh': job }))
    fireEvent.click(await screen.findByRole('button', { name: 'Refresh IP addresses' }))
    await waitFor(() => expect(find(app, 'POST /server/ips/refresh')).toBeTruthy())
    expect(find(app, 'POST /server/ips/refresh')?.body).toBeUndefined()
    expect(await screen.findByText('Server task')).toBeTruthy()
  })

  it('deletes one address after a confirmation, sending no body', async () => {
    const app = renderApp('/server/ip', serverRoutes({ 'DELETE /server/ips/203.0.113.10': job }))
    await row('203.0.113.10')
    fireEvent.click(await screen.findByRole('button', { name: 'Actions for 203.0.113.10' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText(/delete IP address 203.0.113.10/)).toBeTruthy()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(find(app, 'DELETE /server/ips/203.0.113.10')).toBeTruthy())
    expect(find(app, 'DELETE /server/ips/203.0.113.10')?.body).toBeUndefined()
    expect(await screen.findByText('Server task')).toBeTruthy()
  })

  it('deletes the selected addresses in bulk', async () => {
    const app = renderApp('/server/ip', serverRoutes({ 'POST /server/ips/bulk': job }))
    fireEvent.click(within(await row('10.0.0.5')).getByRole('checkbox'))
    fireEvent.click(await screen.findByRole('button', { name: 'Delete' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(find(app, 'POST /server/ips/bulk')).toBeTruthy())
    expect(find(app, 'POST /server/ips/bulk')?.body).toEqual({ action: 'delete', names: ['10.0.0.5'] })
  })

  it('names the addresses a finished bulk delete could not remove', async () => {
    renderApp(
      '/server/ip',
      serverRoutes({
        'POST /server/ips/bulk': job,
        [`GET /server/jobs/${JOB_ID}`]: () => ({
          body: jobDone({ status: 'ok', failed: [{ name: '10.0.0.5', message: 'Error: IP is in use' }] }),
        }),
      }),
    )
    fireEvent.click(within(await row('10.0.0.5')).getByRole('checkbox'))
    fireEvent.click(await screen.findByRole('button', { name: 'Delete' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Delete' }))
    expect((await screen.findByRole('alert')).textContent).toContain('10.0.0.5: Error: IP is in use')
  })

  it('shows the API refusal and keeps the list', async () => {
    renderApp(
      '/server/ip',
      serverRoutes({
        'DELETE /server/ips/203.0.113.10': () => ({
          status: 400,
          body: { error: { code: 'command_failed', message: 'Error: IP 203.0.113.10 is in use' } },
        }),
      }),
    )
    await row('203.0.113.10')
    fireEvent.click(await screen.findByRole('button', { name: 'Actions for 203.0.113.10' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Delete' }))
    expect(await screen.findByText('Error: IP 203.0.113.10 is in use')).toBeTruthy()
    expect(screen.getByRole('link', { name: '203.0.113.10' })).toBeTruthy()
  })
})
