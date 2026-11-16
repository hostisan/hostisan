import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { mailDomain, mailOptions, mailRoutes } from '../../test/mailFixtures'
import { renderApp, type Route } from '../../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const open = (name: string) => fireEvent.click(screen.getByRole('button', { name: `Actions for ${name}` }))
const closeMenu = async () => {
  fireEvent.keyDown(screen.getByRole('menu'), { key: 'Escape' })
  await waitFor(() => expect(screen.queryByRole('menu')).toBeNull())
}
const ok: Route = () => ({ body: { status: 'ok' } })

describe('mail domains list', () => {
  it('lists the domains from the API with their columns', async () => {
    renderApp('/mail', mailRoutes())
    expect(await screen.findByRole('link', { name: 'example.com' })).toBeTruthy()
    const net = screen.getByRole('link', { name: 'example.net' }).closest('tr') as HTMLElement
    expect(within(net).getByText('Catch-all: boss@example.net')).toBeTruthy()
    const com = screen.getByRole('link', { name: 'example.com' }).closest('tr') as HTMLElement
    expect(within(com).getByText('2')).toBeTruthy() // accounts
    expect(within(com).getByRole('img', { name: 'Anti-virus: Enabled' })).toBeTruthy()
    expect(within(net).getByRole('img', { name: 'Anti-virus: Disabled' })).toBeTruthy()
    expect(within(net).getByText('Suspended')).toBeTruthy()
  })

  it('shows an icon as off only for the value "no"', async () => {
    const routes = mailRoutes(
      {},
      {
        'GET /mail': () => ({
          body: {
            domains: [mailDomain('blank.test', { ANTISPAM: '', DKIM: 'no', ANTIVIRUS: 'weird', SSL: '' })],
            usage: { domains: '1', domainsLimit: '5', suspended: '0', accounts: '2', accountsLimit: '50' },
          },
        }),
      },
    )
    renderApp('/mail', routes)
    const row = (await screen.findByRole('link', { name: 'blank.test' })).closest('tr') as HTMLElement
    expect(within(row).getByRole('img', { name: 'Spam filter: Enabled' })).toBeTruthy()
    expect(within(row).getByRole('img', { name: 'Anti-virus: Enabled' })).toBeTruthy()
    expect(within(row).getByRole('img', { name: 'DKIM: Disabled' })).toBeTruthy()
    expect(within(row).getByRole('img', { name: 'SSL: Enabled' })).toBeTruthy()
  })

  it('shows an error when the list fails', async () => {
    const routes = mailRoutes(
      {},
      { 'GET /mail': () => ({ status: 500, body: { error: { code: 'server_error', message: 'Error: boom' } } }) },
    )
    renderApp('/mail', routes)
    expect(await screen.findByText('Error: boom')).toBeTruthy()
  })

  it('opens by name for sortOrder name, newest first for date', async () => {
    const order = () => {
      const rows = screen.getAllByRole('row').map((r) => r.textContent ?? '')
      return rows.findIndex((r) => r.includes('example.net')) - rows.findIndex((r) => r.includes('example.com'))
    }
    renderApp('/mail', mailRoutes({ sortOrder: 'name' }))
    await screen.findByRole('link', { name: 'example.com' })
    expect(order()).toBeGreaterThan(0) // com first
    cleanup()
    renderApp('/mail', mailRoutes({ sortOrder: 'date' }))
    await screen.findByRole('link', { name: 'example.com' })
    expect(order()).toBeLessThan(0) // net (2026-04-01) is newer
  })

  it('shows usage and the suspended count in the aside', async () => {
    const routes = mailRoutes(
      {},
      {
        'GET /mail': () => ({
          body: {
            domains: [mailDomain('example.com')],
            usage: { domains: '2', domainsLimit: '5', suspended: '1', accounts: '7', accountsLimit: '50' },
          },
        }),
      },
    )
    renderApp('/mail', routes)
    expect(await screen.findByText('2 / 5')).toBeTruthy()
    expect(screen.getByText('7 / 50')).toBeTruthy()
    expect(screen.getByText('Mail domains (1 suspended)')).toBeTruthy()
  })
})

