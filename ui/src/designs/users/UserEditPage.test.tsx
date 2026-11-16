import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { accountOptions, accountView, profileRoutes, usersRoutes } from '../../test/accountFixtures'
import { renderApp, type Route } from '../../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const ok = () => ({ body: { status: 'ok', languageChanged: false } })
const routes = (over: Record<string, Route> = {}) => ({ ...usersRoutes(), ...profileRoutes({}, over) })
const text = async (name: string) => (await screen.findByRole('textbox', { name })) as HTMLInputElement
const body = (app: ReturnType<typeof renderApp>, key: string) => app.calls.find((c) => c.key === key)?.body

describe('edit user page', () => {
  it('shows the role, package and advanced sections for an admin', async () => {
    renderApp('/users/bob', routes({ 'PUT /users/bob': ok }))
    expect((await text('Contact name')).value).toBe('Bob Baker')
    expect((await screen.findAllByText('Role and package')).length).toBeGreaterThan(0)
    expect(screen.getAllByText('Role').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Package').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Advanced options').length).toBeGreaterThan(0)
    expect(document.body.textContent).not.toMatch(/Hestia/)
  })

  it('does not ask for the current password when an admin sets another user\'s password', async () => {
    const app = renderApp('/users/bob', routes({ 'PUT /users/bob': ok }))
    await text('Contact name')
    fireEvent.change(screen.getByLabelText(/^New password/), { target: { value: 'Passw0rdOK' } })
    expect(screen.queryByLabelText(/^Current password/)).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(body(app, 'PUT /users/bob')).toBeTruthy())
    expect(body(app, 'PUT /users/bob')).toMatchObject({ password: 'Passw0rdOK', currentPassword: '', package: 'default' })
  })

  it('sends admin fields', async () => {
    const app = renderApp('/users/bob', routes({ 'PUT /users/bob': ok }))
    await text('Contact name')
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(body(app, 'PUT /users/bob')).toBeTruthy())
    expect(body(app, 'PUT /users/bob')).toMatchObject({ loginDisabled: false, package: 'default', shell: 'nologin' })
  })

  it('unsuspends through the users mutation', async () => {
    const susp = accountView('susp', { self: false })
    let suspended = true
    const app = renderApp(
      '/users/susp',
      routes({
        'GET /users/susp': () => ({ body: { ...susp, user: { ...susp.user, SUSPENDED: suspended ? 'yes' : 'no' } } }),
        'POST /users/susp/unsuspend': () => {
          suspended = false
          return { body: { status: 'ok' } }
        },
      }),
    )
    fireEvent.click(await screen.findByRole('button', { name: 'Unsuspend' }))
    const confirm = (await screen.findAllByRole('button', { name: 'Unsuspend' })).at(-1)!
    fireEvent.click(confirm)
    await waitFor(() => expect(app.calls.some((c) => c.key === 'POST /users/susp/unsuspend')).toBe(true))
    await waitFor(() => expect(screen.queryByText('This user is suspended')).toBeNull())
  })

  it('has no own-account shortcuts in the toolbar', async () => {
    renderApp('/users/bob', routes())
    await text('Contact name')
    expect(screen.queryByText('Manage SSH keys')).toBeNull()
  })

  it('shows a not-found page for an unknown user', async () => {
    renderApp('/users/missing', routes())
    expect(await screen.findByText('There\'s no user named “missing”')).toBeTruthy()
  })

  it('does not fetch the system package, which the server never opens', async () => {
    const app = renderApp('/users/admin', routes())
    await text('Contact name')
    expect(await screen.findByText('Quota')).toBeTruthy()
    expect(app.calls.some((c) => c.key === 'GET /packages/system')).toBe(false)
  })

  it('turns 2FA off for another user without asking for a password', async () => {
    const app = renderApp('/users/bob', routes({ 'DELETE /users/bob/2fa': ok }))
    fireEvent.click(await screen.findByRole('button', { name: 'Turn off' }))
    expect(screen.queryByLabelText(/^Current password/)).toBeNull()
    const submit = (await screen.findAllByRole('button', { name: 'Turn off' })).at(-1) as HTMLButtonElement
    expect(submit.disabled).toBe(false)
    fireEvent.click(submit)
    await waitFor(() => expect(app.calls.some((c) => c.key === 'DELETE /users/bob/2fa')).toBe(true))
  })

  it('shows the name server limit message on the ninth entry', async () => {
    const view = accountView('bob', { self: false })
    const eight = Array.from({ length: 8 }, (_, i) => `ns${i + 1}.example.net`).join(',')
    renderApp('/users/bob', routes({ 'GET /users/bob': () => ({ body: { ...view, user: { ...view.user, NS: eight } } }) }))
    await text('Contact name')
    fireEvent.click(screen.getByRole('button', { name: /advanced options/i }))
    const field = await screen.findByLabelText(/^Name servers/)
    fireEvent.change(field, { target: { value: 'ns9.example.net' } })
    fireEvent.keyDown(field, { key: 'Enter' })
    expect(await screen.findByText('Up to 8 name servers are allowed.')).toBeTruthy()
    expect(document.body.textContent).not.toMatch(/Hestia/)
  })
})

