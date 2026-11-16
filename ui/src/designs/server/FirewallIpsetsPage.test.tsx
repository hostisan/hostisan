import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderApp, type Route } from '../../test/renderApp'
import { JOB_ID, jobFailed, serverRoutes } from '../../test/serverFixtures'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

type App = { calls: { key: string; body: unknown }[] }
const find = (app: App, key: string) => app.calls.find((c) => c.key === key)
const ok: Route = () => ({ body: { status: 'ok' } })
const job: Route = () => ({ status: 202, body: { jobId: JOB_ID } })
async function pick(label: RegExp | string, option: string) {
  fireEvent.click(await screen.findByRole('combobox', { name: label }))
  const opt = await screen.findByRole('option', { name: option })
  fireEvent.pointerDown(opt)
  fireEvent.mouseDown(opt)
  fireEvent.pointerUp(opt)
  fireEvent.mouseUp(opt)
  fireEvent.click(opt)
  await waitFor(() => expect(screen.queryByRole('option')).toBeNull())
}

describe('IP lists', () => {
  it('lists the IP lists with their update and version', async () => {
    renderApp('/server/firewall/ipsets', serverRoutes())
    const tr = (await screen.findByText('blacklist')).closest('tr') as HTMLElement
    expect(within(tr).getByText('Enabled')).toBeTruthy()
    expect(within(tr).getByText('IPv4')).toBeTruthy()
    expect(document.body.textContent).not.toMatch(/Hestia/)
  })

  it('shows an empty state while the firewall is disabled', async () => {
    renderApp(
      '/server/firewall/ipsets',
      serverRoutes({ 'GET /server/firewall/ipsets': () => ({ body: { enabled: false, rows: [] } }) }),
    )
    expect(await screen.findByText('The firewall is disabled')).toBeTruthy()
    expect(screen.queryByRole('link', { name: 'Add IP list' })).toBeNull()
  })

  it('deletes one list after a confirmation', async () => {
    const app = renderApp('/server/firewall/ipsets', serverRoutes({ 'DELETE /server/firewall/ipsets/blacklist': ok }))
    await screen.findByText('blacklist')
    fireEvent.click(screen.getByRole('button', { name: 'Actions for blacklist' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(find(app, 'DELETE /server/firewall/ipsets/blacklist')).toBeTruthy())
  })

  it('deletes the selected lists in bulk', async () => {
    const app = renderApp(
      '/server/firewall/ipsets',
      serverRoutes({ 'POST /server/firewall/ipsets/bulk': () => ({ body: { status: 'ok', failed: [] } }) }),
    )
    fireEvent.click(within((await screen.findByText('blacklist')).closest('tr') as HTMLElement).getByRole('checkbox'))
    fireEvent.click(await screen.findByRole('button', { name: 'Delete' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(find(app, 'POST /server/firewall/ipsets/bulk')).toBeTruthy())
    expect(find(app, 'POST /server/firewall/ipsets/bulk')?.body).toEqual({ action: 'delete', names: ['blacklist'] })
  })

  it('shows the refusal when a list is still used by a rule', async () => {
    renderApp(
      '/server/firewall/ipsets',
      serverRoutes({
        'DELETE /server/firewall/ipsets/blacklist': () => ({
          status: 400,
          body: {
            error: { code: 'command_failed', message: 'Error: ipset blacklist still used by iptables. Cannot remove' },
          },
        }),
      }),
    )
    await screen.findByText('blacklist')
    fireEvent.click(screen.getByRole('button', { name: 'Actions for blacklist' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Delete' }))
    expect((await screen.findByRole('alert')).textContent).toContain('still used by iptables')
  })
})

describe('add IP list', () => {
  it('offers the shipped sources and adds the list as a job', async () => {
    const app = renderApp('/server/firewall/ipsets/new', serverRoutes({ 'POST /server/firewall/ipsets': job }))
    fireEvent.change(await screen.findByLabelText(/^IP list name/), { target: { value: 'country-cn' } })
    fireEvent.click(await screen.findByRole('combobox', { name: 'Choose a preset list' }))
    const names = (await screen.findAllByRole('option')).map((o) => o.textContent)
    expect(names).toContain('[IPv4] Block Malicious IPs')
    expect(names).toContain('[IPv4] Country - China')
    expect(names.filter((n) => n?.startsWith('[IPv4] Country - '))).toHaveLength(16)
    fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('option')).toBeNull())
    await pick('Choose a preset list', '[IPv4] Country - China')
    expect((screen.getByLabelText(/^Data source/) as HTMLInputElement).value).toBe(
      'https://raw.githubusercontent.com/ipverse/rir-ip/master/country/cn/ipv4-aggregated.txt',
    )
    fireEvent.click(screen.getByRole('button', { name: 'Add IP list' }))
    await waitFor(() => expect(find(app, 'POST /server/firewall/ipsets')).toBeTruthy())
    expect(find(app, 'POST /server/firewall/ipsets')?.body).toEqual({
      name: 'country-cn',
      source: 'https://raw.githubusercontent.com/ipverse/rir-ip/master/country/cn/ipv4-aggregated.txt',
      ipVersion: 'v4',
      autoupdate: true,
    })
    await waitFor(() => expect(app.location()).toBe('/server/firewall/ipsets'))
    expect(await screen.findByText('IP list has been created successfully.')).toBeTruthy()
  })

  it('shows the failure of the job and stays', async () => {
    const app = renderApp(
      '/server/firewall/ipsets/new',
      serverRoutes({
        'POST /server/firewall/ipsets': job,
        [`GET /server/jobs/${JOB_ID}`]: () => ({ body: jobFailed('Error: IP list x.v4 is too small (<5), ignoring') }),
      }),
    )
    fireEvent.change(await screen.findByLabelText(/^IP list name/), { target: { value: 'x' } })
    fireEvent.change(screen.getByLabelText(/^Data source/), { target: { value: 'https://example.net/list.txt' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add IP list' }))
    expect(await screen.findByText('Error: IP list x.v4 is too small (<5), ignoring')).toBeTruthy()
    expect(app.location()).toBe('/server/firewall/ipsets/new')
  })

  it('shows the validation refusal before any job starts', async () => {
    renderApp(
      '/server/firewall/ipsets/new',
      serverRoutes({
        'POST /server/firewall/ipsets': () => ({
          status: 400,
          body: { error: { code: 'invalid', message: 'Invalid data source: %s', args: ['file:/etc/passwd'] } },
        }),
      }),
    )
    fireEvent.change(await screen.findByLabelText(/^IP list name/), { target: { value: 'x' } })
    fireEvent.change(screen.getByLabelText(/^Data source/), { target: { value: 'file:/etc/passwd' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add IP list' }))
    expect(await screen.findByText('Invalid data source: file:/etc/passwd')).toBeTruthy()
  })
})