describe('row actions', () => {
  it('links webmail as https://<alias>.<domain>/ only for a domain with a client', async () => {
    renderApp(
      '/mail',
      mailRoutes({}, { 'GET /mail/options': () => ({ body: mailOptions({ webmailAlias: 'mailbox' }) }) }),
    )
    await screen.findByRole('link', { name: 'example.com' })
    open('example.com')
    const item = await screen.findByRole('menuitem', { name: 'Open webmail' })
    expect(item.getAttribute('href')).toBe('https://mailbox.example.com/')
    await closeMenu()
    open('example.net')
    await screen.findByRole('menuitem', { name: 'DNS records' })
    expect(screen.queryByRole('menuitem', { name: 'Open webmail' })).toBeNull()
  })

  it('offers Suspend and Unsuspend to the admin, and no add or edit on a suspended domain', async () => {
    renderApp('/mail', mailRoutes())
    await screen.findByRole('link', { name: 'example.com' })
    open('example.com')
    expect(await screen.findByRole('menuitem', { name: 'Suspend' })).toBeTruthy()
    expect(screen.getByRole('menuitem', { name: 'Edit mail domain' })).toBeTruthy()
    await closeMenu()
    open('example.net')
    expect(await screen.findByRole('menuitem', { name: 'Unsuspend' })).toBeTruthy()
    expect(screen.queryByRole('menuitem', { name: 'Edit mail domain' })).toBeNull()
    expect(screen.queryByRole('menuitem', { name: 'Add mail account' })).toBeNull()
  })

  it('offers a non-admin Delete but no Suspend or Unsuspend', async () => {
    renderApp('/mail', mailRoutes({ role: 'user', name: 'bob' }))
    await screen.findByRole('link', { name: 'example.com' })
    for (const name of ['example.com', 'example.net']) {
      open(name)
      expect(await screen.findByRole('menuitem', { name: 'Delete' })).toBeTruthy()
      expect(screen.queryByRole('menuitem', { name: 'Suspend' })).toBeNull()
      expect(screen.queryByRole('menuitem', { name: 'Unsuspend' })).toBeNull()
      await closeMenu()
    }
  })

  it('suspends through the API and refetches', async () => {
    const app = renderApp('/mail', mailRoutes({}, { 'POST /mail/example.com/suspend': ok }))
    await screen.findByRole('link', { name: 'example.com' })
    open('example.com')
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Suspend' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Suspend' }))
    await waitFor(() => expect(app.calls.some((c) => c.key === 'POST /mail/example.com/suspend')).toBe(true))
    await waitFor(() => expect(app.calls.filter((c) => c.key === 'GET /mail').length).toBeGreaterThan(1))
  })

  it('unsuspends through the API', async () => {
    const app = renderApp('/mail', mailRoutes({}, { 'POST /mail/example.net/unsuspend': ok }))
    await screen.findByRole('link', { name: 'example.net' })
    open('example.net')
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Unsuspend' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Unsuspend' }))
    await waitFor(() => expect(app.calls.some((c) => c.key === 'POST /mail/example.net/unsuspend')).toBe(true))
  })

  it('deletes after typing the name', async () => {
    const app = renderApp('/mail', mailRoutes({}, { 'DELETE /mail/example.com': ok }))
    await screen.findByRole('link', { name: 'example.com' })
    open('example.com')
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.change(within(dialog).getByRole('textbox'), { target: { value: 'example.com' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete mail domain' }))
    await waitFor(() => expect(app.calls.some((c) => c.key === 'DELETE /mail/example.com')).toBe(true))
  })
})

describe('bulk', () => {
  it('shows only Delete for a non-admin', async () => {
    renderApp('/mail', mailRoutes({ role: 'user', name: 'bob' }))
    await screen.findByRole('link', { name: 'example.com' })
    fireEvent.click(screen.getAllByRole('checkbox')[1])
    expect(await screen.findByRole('button', { name: 'Delete' })).toBeTruthy()
    for (const n of ['Rebuild all', 'Suspend', 'Unsuspend']) {
      expect(screen.queryByRole('button', { name: n })).toBeNull()
    }
  })

  it('shows Rebuild all, Suspend, Unsuspend and Delete to the admin, and sends rebuild', async () => {
    const app = renderApp(
      '/mail',
      mailRoutes({}, { 'POST /mail/bulk': () => ({ body: { status: 'ok', failed: [] } }) }),
    )
    await screen.findByRole('link', { name: 'example.com' })
    fireEvent.click(screen.getAllByRole('checkbox')[1])
    for (const n of ['Suspend', 'Unsuspend', 'Delete']) {
      expect(await screen.findByRole('button', { name: n })).toBeTruthy()
    }
    fireEvent.click(await screen.findByRole('button', { name: 'Rebuild all' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Rebuild all' }))
    await waitFor(() => expect(app.calls.some((c) => c.key === 'POST /mail/bulk')).toBe(true))
    expect(app.calls.find((c) => c.key === 'POST /mail/bulk')?.body).toEqual({
      action: 'rebuild',
      names: ['example.com'],
    })
  })

  it('names the failed domain after a bulk delete', async () => {
    const routes = mailRoutes(
      {},
      {
        'POST /mail/bulk': () => ({
          body: {
            status: 'ok',
            failed: [{ name: 'example.net', message: "Error: mail domain example.net doesn't exist" }],
          },
        }),
      },
    )
    const app = renderApp('/mail', routes)
    await screen.findByRole('link', { name: 'example.com' })
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select all mail domains' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Delete' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Delete' }))
    expect(await screen.findByText("Error: mail domain example.net doesn't exist")).toBeTruthy()
    expect(app.calls.find((c) => c.key === 'POST /mail/bulk')?.body).toEqual({
      action: 'delete',
      names: expect.arrayContaining(['example.com', 'example.net']),
    })
  })
})

describe('read-only', () => {
  it('hides add, bulk and row actions other than viewing', async () => {
    renderApp('/mail', mailRoutes({ readOnly: true }))
    await screen.findByRole('link', { name: 'example.com' })
    expect(screen.queryByRole('link', { name: 'Add mail domain' })).toBeNull()
    expect(screen.queryByRole('checkbox')).toBeNull()
    open('example.com')
    expect(await screen.findByRole('menuitem', { name: 'DNS records' })).toBeTruthy()
    for (const name of ['Suspend', 'Delete', 'Edit mail domain']) {
      expect(screen.queryByRole('menuitem', { name })).toBeNull()
    }
  })
})

describe('command palette', () => {
  it('fetches the mail domains only once opened and lists them', async () => {
    const app = renderApp('/web', mailRoutes())
    await screen.findByText('Hosting')
    expect(app.calls.some((c) => c.key === 'GET /mail')).toBe(false)
    fireEvent.keyDown(window, { key: 'k', ctrlKey: true })
    await waitFor(() => expect(app.calls.some((c) => c.key === 'GET /mail')).toBe(true))
    fireEvent.change(await screen.findByPlaceholderText(/Search pages, domains/), { target: { value: 'example.net' } })
    const item = await screen.findByText('example.net')
    expect(item.closest('[role=option]')).toBeTruthy()
  })
})

describe('no mock data', () => {
  it('the domain screens import nothing from mocks', () => {
    const files = import.meta.glob(
      [
        './MailDomainsPage.tsx',
        './MailDomainFormPage.tsx',
        './MailDnsPage.tsx',
        './MailAside.tsx',
        '../../layout/CommandMenu.tsx',
      ],
      { query: '?raw', import: 'default', eager: true },
    ) as Record<string, string>
    expect(Object.keys(files)).toHaveLength(5)
    for (const [file, src] of Object.entries(files)) expect(src, file).not.toMatch(/\/mocks\//)
  })

  it('has a sortable Created column, as Hestia has a Date sort', async () => {
    renderApp('/mail', mailRoutes())
    await screen.findAllByRole('link')
    expect(await screen.findByRole('button', { name: /Created/ })).toBeTruthy()
  })
})
