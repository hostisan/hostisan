import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { accountOptions, accountView, profileRoutes, usersRoutes } from '../../test/accountFixtures'
import { renderApp, type Route } from '../../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const ok = () => ({ body: { status: 'ok', languageChanged: false } })
const routes = (user: Record<string, unknown> = {}, over: Record<string, Route> = {}) => ({
  ...usersRoutes(user),
  ...profileRoutes(user, { 'PUT /account': ok, ...over }),
})
const text = async (name: string) => (await screen.findByRole('textbox', { name })) as HTMLInputElement
const body = (app: ReturnType<typeof renderApp>, key: string) => app.calls.find((c) => c.key === key)?.body
const bobView = (over = {}) => accountView('bob', { self: true, ...over })

describe('account page', () => {
  it('is prefilled from the API', async () => {
    renderApp('/account', routes())
    expect((await text('Contact name')).value).toBe('System Administrator')
    expect((await text('Email')).value).toBe('admin@acme-hosting.net')
    expect(document.body.textContent).not.toMatch(/Hestia/)
  })

  it('does not list users while logged in as another user (owner ruling 12)', async () => {
    const view = accountView('bob', { self: false })
    const app = renderApp('/account', routes({ look: 'bob' }, { 'GET /account': () => ({ body: view }) }))
    await text('Contact name')
    await new Promise((r) => setTimeout(r, 50))
    expect(app.calls.some((c) => c.key === 'GET /users')).toBe(false)
  })

  it('disables name and email when details are not editable', async () => {
    const view = bobView({ editable: { details: false, theme: true, admin: false } })
    renderApp('/account', routes({ role: 'user', name: 'bob' }, { 'GET /account': () => ({ body: view }) }))
    expect((await text('Contact name')).disabled).toBe(true)
    expect((await text('Email')).disabled).toBe(true)
  })

  it('hides the theme when it cannot change', async () => {
    const view = bobView({ editable: { details: true, theme: false, admin: false } })
    renderApp('/account', routes({ role: 'user', name: 'bob' }, { 'GET /account': () => ({ body: view }) }))
    await text('Contact name')
    expect(screen.queryByText('Theme')).toBeNull()
  })

  it('shows the theme when it can change', async () => {
    renderApp('/account', routes())
    await text('Contact name')
    expect(await screen.findByText('Theme')).toBeTruthy()
  })

  it('asks for the current password once a new one is typed, and sends both', async () => {
    const app = renderApp('/account', routes())
    await text('Contact name')
    expect(screen.queryByLabelText(/^Current password/)).toBeNull()
    fireEvent.change(screen.getByLabelText(/^New password/), { target: { value: 'Passw0rdOK' } })
    const current = (await screen.findByLabelText(/^Current password/)) as HTMLInputElement
    expect(current.required).toBe(true)
    fireEvent.change(current, { target: { value: 'oldpw' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(body(app, 'PUT /account')).toBeTruthy())
    expect(body(app, 'PUT /account')).toMatchObject({ password: 'Passw0rdOK', currentPassword: 'oldpw', name: 'System Administrator' })
  })

  it('sends the IP allow list as an array', async () => {
    const view = accountView('admin', { user: { ...accountView('admin').user, LOGIN_USE_IPLIST: 'yes', LOGIN_ALLOW_IPS: '10.0.0.1,10.0.0.2' } })
    const app = renderApp('/account', routes({}, { 'GET /account': () => ({ body: view }) }))
    await text('Contact name')
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(body(app, 'PUT /account')).toBeTruthy())
    expect(body(app, 'PUT /account')).toMatchObject({ useIpList: true, allowedIps: ['10.0.0.1', '10.0.0.2'] })
  })

  it('shows a save error from the server', async () => {
    renderApp(
      '/account',
      routes({}, { 'PUT /account': () => ({ status: 400, body: { error: { code: 'invalid', message: 'Please enter a valid email address.' } } }) }),
    )
    await text('Contact name')
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText('Please enter a valid email address.')).toBeTruthy()
  })
})

