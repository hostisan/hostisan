import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { mailAccount, mailDomainDetail, mailOptions, mailRoutes } from '../../test/mailFixtures'
import { renderApp } from '../../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const open = async (email: string) =>
  fireEvent.click(await screen.findByRole('button', { name: `Actions for ${email}` }))
const ok = () => ({ body: { status: 'ok' } })

describe('mail accounts list', () => {
  it('lists the accounts with aliases, forwarding and auto reply', async () => {
    renderApp('/mail/example.com', mailRoutes())
    expect(await screen.findByRole('link', { name: 'info@example.com' })).toBeTruthy()
    expect(screen.getByRole('link', { name: 'sales@example.com' })).toBeTruthy()
    // info: two aliases -> first shown plus a +1 badge
    expect(screen.getByText('contact')).toBeTruthy()
    expect(screen.getAllByText('+1').length).toBeGreaterThan(0)
    // sales: forwards, forward only, auto reply on, unlimited quota
    expect(screen.getByText('owner@gmail.com')).toBeTruthy()
    expect(screen.getByText('Forward only')).toBeTruthy()
    expect(screen.getByText('Enabled')).toBeTruthy()
    expect(screen.getByText('2.00 MB of unlimited')).toBeTruthy()
  })

  it('counts a blackhole as forwarding enabled, and an empty auto reply value as enabled', async () => {
    const domain = mailDomainDetail('example.com')
    renderApp(
      '/mail/example.com',
      mailRoutes(
        {},
        {
          'GET /mail/example.com/accounts': () => ({
            body: {
              domain,
              accounts: [mailAccount('void', { FWD: ':blackhole:', AUTOREPLY: '' }), mailAccount('plain')],
            },
          }),
        },
      ),
    )
    await screen.findByRole('link', { name: 'void@example.com' })
    expect(screen.getByText('Discard all mail')).toBeTruthy()
    // only void has auto reply "" (enabled); plain has "no"
    expect(screen.getAllByText('Enabled')).toHaveLength(1)
  })

  it('shows the API error when the list fails', async () => {
    renderApp('/mail/missing.example', mailRoutes())
    expect(await screen.findByText("Error: mail domain missing.example doesn't exist")).toBeTruthy()
  })

  it('does not link a suspended account', async () => {
    renderApp('/mail/example.net', mailRoutes())
    expect(await screen.findByText('contact@example.net')).toBeTruthy()
    expect(screen.queryByRole('link', { name: 'contact@example.net' })).toBeNull()
  })
})

