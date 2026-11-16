import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { dnsRoutes, renderApp } from '../../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('DNS zones page', () => {
  it('lists the zones from the API', async () => {
    renderApp('/dns', dnsRoutes())
    expect(await screen.findByRole('link', { name: 'example.com' })).toBeTruthy()
    expect(screen.getByRole('link', { name: 'example.org' })).toBeTruthy()
    expect(screen.queryByText('acme-bakery.com')).toBeNull()
  })

  it('offers Unsuspend on the suspended zone', async () => {
    renderApp('/dns', dnsRoutes())
    await screen.findByRole('link', { name: 'example.org' })
    fireEvent.click(screen.getByRole('button', { name: 'Actions for example.org' }))
    expect(await screen.findByRole('menuitem', { name: 'Unsuspend' })).toBeTruthy()
  })

  it('hides add, bulk and row actions when read-only', async () => {
    renderApp('/dns', dnsRoutes({ readOnly: true }))
    await screen.findByRole('link', { name: 'example.com' })
    expect(screen.queryByRole('link', { name: 'Add DNS domain' })).toBeNull()
    expect(screen.queryByRole('checkbox')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Actions for example.com' }))
    expect(await screen.findByRole('menuitem', { name: 'DNS records' })).toBeTruthy()
    for (const name of ['Suspend', 'Delete', 'Edit DNS domain', 'Add DNS record'])
      expect(screen.queryByRole('menuitem', { name })).toBeNull()
  })

  it('shows only Delete in bulk for a non-admin', async () => {
    renderApp('/dns', dnsRoutes({ role: 'user', name: 'bob' }))
    await screen.findByRole('link', { name: 'example.com' })
    fireEvent.click(screen.getAllByRole('checkbox')[1])
    expect(await screen.findByRole('button', { name: 'Delete' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Rebuild' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Suspend' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Unsuspend' })).toBeNull()
  })

  it('offers no row Suspend or Unsuspend to a non-admin', async () => {
    renderApp('/dns', dnsRoutes({ role: 'user', name: 'bob' }))
    await screen.findByRole('link', { name: 'example.com' })
    for (const zone of ['example.com', 'example.org']) {
      fireEvent.click(screen.getByRole('button', { name: `Actions for ${zone}` }))
      expect(await screen.findByRole('menuitem', { name: 'Delete' })).toBeTruthy()
      expect(screen.queryByRole('menuitem', { name: 'Suspend' })).toBeNull()
      expect(screen.queryByRole('menuitem', { name: 'Unsuspend' })).toBeNull()
      fireEvent.keyDown(screen.getByRole('menu'), { key: 'Escape' })
      await waitFor(() => expect(screen.queryByRole('menu')).toBeNull())
    }
  })

  it('names each selection checkbox after its row', async () => {
    renderApp('/dns', dnsRoutes())
    await screen.findByRole('link', { name: 'example.com' })
    expect(screen.getByRole('checkbox', { name: 'Select example.com' })).toBeTruthy()
    expect(screen.getByRole('checkbox', { name: 'Select all DNS zones' })).toBeTruthy()
    expect(screen.queryByRole('checkbox', { name: 'Select row' })).toBeNull()
  })

  it("names the failed zone and Hestia's message after a bulk delete", async () => {
    const routes = dnsRoutes()
    routes['POST /dns/bulk'] = () => ({
      body: { status: 'ok', failed: [{ name: 'example.org', message: "Error: dns domain example.org doesn't exist" }] },
    })
    const app = renderApp('/dns', routes)
    await screen.findByRole('link', { name: 'example.com' })
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select all DNS zones' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Delete' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete' }))
    expect(await screen.findByText("Error: dns domain example.org doesn't exist")).toBeTruthy()
    expect(screen.getAllByText('example.org').length).toBeGreaterThan(0)
    const call = app.calls.find((c) => c.key === 'POST /dns/bulk')
    expect(call?.body).toEqual({ action: 'delete', names: expect.arrayContaining(['example.com', 'example.org']) })
    await waitFor(() => expect(app.calls.filter((c) => c.key === 'GET /dns').length).toBeGreaterThan(1))
  })

  it('opens newest first for sortOrder date, by name otherwise', async () => {
    renderApp('/dns', dnsRoutes({ sortOrder: 'date' }))
    await screen.findByRole('link', { name: 'example.com' })
    const names = screen.getAllByRole('link', { name: /^example\./ }).map((a) => a.textContent)
    expect(names.slice(0, 2)).toEqual(['example.org', 'example.com'])
  })
})

describe('sidebar', () => {
  it('hides DNS when the package has no DNS feature', async () => {
    renderApp('/web', dnsRoutes({ features: { dns: false } }))
    await screen.findByText('Hosting')
    expect(screen.queryByRole('link', { name: 'DNS' })).toBeNull()
  })
  it('shows DNS otherwise', async () => {
    renderApp('/web', dnsRoutes())
    expect(await screen.findByRole('link', { name: 'DNS' })).toBeTruthy()
  })
})

describe('command palette', () => {
  it('fetches zones only once opened', async () => {
    const app = renderApp('/web', dnsRoutes())
    await screen.findByText('Hosting')
    expect(app.calls.some((c) => c.key === 'GET /dns')).toBe(false)
    fireEvent.keyDown(window, { key: 'k', ctrlKey: true })
    await waitFor(() => expect(app.calls.some((c) => c.key === 'GET /dns')).toBe(true))
  })
})

describe('row actions', () => {
  it('unsuspends a zone through the API and refetches', async () => {
    const routes = dnsRoutes()
    routes['POST /dns/example.org/unsuspend'] = () => ({ body: { status: 'ok' } })
    const app = renderApp('/dns', routes)
    await screen.findByRole('link', { name: 'example.org' })
    fireEvent.click(screen.getByRole('button', { name: 'Actions for example.org' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Unsuspend' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Unsuspend' }))
    await waitFor(() => expect(app.calls.some((c) => c.key === 'POST /dns/example.org/unsuspend')).toBe(true))
    await waitFor(() => expect(app.calls.filter((c) => c.key === 'GET /dns').length).toBeGreaterThan(1))
  })
})

describe('aside usage', () => {
  it('shows used and limit from the API', async () => {
    renderApp('/dns', dnsRoutes())
    expect(await screen.findByText('2 / 5')).toBeTruthy()
    expect(screen.getByText('13 / 250')).toBeTruthy()
    expect(screen.getByText('DNS zones (1 suspended)')).toBeTruthy()
  })
})

describe('delete dialog errors', () => {
  it("does not carry one zone's error to the next zone", async () => {
    const routes = dnsRoutes()
    routes['DELETE /dns/example.com'] = () => ({
      status: 400,
      body: { error: { code: 'invalid', message: 'Error: example.com is busy' } },
    })
    renderApp('/dns', routes)
    await screen.findByRole('link', { name: 'example.com' })
    fireEvent.click(screen.getByRole('button', { name: 'Actions for example.com' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete' }))
    let dialog = await screen.findByRole('dialog')
    fireEvent.change(within(dialog).getByRole('textbox'), { target: { value: 'example.com' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete DNS domain' }))
    expect(await screen.findByText('Error: example.com is busy')).toBeTruthy()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())

    fireEvent.click(screen.getByRole('button', { name: 'Actions for example.org' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete' }))
    dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('example.org', { selector: 'span.font-medium' })).toBeTruthy()
    expect(screen.queryByText('Error: example.com is busy')).toBeNull()
  })

  it('has a sortable Created column, as Hestia has a Date sort', async () => {
    renderApp('/dns', dnsRoutes())
    await screen.findAllByRole('link')
    expect(await screen.findByRole('button', { name: /Created/ })).toBeTruthy()
  })
})
