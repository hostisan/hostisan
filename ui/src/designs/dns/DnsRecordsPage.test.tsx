import { cleanup, fireEvent, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { dnsRoutes, record, renderApp } from '../../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('DNS records page', () => {
  it('offers row Suspend to an admin', async () => {
    renderApp('/dns/example.com', dnsRoutes())
    fireEvent.click(await screen.findByRole('button', { name: 'Actions for record www CNAME' }))
    expect(await screen.findByRole('menuitem', { name: 'Suspend' })).toBeTruthy()
  })

  it('offers no row Suspend or Unsuspend to a non-admin', async () => {
    renderApp('/dns/example.com', dnsRoutes({ role: 'user', name: 'bob' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Actions for record www CNAME' }))
    expect(await screen.findByRole('menuitem', { name: 'Delete' })).toBeTruthy()
    expect(screen.queryByRole('menuitem', { name: 'Suspend' })).toBeNull()
    expect(screen.queryByRole('menuitem', { name: 'Unsuspend' })).toBeNull()
  })

  it('names a failed record by name and type in the bulk toast, not by id', async () => {
    const routes = dnsRoutes()
    routes['POST /dns/example.com/records/bulk'] = () => ({
      body: { status: 'ok', failed: [{ name: '2', message: 'Error: record is busy' }] },
    })
    const app = renderApp('/dns/example.com', routes)
    const row = (await screen.findByRole('link', { name: 'www' })).closest('tr') as HTMLElement
    fireEvent.click(within(row).getByRole('checkbox'))
    fireEvent.click(await screen.findByRole('button', { name: 'Delete' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete' }))
    expect(await screen.findByText('Error: record is busy')).toBeTruthy()
    expect(screen.getByText('www CNAME')).toBeTruthy()
    expect(app.calls.find((c) => c.key === 'POST /dns/example.com/records/bulk')?.body).toEqual({
      action: 'delete',
      names: ['2'],
    })
  })

  it('selects the rows between two clicks on shift-click', async () => {
    const routes = dnsRoutes()
    const base = routes['GET /dns/example.com/records']({}) .body as { zone: unknown }
    const names = ['@', 'www', 'mail', 'ftp']
    routes['GET /dns/example.com/records'] = () => ({
      body: { zone: base.zone, records: names.map((n, k) => record(String(k + 1), { RECORD: n })) },
    })
    renderApp('/dns/example.com', routes)
    await screen.findByRole('link', { name: 'ftp' })
    const boxes = () => screen.getAllByRole('checkbox').slice(1)
    fireEvent.click(boxes()[0])
    fireEvent.click(boxes()[3], { shiftKey: true })
    expect(await screen.findByText('4 selected')).toBeTruthy()
  })
})
