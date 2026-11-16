import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderApp, signedIn, type Route } from '../../test/renderApp'
import { serverRoutes } from '../../test/serverFixtures'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

type App = { calls: { key: string; body: unknown }[] }
const find = (app: App, key: string) => app.calls.find((c) => c.key === key)
const done: Route = () => ({ body: { status: 'ok', failed: [] } })
const ok: Route = () => ({ body: { status: 'ok' } })
const rowOf = async (ip: string, chain: string) => {
  await screen.findAllByText(ip)
  const cell = screen
    .getAllByText(chain, { selector: '*' })
    .map((e) => e.closest('tr'))
    .find((tr) => tr?.textContent?.includes(ip))
  return cell as HTMLElement
}
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

describe('banned IP addresses', () => {
  it('lists every ip and chain pair, newest first', async () => {
    renderApp('/server/firewall/banlist', serverRoutes())
    await rowOf('203.0.113.9', 'MAIL')
    expect(screen.getAllByText('203.0.113.9')).toHaveLength(2)
    expect(await rowOf('203.0.113.9', 'SSH')).toBeTruthy()
    expect(await rowOf('198.51.100.0/24', 'WEB')).toBeTruthy()
    expect(document.body.textContent).not.toMatch(/Hestia/)
  })

  it('unbans one pair, naming its chain', async () => {
    const app = renderApp('/server/firewall/banlist', serverRoutes({ 'POST /server/firewall/bans/delete': done }))
    const tr = await rowOf('203.0.113.9', 'SSH')
    fireEvent.click(within(tr).getByRole('button', { name: /^Actions for/ }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(find(app, 'POST /server/firewall/bans/delete')).toBeTruthy())
    expect(find(app, 'POST /server/firewall/bans/delete')?.body).toEqual({
      items: [{ ip: '203.0.113.9', chain: 'SSH' }],
    })
  })

  it('unbans the selected pairs', async () => {
    const app = renderApp('/server/firewall/banlist', serverRoutes({ 'POST /server/firewall/bans/delete': done }))
    fireEvent.click(within(await rowOf('203.0.113.9', 'MAIL')).getByRole('checkbox'))
    fireEvent.click(within(await rowOf('198.51.100.0/24', 'WEB')).getByRole('checkbox'))
    fireEvent.click(await screen.findByRole('button', { name: 'Delete' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(find(app, 'POST /server/firewall/bans/delete')).toBeTruthy())
    expect(find(app, 'POST /server/firewall/bans/delete')?.body).toEqual({
      items: [
        { ip: '203.0.113.9', chain: 'MAIL' },
        { ip: '198.51.100.0/24', chain: 'WEB' },
      ],
    })
  })

  it('names the pairs the core refused', async () => {
    renderApp(
      '/server/firewall/banlist',
      serverRoutes({
        'POST /server/firewall/bans/delete': () => ({
          body: { status: 'ok', failed: [{ name: '203.0.113.9 SSH', message: 'Error: ban not found' }] },
        }),
      }),
    )
    const tr = await rowOf('203.0.113.9', 'SSH')
    fireEvent.click(within(tr).getByRole('button', { name: /^Actions for/ }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Delete' }))
    expect((await screen.findByRole('alert')).textContent).toContain('203.0.113.9 SSH')
  })

  it('is unavailable without a firewall extension', async () => {
    const app = renderApp(
      '/server/firewall/banlist',
      serverRoutes({
        'GET /bootstrap': () => ({
          body: signedIn({
            features: {
              web: true,
              dns: true,
              db: true,
              mail: true,
              cron: true,
              backup: true,
              incremental: true,
              logs: true,
              accessKeys: true,
              server: true,
              firewall: true,
              firewallBans: false,
            },
          }),
        }),
      }),
    )
    expect(await screen.findByText('Banned IP addresses are not available')).toBeTruthy()
    expect(app.calls.some((c) => c.key === 'GET /server/firewall/bans')).toBe(false)
  })
})

describe('ban an IP address', () => {
  it('offers the seven chains and bans the address', async () => {
    const app = renderApp('/server/firewall/banlist/new', serverRoutes({ 'POST /server/firewall/bans': ok }))
    fireEvent.click(await screen.findByRole('combobox', { name: 'Banlist' }))
    expect((await screen.findAllByRole('option')).map((o) => o.textContent)).toEqual([
      'SSH',
      'WEB',
      'FTP',
      'DNS',
      'MAIL',
      'DB',
      'Panel',
    ])
    fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('option')).toBeNull())
    await pick('Banlist', 'MAIL')
    fireEvent.change(await screen.findByLabelText(/^IP address/), { target: { value: '198.51.100.7' } })
    fireEvent.click(screen.getByRole('button', { name: 'Ban IP address' }))
    await waitFor(() => expect(find(app, 'POST /server/firewall/bans')).toBeTruthy())
    expect(find(app, 'POST /server/firewall/bans')?.body).toEqual({ ip: '198.51.100.7', chain: 'MAIL' })
    await waitFor(() => expect(app.location()).toBe('/server/firewall/banlist'))
    expect(await screen.findByText('IP address has been banned successfully.')).toBeTruthy()
  })

  it.each([
    ['lockout', 'This ban would cover your own address.'],
    ['not_banned', 'The IP address was not banned: it is a server address, excluded, or already banned.'],
  ])('shows the %s refusal', async (code, message) => {
    const app = renderApp(
      '/server/firewall/banlist/new',
      serverRoutes({ 'POST /server/firewall/bans': () => ({ status: 409, body: { error: { code, message } } }) }),
    )
    fireEvent.change(await screen.findByLabelText(/^IP address/), { target: { value: '198.51.100.7' } })
    fireEvent.click(screen.getByRole('button', { name: 'Ban IP address' }))
    expect(await screen.findByText(message)).toBeTruthy()
    expect(app.location()).toBe('/server/firewall/banlist/new')
  })
})