describe('row actions', () => {
  it('opens webmail at the system alias with the login prefilled (Parity 7)', async () => {
    renderApp('/mail/example.com', mailRoutes())
    await open('info@example.com')
    const item = await screen.findByRole('menuitem', { name: 'Open webmail' })
    expect(item.getAttribute('href')).toBe('https://webmail.example.com/?_user=info@example.com')
    expect(item.getAttribute('target')).toBe('_blank')
  })

  it('uses a non-default webmail alias', async () => {
    renderApp(
      '/mail/example.com',
      mailRoutes({}, { 'GET /mail/options': () => ({ body: mailOptions({ webmailAlias: 'mail' }) }) }),
    )
    await open('info@example.com')
    const item = await screen.findByRole('menuitem', { name: 'Open webmail' })
    expect(item.getAttribute('href')).toBe('https://mail.example.com/?_user=info@example.com')
  })

  it('offers Suspend and Delete to the admin, only Delete to a user', async () => {
    renderApp('/mail/example.com', mailRoutes())
    await open('info@example.com')
    expect(await screen.findByRole('menuitem', { name: 'Suspend' })).toBeTruthy()
    expect(screen.getByRole('menuitem', { name: 'Delete' })).toBeTruthy()
    cleanup()
    renderApp('/mail/example.com', mailRoutes({ role: 'user', name: 'bob' }))
    await open('info@example.com')
    expect(await screen.findByRole('menuitem', { name: 'Delete' })).toBeTruthy()
    expect(screen.queryByRole('menuitem', { name: 'Suspend' })).toBeNull()
    expect(screen.queryByRole('menuitem', { name: 'Unsuspend' })).toBeNull()
  })

  it('offers Unsuspend, not Edit, on a suspended account', async () => {
    renderApp('/mail/example.net', mailRoutes())
    await open('contact@example.net')
    expect(await screen.findByRole('menuitem', { name: 'Unsuspend' })).toBeTruthy()
    expect(screen.queryByRole('menuitem', { name: 'Edit mail account' })).toBeNull()
  })

  it('suspends through the API and refetches', async () => {
    const app = renderApp('/mail/example.com', mailRoutes({}, { 'POST /mail/example.com/accounts/info/suspend': ok }))
    await open('info@example.com')
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Suspend' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Suspend' }))
    await waitFor(() =>
      expect(app.calls.some((c) => c.key === 'POST /mail/example.com/accounts/info/suspend')).toBe(true),
    )
    await waitFor(() =>
      expect(app.calls.filter((c) => c.key === 'GET /mail/example.com/accounts').length).toBeGreaterThan(1),
    )
  })

  it('unsuspends through the API', async () => {
    const app = renderApp(
      '/mail/example.net',
      mailRoutes({}, { 'POST /mail/example.net/accounts/contact/unsuspend': ok }),
    )
    await open('contact@example.net')
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Unsuspend' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Unsuspend' }))
    await waitFor(() =>
      expect(app.calls.some((c) => c.key === 'POST /mail/example.net/accounts/contact/unsuspend')).toBe(true),
    )
  })

  it('deletes after typing the address, sending the account name', async () => {
    const app = renderApp('/mail/example.com', mailRoutes({}, { 'DELETE /mail/example.com/accounts/info': ok }))
    await open('info@example.com')
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.change(within(dialog).getByRole('textbox'), { target: { value: 'info@example.com' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete mail account' }))
    await waitFor(() => expect(app.calls.some((c) => c.key === 'DELETE /mail/example.com/accounts/info')).toBe(true))
  })
})

describe('bulk', () => {
  it('shows only Delete to a non-admin', async () => {
    renderApp('/mail/example.com', mailRoutes({ role: 'user', name: 'bob' }))
    await screen.findByRole('link', { name: 'info@example.com' })
    fireEvent.click(screen.getAllByRole('checkbox')[1])
    expect(await screen.findByRole('button', { name: 'Delete' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Suspend' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Unsuspend' })).toBeNull()
  })

  it('names the failed account and sends the account names', async () => {
    const app = renderApp(
      '/mail/example.com',
      mailRoutes(
        {},
        {
          'POST /mail/example.com/accounts/bulk': () => ({
            body: { status: 'ok', failed: [{ name: 'sales', message: "Error: mail account sales doesn't exist" }] },
          }),
        },
      ),
    )
    await screen.findByRole('link', { name: 'info@example.com' })
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select all mail accounts' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Suspend' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Suspend' }))
    expect(await screen.findByText("Error: mail account sales doesn't exist")).toBeTruthy()
    expect(screen.getAllByText('sales').length).toBeGreaterThan(0)
    expect(app.calls.find((c) => c.key === 'POST /mail/example.com/accounts/bulk')?.body).toEqual({
      action: 'suspend',
      names: expect.arrayContaining(['info', 'sales']),
    })
  })
})

describe('read-only', () => {
  it('hides add, bulk and the write actions but keeps webmail', async () => {
    renderApp('/mail/example.com', mailRoutes({ readOnly: true }))
    await screen.findByRole('link', { name: 'info@example.com' })
    expect(screen.queryByRole('link', { name: 'Add mail account' })).toBeNull()
    expect(screen.queryByRole('checkbox')).toBeNull()
    await open('info@example.com')
    expect(await screen.findByRole('menuitem', { name: 'Open webmail' })).toBeTruthy()
    for (const name of ['Suspend', 'Delete', 'Edit mail account'])
      expect(screen.queryByRole('menuitem', { name })).toBeNull()
  })
})

describe('no mock data', () => {
  it('the account screens do not import the mocks', () => {
    const files = import.meta.glob(
      ['./MailAccountsPage.tsx', './MailAccountFormPage.tsx', './EmailSettingsPanel.tsx'],
      {
        query: '?raw',
        import: 'default',
        eager: true,
      },
    ) as Record<string, string>
    expect(Object.keys(files)).toHaveLength(3)
    for (const [file, src] of Object.entries(files)) {
      expect(src, file).not.toMatch(/mocks\/(mail|session)/)
    }
  })
})
