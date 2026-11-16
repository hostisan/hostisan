import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { database, dbRoutes, renderApp, type Route } from '../../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const hosts = [
  { host: 'localhost', type: 'mysql', charsets: [] },
  { host: 'db2.example', type: 'mysql', charsets: [] },
  { host: 'pg.example', type: 'pgsql', charsets: ['LATIN1', 'UTF8'] },
]
const options = {
  hosts,
  mysqlCharsets: ['utf8mb4', 'utf8', 'latin1'],
}

const reply = {
  status: 'ok',
  database: 'admin_shop',
  dbAdmin: 'phpMyAdmin',
  dbAdminUrl: 'https://panel.example/phpmyadmin/',
}

function addRoutes(over: Record<string, Route> = {}, opts: Record<string, unknown> = options) {
  return { ...dbRoutes({}, opts), 'POST /db': () => ({ body: reply }), ...over }
}

async function pick(label: RegExp, option: string) {
  fireEvent.click(await screen.findByRole('combobox', { name: label }))
  const opt = await screen.findByRole('option', { name: option })
  fireEvent.pointerDown(opt)
  fireEvent.mouseDown(opt)
  fireEvent.pointerUp(opt)
  fireEvent.mouseUp(opt)
  fireEvent.click(opt)
  await waitFor(() => expect(screen.queryByRole('option')).toBeNull())
}

const field = (label: RegExp | string) => screen.findByLabelText(label) as Promise<HTMLInputElement>
async function fillAdd(db: string, user: string, pw: string) {
  fireEvent.change(await field(/^Database/), { target: { value: db } })
  fireEvent.change(await field(/^Username/), { target: { value: user } })
  fireEvent.change(await field('Password'), { target: { value: pw } })
}
const openAdvanced = async () => fireEvent.click(await screen.findByRole('button', { name: /advanced options/i }))

