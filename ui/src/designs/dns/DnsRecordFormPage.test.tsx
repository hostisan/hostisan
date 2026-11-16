import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { dnsRoutes, record, renderApp } from '../../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('add DNS record', () => {
  it('defaults the record to @ and shows the resolved name for a host', async () => {
    renderApp('/dns/example.com/records/new', dnsRoutes())
    const input = (await screen.findByRole('textbox', { name: 'Record' })) as HTMLInputElement
    expect(input.value).toBe('@')
    expect(screen.queryByText('www.example.com')).toBeNull()
    fireEvent.change(input, { target: { value: 'www' } })
    expect(await screen.findByText('www.example.com')).toBeTruthy()
  })

  it('posts the record and returns to the records page', async () => {
    const routes = dnsRoutes()
    routes['POST /dns/example.com/records'] = () => ({ body: { status: 'ok' } })
    const app = renderApp('/dns/example.com/records/new', routes)
    const input = await screen.findByRole('textbox', { name: 'Record' })
    fireEvent.change(input, { target: { value: 'www' } })
    fireEvent.change(screen.getByRole('combobox', { name: /IP or value/ }), { target: { value: '203.0.113.9' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add record' }))
    await waitFor(() => expect(app.location()).toBe('/dns/example.com'))
    expect(app.calls.find((c) => c.key === 'POST /dns/example.com/records')?.body).toEqual({
      record: 'www',
      type: 'A',
      value: '203.0.113.9',
      priority: '',
      ttl: '',
    })
    expect(await screen.findByText('Record www.example.com has been created successfully.')).toBeTruthy()
  })
})

describe('edit DNS record', () => {
  it('prefills from the record', async () => {
    renderApp('/dns/example.com/records/2', dnsRoutes())
    expect(((await screen.findByRole('textbox', { name: 'Record' })) as HTMLInputElement).value).toBe('www')
  })

  it('shows an unknown record id as an error', async () => {
    renderApp('/dns/example.com/records/99', dnsRoutes())
    expect(await screen.findByText('Error: unknown record ID.')).toBeTruthy()
  })

  it('shows No pending changes as an error and keeps the page', async () => {
    const routes = dnsRoutes()
    routes['PUT /dns/example.com/records/1'] = () => ({
      status: 422,
      body: { error: { code: 'command_failed', message: 'Error: No pending changes in DNS entry.' } },
    })
    const base = dnsRoutes()['GET /dns/example.com/records']({}).body as { zone: unknown }
    routes['GET /dns/example.com/records'] = () => ({ body: { zone: base.zone, records: [record('1')] } })
    const app = renderApp('/dns/example.com/records/1', routes)
    await screen.findByRole('textbox', { name: 'Record' })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText('Error: No pending changes in DNS entry.')).toBeTruthy()
    expect(app.location()).toBe('/dns/example.com/records/1')
  })
})
