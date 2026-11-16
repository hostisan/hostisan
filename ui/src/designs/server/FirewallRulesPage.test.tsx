import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderApp, signedIn, type Route } from '../../test/renderApp'
import { firewallRule, JOB_ID, jobRunning, serverRoutes } from '../../test/serverFixtures'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const ok: Route = () => ({ body: { status: 'ok' } })
const job: Route = () => ({ status: 202, body: { jobId: JOB_ID } })
type App = { calls: { key: string; body: unknown }[] }
const find = (app: App, key: string) => app.calls.find((c) => c.key === key)
const count = (app: App, key: string) => app.calls.filter((c) => c.key === key).length
const features = (over: Record<string, boolean> = {}) => ({
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
  firewallBans: true,
  ...over,
})
const row = async (id: string) =>
  (await screen.findByRole('link', { name: `Edit rule ${id}` })).closest('tr') as HTMLElement
const menu = async (id: string) => {
  await row(id)
  fireEvent.click(screen.getByRole('button', { name: `Actions for rule ${id}` }))
}
const confirm = async (label: string) =>
  fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: label }))

/** Three unprotected rules, so both ends can be tested. */
const plain = () => ({
  'GET /server/firewall': () => ({
    body: {
      enabled: true,
      bans: true,
      rules: [
        firewallRule('3', { comment: 'THREE' }),
        firewallRule('2', { comment: 'TWO', suspended: true }),
        firewallRule('1', { comment: 'ONE' }),
      ],
    },
  }),
})