describe('add database', () => {
  it('shows the admin gate first and the form after Continue', async () => {
    renderApp('/db/new', addRoutes({}, { ...options, adminGate: true }))
    expect(await screen.findByText('Consider using a standard user account')).toBeTruthy()
    expect(screen.queryByLabelText(/^Username/)).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
    expect(await field(/^Username/)).toBeTruthy()
  })

  it('shows the prefix hint and live prefixed names, and the length limit', async () => {
    renderApp('/db/new', addRoutes())
    expect(await screen.findByText(/Prefix/)).toBeTruthy()
    expect(screen.getByText(/will be automatically added to database name and database user/)).toBeTruthy()
    fireEvent.change(await field(/^Database/), { target: { value: 'shop' } })
    fireEvent.change(await field(/^Username/), { target: { value: 'bob' } })
    expect(screen.getByText('admin_shop')).toBeTruthy()
    expect(screen.getByText('admin_bob')).toBeTruthy()
    expect(screen.getByText('Maximum 32 characters length, including prefix')).toBeTruthy()
  })

  it('lists the MySQL charsets with utf8mb4 selected, and switches host and charset with the type', async () => {
    renderApp('/db/new', addRoutes())
    await openAdvanced()
    expect(await screen.findByRole('combobox', { name: /^Charset/ })).toHaveProperty('textContent', 'utf8mb4')
    fireEvent.click(screen.getByRole('combobox', { name: /^Charset/ }))
    expect((await screen.findAllByRole('option')).map((o) => o.textContent)).toEqual(['utf8mb4', 'utf8', 'latin1'])
    fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Escape' })
    cleanup()
    renderApp('/db/new', addRoutes())
    await pick(/^Type/, 'PostgreSQL')
    await openAdvanced()
    expect(screen.getByRole('combobox', { name: /^Host/ }).textContent).toContain('pg.example')
    expect(screen.getByRole('combobox', { name: /^Charset/ }).textContent).toBe('UTF8')
    fireEvent.click(screen.getByRole('combobox', { name: /^Charset/ }))
    expect((await screen.findAllByRole('option')).map((o) => o.textContent)).toEqual(['UTF8', 'LATIN1'])
  })

  it('offers only the chosen type hosts', async () => {
    renderApp('/db/new', addRoutes())
    await openAdvanced()
    fireEvent.click(await screen.findByRole('combobox', { name: /^Host/ }))
    const names = (await screen.findAllByRole('option')).map((o) => o.textContent)
    expect(names.some((n) => n?.includes('db2.example'))).toBe(true)
    expect(names.some((n) => n?.includes('pg.example'))).toBe(false)
  })

  it('posts bare names, toasts with links, then goes to the list', async () => {
    const app = renderApp('/db/new', addRoutes())
    await fillAdd('shop', 'bob', 'Passw0rdOK')
    fireEvent.click(screen.getByRole('button', { name: 'Add database' }))
    await waitFor(() => expect(app.location()).toBe('/db'))
    expect(app.calls.find((c) => c.key === 'POST /db')?.body).toEqual({
      database: 'shop',
      dbuser: 'bob',
      password: 'Passw0rdOK',
      type: 'mysql',
      host: 'localhost',
      charset: 'utf8mb4',
      email: '',
    })
    const edit = await screen.findByRole('link', { name: 'admin_shop' })
    expect(edit.getAttribute('href')).toBe('/db/admin_shop')
    const open = screen.getByRole('link', { name: /Open phpMyAdmin/ })
    expect(open.getAttribute('href')).toBe('https://panel.example/phpmyadmin/')
    expect(open.getAttribute('target')).toBe('_blank')
  })

  it('omits the manager link when the reply has no URL', async () => {
    const app = renderApp('/db/new', addRoutes({ 'POST /db': () => ({ body: { ...reply, dbAdminUrl: '' } }) }))
    await fillAdd('shop', 'bob', 'Passw0rdOK')
    fireEvent.click(screen.getByRole('button', { name: 'Add database' }))
    await waitFor(() => expect(app.location()).toBe('/db'))
    await screen.findByRole('link', { name: 'admin_shop' })
    expect(screen.queryByRole('link', { name: /Open phpMyAdmin/ })).toBeNull()
  })

  it('lists the translated blank fields and keeps the typed values', async () => {
    const app = renderApp(
      '/db/new',
      addRoutes({
        'POST /db': () => ({
          status: 400,
          body: { error: { code: 'invalid', message: 'Field "%s" can not be blank.', args: ['Username', 'Password'] } },
        }),
      }),
    )
    fireEvent.change(await field(/^Database/), { target: { value: 'shop' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add database' }))
    expect(await screen.findByText('Field "Username, Password" can not be blank.')).toBeTruthy()
    expect((screen.getByLabelText(/^Database/) as HTMLInputElement).value).toBe('shop')
    expect(app.location()).toBe('/db/new')
  })

  it('shows the weak-password message', async () => {
    renderApp(
      '/db/new',
      addRoutes({
        'POST /db': () => ({
          status: 400,
          body: { error: { code: 'invalid', message: 'Password does not match the minimum requirements.' } },
        }),
      }),
    )
    await fillAdd('shop', 'bob', 'weak')
    fireEvent.click(screen.getByRole('button', { name: 'Add database' }))
    expect(await screen.findByText('Password does not match the minimum requirements.')).toBeTruthy()
  })

  it('is read-only in a read-only session', async () => {
    renderApp('/db/new', {
      ...addRoutes(),
      ...dbRoutes({ readOnly: true }, options),
      'POST /db': () => ({ body: reply }),
    })
    await field(/^Username/)
    expect((screen.getByRole('button', { name: 'Add database' }) as HTMLButtonElement).disabled).toBe(true)
  })
})

function editRoutes(over: Record<string, Route> = {}, user: Record<string, unknown> = {}, db = {}) {
  return {
    ...dbRoutes(user, options),
    'GET /db/admin_wordpress': () => ({ body: database('admin_wordpress', { DBUSER: 'admin_wpuser', ...db }) }),
    'PUT /db/admin_wordpress': () => ({ body: { status: 'ok' } }),
    ...over,
  }
}

describe('edit database', () => {
  it('prefills the username without the prefix and disables the fixed fields', async () => {
    renderApp('/db/admin_wordpress', editRoutes())
    expect((await field(/^Username/)).value).toBe('wpuser')
    for (const label of [/^Database/, /^Type/, /^Host/, /^Charset/]) {
      expect((screen.getByLabelText(label) as HTMLInputElement).disabled).toBe(true)
    }
  })

  it('sends only the username when no password is typed, then goes back with a toast', async () => {
    const app = renderApp('/db/admin_wordpress', editRoutes())
    fireEvent.change(await field(/^Username/), { target: { value: 'newuser' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(app.location()).toBe('/db'))
    expect(app.calls.find((c) => c.key === 'PUT /db/admin_wordpress')?.body).toEqual({ dbuser: 'newuser' })
    expect(await screen.findByText('Changes have been saved.')).toBeTruthy()
  })

  it('sends the password when typed', async () => {
    const app = renderApp('/db/admin_wordpress', editRoutes())
    fireEvent.change(await field('Password'), { target: { value: 'Passw0rdOK' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(app.location()).toBe('/db'))
    expect(app.calls.find((c) => c.key === 'PUT /db/admin_wordpress')?.body).toEqual({
      dbuser: 'wpuser',
      password: 'Passw0rdOK',
    })
  })

  it('shows the error and stays on failure', async () => {
    const app = renderApp(
      '/db/admin_wordpress',
      editRoutes({
        'PUT /db/admin_wordpress': () => ({
          status: 400,
          body: { error: { code: 'invalid', message: 'Password does not match the minimum requirements.' } },
        }),
      }),
    )
    fireEvent.change(await field('Password'), { target: { value: 'weak' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText('Password does not match the minimum requirements.')).toBeTruthy()
    expect(app.location()).toBe('/db/admin_wordpress')
  })

  it('links to phpMyAdmin single sign-on for MySQL', async () => {
    renderApp('/db/admin_wordpress', editRoutes())
    const link = await screen.findByRole('link', { name: /phpMyAdmin/ })
    expect(link.getAttribute('href')).toBe('/api/v1/db/admin_wordpress/phpmyadmin')
    expect(link.getAttribute('target')).toBe('_blank')
  })

  it('hides the SSO link for PostgreSQL, without SSO, and when read-only', async () => {
    renderApp('/db/admin_wordpress', editRoutes({}, {}, { TYPE: 'pgsql', CHARSET: 'UTF8' }))
    await field(/^Username/)
    expect(screen.queryByRole('link', { name: /phpMyAdmin/ })).toBeNull()
    cleanup()
    renderApp('/db/admin_wordpress', {
      ...editRoutes(),
      ...{ 'GET /db/options': dbRoutes({}, { ...options, ssoEnabled: false })['GET /db/options'] },
    })
    await field(/^Username/)
    expect(screen.queryByRole('link', { name: /phpMyAdmin/ })).toBeNull()
    cleanup()
    renderApp('/db/admin_wordpress', editRoutes({}, { readOnly: true }))
    await field(/^Username/)
    expect(screen.queryByRole('link', { name: /phpMyAdmin/ })).toBeNull()
  })

  it('is read-only when the session is read-only or the database is suspended', async () => {
    renderApp('/db/admin_wordpress', editRoutes({}, { readOnly: true }))
    await field(/^Username/)
    expect((screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(true)
    cleanup()
    renderApp('/db/admin_wordpress', editRoutes({}, {}, { SUSPENDED: 'yes' }))
    expect(await screen.findByText('This database is suspended')).toBeTruthy()
    expect((screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(true)
    expect(
      within(document.body)
        .getByLabelText(/^Username/)
        .hasAttribute('disabled'),
    ).toBe(true)
  })

  it('shows not found for an unknown database', async () => {
    renderApp('/db/nope', {
      ...dbRoutes({}, options),
      'GET /db/nope': () => ({
        status: 404,
        body: { error: { code: 'not_found', message: "Error: database nope doesn't exist" } },
      }),
    })
    expect(await screen.findByText(/doesn't exist/)).toBeTruthy()
  })
})
