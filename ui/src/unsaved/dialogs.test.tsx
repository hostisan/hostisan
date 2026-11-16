// A dialog rendered inside a form page is not part of the form: typing in it does not
// mark the page changed, and submitting its own form does not save the page.
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { accountView, profileRoutes, usersRoutes } from '../test/accountFixtures'
import { renderApp, type Route } from '../test/renderApp'
import { webRoutes } from '../test/webFixtures'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const ok = () => ({ body: { status: 'ok', languageChanged: false } })
const unsaved = () => screen.queryByRole('dialog', { name: 'Unsaved changes' })
const writes = (app: ReturnType<typeof renderApp>) => app.calls.filter((c) => !c.key.startsWith('GET ')).map((c) => c.key)

function sidebarLink(name: string) {
  return within(document.querySelector('[data-sidebar="sidebar"]') as HTMLElement).getByRole('link', { name })
}

/** Clicks the sidebar's Web link and reports whether the page asked. */
async function leaveAsks(app: ReturnType<typeof renderApp>, from: string): Promise<boolean> {
  fireEvent.click(sidebarLink('Web'))
  await waitFor(() => expect(unsaved() !== null || app.location() !== from).toBe(true))
  return unsaved() !== null
}

const accountRoutes = (user: Record<string, unknown> = {}, over: Record<string, Route> = {}) => ({
  ...usersRoutes(user),
  ...profileRoutes(user, { 'PUT /account': ok, ...over }),
})
const setup = { secret: 'JBSWY3DPEHPK3PXP', otpauthUrl: 'otpauth://totp/x', qr: 'data:image/png;base64,AAAA' }

async function openTwoFactorSetup() {
  fireEvent.click(await screen.findByRole('button', { name: 'Set up two-factor authentication' }))
  await screen.findByAltText('2FA QR code')
  fireEvent.change(screen.getByLabelText('Code from your app'), { target: { value: '123456' } })
  fireEvent.change(screen.getAllByLabelText(/^Current password/).at(-1)!, { target: { value: 'pw' } })
}

describe('dialogs inside a form page', () => {
  it('deleting a web domain from its edit page does not ask', async () => {
    const app = renderApp('/web/example.com', webRoutes({}, { 'DELETE /web/example.com': ok }))
    const header = (await screen.findByRole('heading', { level: 1 })).closest('header') ?? document.body
    fireEvent.click((await within(header as HTMLElement).findAllByRole('button', { name: 'Delete' }))[0])
    fireEvent.change(await screen.findByLabelText('Type example.com to confirm deletion'), {
      target: { value: 'example.com' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Delete web domain' }))
    await waitFor(() => expect(app.location()).toBe('/web'))
    expect(writes(app)).toEqual(['DELETE /web/example.com'])
    expect(unsaved()).toBeNull()
  }, 20000)

  it('typing in the 2FA setup dialog does not mark the account form changed', async () => {
    const app = renderApp('/account', accountRoutes({}, { 'POST /account/2fa/setup': () => ({ body: setup }) }))
    await openTwoFactorSetup()
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByAltText('2FA QR code')).toBeNull())
    expect(await leaveAsks(app, '/account')).toBe(false)
    expect(app.location()).toBe('/web')
  }, 20000)

  it('typing the password in the 2FA off dialog does not mark the account form changed', async () => {
    const app = renderApp(
      '/account',
      accountRoutes({ role: 'user', name: 'bob' }, { 'GET /account': () => ({ body: accountView('bob', { self: true }) }) }),
    )
    fireEvent.click(await screen.findByRole('button', { name: 'Turn off' }))
    await screen.findByRole('dialog', { name: 'Turn off two-factor authentication' })
    fireEvent.change(screen.getAllByLabelText(/^Current password/).at(-1)!, { target: { value: 'pw' } })
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Turn off two-factor authentication' })).toBeNull())
    expect(await leaveAsks(app, '/account')).toBe(false)
  }, 20000)

  it("submitting the 2FA dialog's own form does not save the account", async () => {
    const app = renderApp(
      '/account',
      accountRoutes(
        {},
        {
          'POST /account/2fa/setup': () => ({ body: setup }),
          'POST /account/2fa/confirm': () => ({ body: { status: 'ok' } }),
        },
      ),
    )
    await openTwoFactorSetup()
    fireEvent.click(screen.getByRole('button', { name: 'Turn on' }))
    expect(await screen.findByText('Two-factor authentication enabled.')).toBeTruthy()
    // Give a stray account save the time to go out.
    await new Promise((r) => setTimeout(r, 50))
    expect(writes(app)).toEqual(['POST /account/2fa/setup', 'POST /account/2fa/confirm'])
  }, 20000)
})