describe('two-factor authentication', () => {
  const setup = { secret: 'JBSWY3DPEHPK3PXP', otpauthUrl: 'otpauth://totp/x', qr: 'data:image/png;base64,AAAA' }

  it('walks through setup: QR, secret, wrong code, then success', async () => {
    let attempts = 0
    const app = renderApp(
      '/account',
      routes({}, {
        'POST /account/2fa/setup': () => ({ body: setup }),
        'POST /account/2fa/confirm': () =>
          ++attempts === 1
            ? { status: 400, body: { error: { code: 'invalid', message: 'The code is not correct.' } } }
            : { body: { status: 'ok' } },
      }),
    )
    fireEvent.click(await screen.findByRole('button', { name: 'Set up two-factor authentication' }))
    const qr = (await screen.findByAltText('2FA QR code')) as HTMLImageElement
    expect(qr.src).toBe(setup.qr)
    expect(await screen.findByText(setup.secret)).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Code from your app'), { target: { value: '123456' } })
    fireEvent.change(screen.getAllByLabelText(/^Current password/).at(-1)!, { target: { value: 'pw' } })
    fireEvent.click(screen.getByRole('button', { name: 'Turn on' }))
    expect(await screen.findByText('The code is not correct.')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Turn on' }))
    expect(await screen.findByText('Two-factor authentication enabled.')).toBeTruthy()
    expect(app.calls.filter((c) => c.key === 'POST /account/2fa/confirm').map((c) => c.body)).toEqual([
      { code: '123456', currentPassword: 'pw' },
      { code: '123456', currentPassword: 'pw' },
    ])
  })

  it('asks for the password before turning 2FA off', async () => {
    const app = renderApp(
      '/account',
      routes({ role: 'user', name: 'bob' }, { 'GET /account': () => ({ body: bobView() }), 'DELETE /account/2fa': ok }),
    )
    fireEvent.click(await screen.findByRole('button', { name: 'Turn off' }))
    const submit = (await screen.findAllByRole('button', { name: 'Turn off' })).at(-1) as HTMLButtonElement
    expect(submit.disabled).toBe(true)
    fireEvent.change(screen.getAllByLabelText(/^Current password/).at(-1)!, { target: { value: 'pw' } })
    fireEvent.click(submit)
    await waitFor(() => expect(body(app, 'DELETE /account/2fa')).toEqual({ currentPassword: 'pw' }))
  })
})

describe('options', () => {
  it('hides the name servers field when the system has no DNS', async () => {
    renderApp('/account', routes({}, { 'GET /account/options': () => ({ body: accountOptions({ dnsSystem: false }) }) }))
    await text('Contact name')
    fireEvent.click(screen.getByRole('button', { name: /advanced options/i }))
    expect(screen.queryByText('Name servers')).toBeNull()
  })
})

describe('what the page asks the server for', () => {
  const adminOnly = (app: ReturnType<typeof renderApp>) =>
    app.calls.map((c) => c.key).filter((k) => /^GET \/(users|packages)/.test(k))

  it('never touches admin-only endpoints for a non-admin', async () => {
    const app = renderApp(
      '/account',
      routes(
        { role: 'user', name: 'bob' },
        {
          'GET /account': () => ({ body: bobView({ editable: { details: true, theme: true, admin: false } }) }),
          'GET /account/options': () => ({ body: accountOptions({ packages: [], shells: [], phpVersions: [], defaultPhp: '' }) }),
        },
      ),
    )
    await text('Contact name')
    await screen.findByText(/You're on the/)
    expect(adminOnly(app)).toEqual([])
  })

  it('shows a non-admin their package limits from their own record', async () => {
    renderApp(
      '/account',
      routes(
        { role: 'user', name: 'bob' },
        {
          'GET /account': () => ({ body: bobView({ editable: { details: true, theme: true, admin: false } }) }),
          'GET /account/options': () => ({ body: accountOptions({ packages: [], shells: [], phpVersions: [], defaultPhp: '' }) }),
        },
      ),
    )
    await screen.findByText(/You're on the/)
    const dns = screen.getByText('DNS zones').parentElement as HTMLElement
    expect(dns.textContent).toContain('5')
    expect(screen.getByText('Web domains')).toBeTruthy()
  })

  it('does not list users on an admin\'s own page', async () => {
    const app = renderApp('/account', routes())
    await text('Contact name')
    expect(adminOnly(app).filter((k) => k !== 'GET /packages/system')).toEqual([])
  })

  it('omits an empty shell, PHP version and name servers', async () => {
    const base = accountView('admin')
    const view = { ...base, user: { ...base.user, SHELL: '', PHPCLI: '', NS: '' } }
    const app = renderApp(
      '/account',
      routes({}, { 'GET /account': () => ({ body: view }), 'GET /account/options': () => ({ body: accountOptions({ defaultPhp: '' }) }) }),
    )
    await text('Contact name')
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(body(app, 'PUT /account')).toBeTruthy())
    const sent = body(app, 'PUT /account') as Record<string, unknown>
    expect(sent).not.toHaveProperty('shell')
    expect(sent).not.toHaveProperty('phpCli')
    expect(sent).not.toHaveProperty('nameservers')
    expect(sent).toHaveProperty('package')
  })

  it('blocks saving an IP allow list with no addresses', async () => {
    const base = accountView('admin')
    const view = { ...base, user: { ...base.user, LOGIN_USE_IPLIST: 'yes', LOGIN_ALLOW_IPS: '' } }
    renderApp('/account', routes({}, { 'GET /account': () => ({ body: view }) }))
    await text('Contact name')
    expect((screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('shows the key and log links only when the server offers them', async () => {
    renderApp(
      '/account',
      routes({}, { 'GET /account/options': () => ({ body: accountOptions({ apiAccess: false, logsVisible: false }) }) }),
    )
    await text('Contact name')
    // The sidebar has its own Logs link, so look only at the page toolbar.
    const toolbar = screen.getByText('Manage SSH keys').closest('a')!.parentElement!
    expect(within(toolbar).queryByText('Logs')).toBeNull()
    expect(within(toolbar).queryByText('Access keys')).toBeNull()
  })
})