describe('name servers', () => {
  it('refuses to save when every name server was removed', async () => {
    const app = renderApp('/users/bob', routes({ 'PUT /users/bob': ok }))
    await text('Contact name')
    fireEvent.click(screen.getByRole('button', { name: /advanced options/i }))
    for (const ns of ['ns1.acme-hosting.net', 'ns2.acme-hosting.net']) {
      fireEvent.click(await screen.findByRole('button', { name: `Remove ${ns}` }))
    }
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText('Field "Nameserver 1" can not be blank.')).toBeTruthy()
    expect(app.calls.some((c) => c.key === 'PUT /users/bob')).toBe(false)
  })
})

describe('add user page', () => {
  it('posts the welcome email address and offers "Log in as" after creating', async () => {
    const app = renderApp(
      '/users/new',
      {
        ...routes({ 'POST /users': ok, 'POST /auth/login-as': () => ({ body: { status: 'ok', landing: '/web' } }) }),
        'GET /account/options': () => ({ body: accountOptions() }),
      },
    )
    fireEvent.change(await text('Username'), { target: { value: 'newbie' } })
    fireEvent.change(await text('Contact name'), { target: { value: 'New Bie' } })
    fireEvent.change(await text('Email'), { target: { value: 'new@example.com' } })
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'Passw0rdOK' } })
    fireEvent.click(screen.getByRole('checkbox', { name: 'Send welcome email' }))
    const notify = (await text('Email login credentials to:')) as HTMLInputElement
    expect(notify.value).toBe('new@example.com')
    fireEvent.click(screen.getByRole('button', { name: 'Add user' }))
    await waitFor(() => expect(body(app, 'POST /users')).toBeTruthy())
    expect(body(app, 'POST /users')).toMatchObject({
      username: 'newbie',
      name: 'New Bie',
      email: 'new@example.com',
      password: 'Passw0rdOK',
      notify: 'new@example.com',
      role: 'user',
      package: 'default',
      loginDisabled: false,
    })
    expect(await screen.findByText('User newbie has been created successfully.')).toBeTruthy()
    fireEvent.click(await screen.findByRole('button', { name: 'Log in as newbie' }))
    await waitFor(() => expect(body(app, 'POST /auth/login-as')).toEqual({ user: 'newbie' }))
  })

  it('starts the language at the system language (owner ruling 13)', async () => {
    const app = renderApp('/users/new', {
      ...routes({ 'POST /users': ok }),
      'GET /account/options': () => ({ body: accountOptions({ systemLanguage: 'de' }) }),
    })
    fireEvent.change(await text('Username'), { target: { value: 'newbie' } })
    fireEvent.change(await text('Contact name'), { target: { value: 'New Bie' } })
    fireEvent.change(await text('Email'), { target: { value: 'new@example.com' } })
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'Passw0rdOK' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add user' }))
    await waitFor(() => expect(body(app, 'POST /users')).toMatchObject({ language: 'de' }))
  })

  it('sends no notify address when the welcome email is off', async () => {
    const app = renderApp('/users/new', routes({ 'POST /users': ok }))
    fireEvent.change(await text('Username'), { target: { value: 'newbie' } })
    fireEvent.change(await text('Contact name'), { target: { value: 'New Bie' } })
    fireEvent.change(await text('Email'), { target: { value: 'new@example.com' } })
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'Passw0rdOK' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add user' }))
    await waitFor(() => expect(body(app, 'POST /users')).toBeTruthy())
    expect(body(app, 'POST /users')).toMatchObject({ notify: '' })
  })
})