describe('firewall rules', () => {
  it('lists the rules in evaluation order and calls the panel rule Panel', async () => {
    renderApp('/server/firewall', serverRoutes())
    await row('10')
    const ids = screen.getAllByRole('link', { name: /^Edit rule \d+$/ }).map((a) => a.textContent)
    expect(ids).toEqual(['10', '9', '8', '7', '6', '5', '4', '3', '2', '1'])
    expect(within(await row('2')).getByText('Panel')).toBeTruthy()
    expect(within(await row('10')).getByText('SSH')).toBeTruthy()
    expect(within(await row('1')).getByText('All')).toBeTruthy()
    expect(document.body.textContent).not.toMatch(/Hestia/)
    expect(screen.queryByText('HESTIA')).toBeNull()
  })

  it('gives protected rules no delete, suspend or move actions and no selection', async () => {
    renderApp('/server/firewall', serverRoutes())
    for (const id of ['10', '2']) {
      expect(
        within(await row(id))
          .getByRole('checkbox')
          .getAttribute('aria-disabled'),
      ).toBe('true')
      fireEvent.click(screen.getByRole('button', { name: `Actions for rule ${id}` }))
      expect(await screen.findByRole('menuitem', { name: 'Edit firewall rule' })).toBeTruthy()
      for (const gone of ['Delete', 'Suspend', 'Unsuspend', 'Move up', 'Move down']) {
        expect(screen.queryByRole('menuitem', { name: gone })).toBeNull()
      }
      fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Escape' })
      await waitFor(() => expect(screen.queryByRole('menuitem')).toBeNull())
    }
  })

  it('hides the move arrows at the ends of the list', async () => {
    renderApp('/server/firewall', serverRoutes(plain()))
    await menu('3')
    expect(screen.queryByRole('menuitem', { name: 'Move up' })).toBeNull()
    expect(screen.getByRole('menuitem', { name: 'Move down' })).toBeTruthy()
    fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('menuitem')).toBeNull())
    await menu('1')
    expect(screen.getByRole('menuitem', { name: 'Move up' })).toBeTruthy()
    expect(screen.queryByRole('menuitem', { name: 'Move down' })).toBeNull()
  })

  it.each([
    ['Suspend', 'suspend', '3'],
    ['Unsuspend', 'unsuspend', '2'],
    ['Delete', 'delete', '3'],
    ['Move up', 'move-up', '2'],
    ['Move down', 'move-down', '2'],
  ])('%s sends the rule fingerprint', async (item, action, id) => {
    const key = `POST /server/firewall/rules/${id}/${action}`
    const app = renderApp('/server/firewall', serverRoutes({ ...plain(), [key]: ok }))
    await menu(id)
    fireEvent.click(await screen.findByRole('menuitem', { name: item }))
    await confirm(item)
    await waitFor(() => expect(find(app, key)).toBeTruthy())
    expect(find(app, key)?.body).toEqual({ expect: `expect-${id}` })
  })

  it('deletes the selected rules with their fingerprints', async () => {
    const app = renderApp(
      '/server/firewall',
      serverRoutes({ 'POST /server/firewall/rules/bulk': () => ({ body: { status: 'ok', failed: [] } }) }),
    )
    fireEvent.click(within(await row('9')).getByRole('checkbox'))
    fireEvent.click(within(await row('8')).getByRole('checkbox'))
    fireEvent.click(await screen.findByRole('button', { name: 'Delete' }))
    await confirm('Delete')
    await waitFor(() => expect(find(app, 'POST /server/firewall/rules/bulk')).toBeTruthy())
    expect(find(app, 'POST /server/firewall/rules/bulk')?.body).toEqual({
      action: 'delete',
      items: [
        { id: '9', expect: 'expect-9' },
        { id: '8', expect: 'expect-8' },
      ],
    })
  })

  it('select-all skips protected rules, so the bulk delete never names one', async () => {
    const app = renderApp(
      '/server/firewall',
      serverRoutes({ 'POST /server/firewall/rules/bulk': () => ({ body: { status: 'ok', failed: [] } }) }),
    )
    await row('10')
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select all rules' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Delete' }))
    await confirm('Delete')
    await waitFor(() => expect(find(app, 'POST /server/firewall/rules/bulk')).toBeTruthy())
    const ids = (find(app, 'POST /server/firewall/rules/bulk')?.body as { items: { id: string }[] }).items.map(
      (i) => i.id,
    )
    expect(ids).toHaveLength(8)
    expect(ids).not.toContain('10')
    expect(ids).not.toContain('2')
  })

  it('shift-click across a protected rule never selects it', async () => {
    renderApp('/server/firewall', serverRoutes())
    const box = async (id: string) => within(await row(id)).getByRole('checkbox')
    fireEvent.click(await box('4'))
    fireEvent.click(await box('1'), { shiftKey: true })
    await screen.findByText('3 selected')
    expect((await box('2')).getAttribute('aria-checked')).not.toBe('true')
  })

  it('keeps the on/off switch disabled while the toggle job runs', async () => {
    renderApp(
      '/server/firewall',
      serverRoutes({
        'POST /server/firewall/toggle': job,
        [`GET /server/jobs/${JOB_ID}`]: () => ({ body: jobRunning() }),
      }),
    )
    const sw = await screen.findByRole('switch', { name: 'Firewall running' })
    fireEvent.click(sw)
    await confirm('Stop firewall')
    await screen.findByText('Server task')
    await waitFor(() =>
      expect(
        screen.getByRole('switch', { name: 'Firewall running' }).getAttribute('aria-disabled') === 'true' ||
          screen.getByRole('switch', { name: 'Firewall running' }).hasAttribute('disabled'),
      ).toBe(true),
    )
  })

  it('has an Order column', async () => {
    renderApp('/server/firewall', serverRoutes())
    expect(await screen.findByRole('columnheader', { name: 'Order' })).toBeTruthy()
  })

  it('shows a lockout refusal in a banner without an override', async () => {
    const app = renderApp(
      '/server/firewall',
      serverRoutes({
        ...plain(),
        'POST /server/firewall/rules/3/suspend': () => ({
          status: 409,
          body: { error: { code: 'lockout', message: 'This rule keeps the panel reachable and cannot be suspended.' } },
        }),
      }),
    )
    await menu('3')
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Suspend' }))
    await confirm('Suspend')
    expect((await screen.findByRole('alert')).textContent).toContain(
      'This rule keeps the panel reachable and cannot be suspended.',
    )
    expect(screen.queryByRole('button', { name: /anyway|override|force/i })).toBeNull()
    await waitFor(() => expect(count(app, 'GET /server/firewall')).toBeGreaterThan(1))
  })

  it('says the list changed and refetches when a rule was edited elsewhere', async () => {
    const app = renderApp(
      '/server/firewall',
      serverRoutes({
        ...plain(),
        'POST /server/firewall/rules/3/delete': () => ({
          status: 409,
          body: { error: { code: 'changed', message: 'The rule has changed since the list was loaded.' } },
        }),
      }),
    )
    await menu('3')
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete' }))
    await confirm('Delete')
    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain('The rule has changed since the list was loaded.')
    expect(alert.textContent).toContain('reloaded')
    await waitFor(() => expect(count(app, 'GET /server/firewall')).toBeGreaterThan(1))
  })

  it('shows the Banned IP addresses tab only with the bans feature', async () => {
    renderApp('/server/firewall', serverRoutes())
    expect(await screen.findByRole('tab', { name: 'Banned IP addresses' })).toBeTruthy()
    cleanup()
    renderApp(
      '/server/firewall',
      serverRoutes({ 'GET /bootstrap': () => ({ body: signedIn({ features: features({ firewallBans: false }) }) }) }),
    )
    await row('10')
    expect(screen.getByRole('tab', { name: 'IPset IP lists' })).toBeTruthy()
    expect(screen.queryByRole('tab', { name: 'Banned IP addresses' })).toBeNull()
  })

  it('shows an empty state while the firewall is disabled and can turn it on', async () => {
    const app = renderApp(
      '/server/firewall',
      serverRoutes({
        'GET /server/firewall': () => ({ body: { enabled: false, bans: false, rules: [] } }),
        'POST /server/firewall/toggle': job,
      }),
    )
    expect(await screen.findByText('The firewall is disabled')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Turn on firewall' }))
    await waitFor(() => expect(find(app, 'POST /server/firewall/toggle')).toBeTruthy())
    expect(find(app, 'POST /server/firewall/toggle')?.body).toEqual({ enabled: true })
  })

  it('shows the lockout guard when turning the firewall on is refused', async () => {
    renderApp(
      '/server/firewall',
      serverRoutes({
        'GET /server/firewall': () => ({ body: { enabled: false, bans: false, rules: [] } }),
        'POST /server/firewall/toggle': () => ({
          status: 409,
          body: {
            error: {
              code: 'lockout',
              message: "The firewall can't be turned on until active ACCEPT rules keep the panel and SSH ports open.",
            },
          },
        }),
      }),
    )
    fireEvent.click(await screen.findByRole('button', { name: 'Turn on firewall' }))
    expect((await screen.findByRole('alert')).textContent).toContain("can't be turned on")
  })

  it('stops the firewall after a confirmation', async () => {
    const app = renderApp('/server/firewall', serverRoutes({ 'POST /server/firewall/toggle': job }))
    fireEvent.click(await screen.findByRole('switch', { name: 'Firewall running' }))
    await confirm('Stop firewall')
    await waitFor(() => expect(find(app, 'POST /server/firewall/toggle')).toBeTruthy())
    expect(find(app, 'POST /server/firewall/toggle')?.body).toEqual({ enabled: false })
  })
})
