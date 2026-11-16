import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderApp, type Route } from '../../test/renderApp'
import { firewallRule, serverRoutes } from '../../test/serverFixtures'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const ok: Route = () => ({ body: { status: 'ok' } })
const field = (label: RegExp | string) => screen.findByLabelText(label) as Promise<HTMLInputElement>
const body = (app: { calls: { key: string; body: unknown }[] }, key: string) =>
  app.calls.find((c) => c.key === key)?.body

describe('add firewall rule', () => {
  it('adds a rule and goes back to the list', async () => {
    const app = renderApp('/server/firewall/new', serverRoutes({ 'POST /server/firewall/rules': ok }))
    fireEvent.change(await field(/^Port/), { target: { value: '8080' } })
    fireEvent.change(await field(/^IP address/), { target: { value: '203.0.113.0/24' } })
    fireEvent.change(await field(/^Comment/), { target: { value: 'Office' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add rule' }))
    await waitFor(() => expect(body(app, 'POST /server/firewall/rules')).toBeTruthy())
    expect(body(app, 'POST /server/firewall/rules')).toEqual({
      action: 'DROP',
      protocol: 'TCP',
      port: '8080',
      ip: '203.0.113.0/24',
      comment: 'Office',
    })
    await waitFor(() => expect(app.location()).toBe('/server/firewall'))
    expect(await screen.findByText('Rule has been created successfully.')).toBeTruthy()
  })

  it('offers the IPv4 IP sets that are not suspended', async () => {
    renderApp(
      '/server/firewall/new',
      serverRoutes({
        'GET /server/firewall/ipsets': () => ({
          body: {
            enabled: true,
            rows: [
              {
                LISTNAME: 'good',
                IP_VERSION: 'v4',
                AUTOUPDATE: 'yes',
                SUSPENDED: 'no',
                SOURCE: 'https://x/a',
                TIME: '',
                DATE: '',
              },
              {
                LISTNAME: 'six',
                IP_VERSION: 'v6',
                AUTOUPDATE: 'yes',
                SUSPENDED: 'no',
                SOURCE: 'https://x/b',
                TIME: '',
                DATE: '',
              },
              {
                LISTNAME: 'off',
                IP_VERSION: 'v4',
                AUTOUPDATE: 'yes',
                SUSPENDED: 'yes',
                SOURCE: 'https://x/c',
                TIME: '',
                DATE: '',
              },
            ],
          },
        }),
      }),
    )
    fireEvent.click(await screen.findByRole('combobox', { name: 'Use an IP list' }))
    const names = (await screen.findAllByRole('option')).map((o) => o.textContent)
    expect(names).toContain('good')
    expect(names).not.toContain('six')
    expect(names).not.toContain('off')
  })

  it('shows a lockout refusal and stays on the form', async () => {
    const app = renderApp(
      '/server/firewall/new',
      serverRoutes({
        'POST /server/firewall/rules': () => ({
          status: 409,
          body: { error: { code: 'lockout', message: 'This rule would block your own address.' } },
        }),
      }),
    )
    fireEvent.change(await field(/^Port/), { target: { value: '0' } })
    fireEvent.change(await field(/^IP address/), { target: { value: '0.0.0.0/0' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add rule' }))
    expect(await screen.findByText('This rule would block your own address.')).toBeTruthy()
    expect(app.location()).toBe('/server/firewall/new')
  })
})

describe('edit firewall rule', () => {
  it('saves the rule with its fingerprint', async () => {
    const app = renderApp('/server/firewall/9', serverRoutes({ 'PUT /server/firewall/rules/9': ok }))
    expect((await field(/^Port/)).value).toBe('80,443')
    fireEvent.change(await field(/^Port/), { target: { value: '80,443,8443' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(body(app, 'PUT /server/firewall/rules/9')).toBeTruthy())
    expect(body(app, 'PUT /server/firewall/rules/9')).toEqual({
      action: 'ACCEPT',
      protocol: 'TCP',
      port: '80,443,8443',
      ip: '0.0.0.0/0',
      comment: 'WEB',
      expect: 'expect-9',
    })
    expect(await screen.findByText('Changes have been saved.')).toBeTruthy()
  })

  it('locks every field of a protected rule', async () => {
    renderApp('/server/firewall/2', serverRoutes())
    expect((await field(/^Port/)).disabled).toBe(true)
    expect((await field(/^IP address/)).disabled).toBe(true)
    expect((await field(/^Comment/)).disabled).toBe(true)
    expect((await field(/^Comment/)).value).toBe('Panel')
    expect(screen.getByRole('combobox', { name: 'Action' }).hasAttribute('disabled')).toBe(true)
    expect(screen.getByRole('combobox', { name: 'Protocol' }).hasAttribute('disabled')).toBe(true)
    expect(screen.getByRole('button', { name: 'Save' }).hasAttribute('disabled')).toBe(true)
    expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull()
    expect(document.body.textContent).not.toMatch(/Hestia/)
  })

  it('reloads the rule and says so when it changed meanwhile', async () => {
    let gets = 0
    const app = renderApp(
      '/server/firewall/9',
      serverRoutes({
        'GET /server/firewall/rules/9': () => {
          gets++
          return { body: firewallRule('9', { port: gets > 1 ? '443' : '80,443', expect: `expect-9-${gets}` }) }
        },
        'PUT /server/firewall/rules/9': () => ({
          status: 409,
          body: { error: { code: 'changed', message: 'The rule has changed since the list was loaded.' } },
        }),
      }),
    )
    fireEvent.change(await field(/^Port/), { target: { value: '81' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain('reloaded')
    await waitFor(() =>
      expect(app.calls.filter((c) => c.key === 'GET /server/firewall/rules/9').length).toBeGreaterThan(1),
    )
    await waitFor(() => expect(gets).toBeGreaterThan(1))
  })

  it('deletes an ordinary rule from the form', async () => {
    const app = renderApp('/server/firewall/9', serverRoutes({ 'POST /server/firewall/rules/9/delete': ok }))
    fireEvent.click(await screen.findByRole('button', { name: 'Delete' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(body(app, 'POST /server/firewall/rules/9/delete')).toEqual({ expect: 'expect-9' }))
    await waitFor(() => expect(app.location()).toBe('/server/firewall'))
  })
})
