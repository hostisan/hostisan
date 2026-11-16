import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { mailOptions, mailRoutes } from '../../test/mailFixtures'
import { renderApp, type Route } from '../../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const ok = () => ({ body: { status: 'ok' } })
const addRoutes = (user: Record<string, unknown> = {}, over: Record<string, Route> = {}) =>
  mailRoutes(user, { 'POST /mail/example.com/accounts': ok, ...over })
const editRoutes = (user: Record<string, unknown> = {}, over: Record<string, Route> = {}) =>
  mailRoutes(user, {
    'PUT /mail/example.com/accounts/sales': ok,
    'PUT /mail/example.com/accounts/info': ok,
    ...over,
  })

const input = async (name: string) => (await screen.findByRole('textbox', { name })) as HTMLInputElement
const type = async (name: string, value: string) => fireEvent.change(await input(name), { target: { value } })
const password = async () => (await screen.findAllByLabelText(/^Password/))[0] as HTMLInputElement
const openAdvanced = async () => fireEvent.click(await screen.findByRole('button', { name: /advanced options/i }))
async function addTag(label: RegExp, value: string) {
  const field = await screen.findByLabelText(label)
  fireEvent.change(field, { target: { value } })
  fireEvent.keyDown(field, { key: 'Enter' })
}
const submit = (name: string) => fireEvent.click(screen.getByRole('button', { name }))
const body = (app: ReturnType<typeof renderApp>, key: string) => app.calls.find((c) => c.key === key)?.body

describe('add mail account', () => {
  it('restricts the account name to the characters the server accepts', async () => {
    renderApp('/mail/example.com/accounts/new', addRoutes())
    const field = await input('Account')
    expect(field.pattern).toBe('[A-Za-z0-9_.\\-]{1,64}')
    expect(field.title).toBe('Use letters, numbers, dots, hyphens or underscores')
  })

  it('generates a password', async () => {
    renderApp('/mail/example.com/accounts/new', addRoutes())
    expect((await password()).value).toBe('')
    fireEvent.click(await screen.findByRole('button', { name: 'Generate password' }))
    expect((await password()).value).toHaveLength(16)
  })

  it('posts the defaults: empty (unlimited) quota, no aliases, no forwards, the admin rate', async () => {
    const app = renderApp('/mail/example.com/accounts/new', addRoutes())
    await type('Account', 'hello')
    fireEvent.change(await password(), { target: { value: 'Passw0rdOK' } })
    submit('Add mail account')
    await waitFor(() => expect(app.calls.some((c) => c.key === 'POST /mail/example.com/accounts')).toBe(true))
    expect(body(app, 'POST /mail/example.com/accounts')).toEqual({
      account: 'hello',
      password: 'Passw0rdOK',
      quota: '',
      aliases: '',
      forwards: '',
      discard: false,
      forwardOnly: false,
      rate: '',
      sendTo: '',
    })
  })

  it('sends "unlimited" when the Unlimited box is ticked', async () => {
    const app = renderApp('/mail/example.com/accounts/new', addRoutes())
    await openAdvanced()
    fireEvent.click(await screen.findByRole('checkbox', { name: 'Unlimited' }))
    expect((await input('Quota')).disabled).toBe(true)
    await type('Account', 'hello')
    fireEvent.change(await password(), { target: { value: 'Passw0rdOK' } })
    submit('Add mail account')
    await waitFor(() => expect(body(app, 'POST /mail/example.com/accounts')).toBeTruthy())
    expect(body(app, 'POST /mail/example.com/accounts')).toMatchObject({ quota: 'unlimited' })
  })

  it('sends aliases and forwards as raw text', async () => {
    const app = renderApp('/mail/example.com/accounts/new', addRoutes())
    await openAdvanced()
    await addTag(/^Aliases/, 'one')
    await addTag(/^Aliases/, 'two')
    await addTag(/^Forward to/, 'a@example.org')
    await addTag(/^Forward to/, 'b@example.org')
    fireEvent.click(await screen.findByRole('checkbox', { name: 'Do not store forwarded mail' }))
    await type('Account', 'hello')
    fireEvent.change(await password(), { target: { value: 'Passw0rdOK' } })
    submit('Add mail account')
    await waitFor(() => expect(body(app, 'POST /mail/example.com/accounts')).toBeTruthy())
    const sent = body(app, 'POST /mail/example.com/accounts') as Record<string, unknown>
    expect(sent.aliases).toBe('one\ntwo')
    expect(sent.forwards).toBe('a@example.org\nb@example.org')
    expect(sent.forwardOnly).toBe(true)
    expect(sent.discard).toBe(false)
  })

  it('Discard all mail turns forward-only on, hides it and drops the forwards', async () => {
    const app = renderApp('/mail/example.com/accounts/new', addRoutes())
    await openAdvanced()
    await addTag(/^Forward to/, 'a@example.org')
    fireEvent.click(await screen.findByRole('checkbox', { name: 'Discard all mail' }))
    expect(screen.queryByRole('checkbox', { name: 'Do not store forwarded mail' })).toBeNull()
    expect(screen.queryByLabelText(/^Forward to/)).toBeNull()
    await type('Account', 'hello')
    fireEvent.change(await password(), { target: { value: 'Passw0rdOK' } })
    submit('Add mail account')
    await waitFor(() => expect(body(app, 'POST /mail/example.com/accounts')).toBeTruthy())
    expect(body(app, 'POST /mail/example.com/accounts')).toMatchObject({
      discard: true,
      forwardOnly: true,
      forwards: '',
    })
  })

  it('disables the rate limit and omits it unless canSetRate', async () => {
    const app = renderApp(
      '/mail/example.com/accounts/new',
      addRoutes({}, { 'GET /mail/options': () => ({ body: mailOptions({ canSetRate: false }) }) }),
    )
    await openAdvanced()
    expect((await input('Rate limit')).disabled).toBe(true)
    await type('Account', 'hello')
    fireEvent.change(await password(), { target: { value: 'Passw0rdOK' } })
    submit('Add mail account')
    await waitFor(() => expect(body(app, 'POST /mail/example.com/accounts')).toBeTruthy())
    expect(body(app, 'POST /mail/example.com/accounts')).not.toHaveProperty('rate')
  })

  it('sends a typed rate limit for an admin', async () => {
    const app = renderApp('/mail/example.com/accounts/new', addRoutes())
    await openAdvanced()
    await type('Rate limit', '50')
    await type('Account', 'hello')
    fireEvent.change(await password(), { target: { value: 'Passw0rdOK' } })
    submit('Add mail account')
    await waitFor(() => expect(body(app, 'POST /mail/example.com/accounts')).toBeTruthy())
    expect(body(app, 'POST /mail/example.com/accounts')).toMatchObject({ rate: '50' })
  })

  it('sends the credentials email address', async () => {
    const app = renderApp('/mail/example.com/accounts/new', addRoutes())
    await type('Account', 'hello')
    fireEvent.change(await password(), { target: { value: 'Passw0rdOK' } })
    fireEvent.change(await screen.findByLabelText(/^Email login credentials to/), {
      target: { value: 'marta@northwind-shop.io' },
    })
    submit('Add mail account')
    await waitFor(() => expect(body(app, 'POST /mail/example.com/accounts')).toBeTruthy())
    expect(body(app, 'POST /mail/example.com/accounts')).toMatchObject({ sendTo: 'marta@northwind-shop.io' })
  })

  it('toasts a link to the edit page, then goes to the accounts list', async () => {
    const app = renderApp('/mail/example.com/accounts/new', addRoutes())
    await type('Account', 'hello')
    fireEvent.change(await password(), { target: { value: 'Passw0rdOK' } })
    submit('Add mail account')
    await waitFor(() => expect(app.location()).toBe('/mail/example.com'))
    const link = await screen.findByRole('link', { name: 'hello@example.com' })
    expect(link.getAttribute('href')).toBe('/mail/example.com/accounts/hello')
    expect(link.closest('[role="alert"], [role="status"], div')?.parentElement?.textContent).toContain(
      'Mail account hello@example.com has been created successfully.',
    )
  })

  it('shows Hestia text and stays on the page when the add fails', async () => {
    const app = renderApp(
      '/mail/example.com/accounts/new',
      addRoutes(
        {},
        {
          'POST /mail/example.com/accounts': () => ({
            status: 400,
            body: { error: { code: 'exists', message: 'Error: mail account hello exists' } },
          }),
        },
      ),
    )
    await type('Account', 'hello')
    fireEvent.change(await password(), { target: { value: 'Passw0rdOK' } })
    submit('Add mail account')
    expect(await screen.findByText('Error: mail account hello exists')).toBeTruthy()
    expect(app.location()).toBe('/mail/example.com/accounts/new')
  })

  it('shows the API error for a missing domain', async () => {
    renderApp('/mail/missing.example/accounts/new', addRoutes())
    expect(await screen.findByText("Error: mail domain missing.example doesn't exist")).toBeTruthy()
  })

  it('shows the settings panel mirroring the inputs, with webmail and the mail host', async () => {
    renderApp('/mail/example.com/accounts/new', addRoutes())
    await type('Account', 'hello')
    fireEvent.change(await password(), { target: { value: 'Passw0rdOK' } })
    expect(await screen.findByText('hello@example.com')).toBeTruthy()
    expect(screen.getAllByText('Passw0rdOK').length).toBeGreaterThan(0)
    expect(screen.getByText('mail.example.com')).toBeTruthy()
    expect(screen.getByText('https://webmail.example.com/')).toBeTruthy()
    expect(screen.getByText('Port 993')).toBeTruthy()
  })

  it('leaves webmail out of the panel when the server has none', async () => {
    renderApp(
      '/mail/example.com/accounts/new',
      addRoutes({}, { 'GET /mail/options': () => ({ body: mailOptions({ webmail: false }) }) }),
    )
    await screen.findByText('mail.example.com')
    expect(screen.queryByText('https://webmail.example.com/')).toBeNull()
  })
})

describe('edit mail account', () => {
  it('prefills the account, including the auto reply message', async () => {
    renderApp('/mail/example.com/accounts/sales', editRoutes())
    expect(((await input('Quota')) as HTMLInputElement).value).toBe('Unlimited')
    expect((await screen.findByRole('checkbox', { name: 'Unlimited' })).getAttribute('aria-checked')).toBe('true')
    expect(await screen.findByText('owner@gmail.com')).toBeTruthy()
    expect(screen.getByText('archive@example.org')).toBeTruthy()
    expect((await input('Rate limit')).value).toBe('200')
    expect(((await screen.findByLabelText(/^Message/)) as HTMLTextAreaElement).value).toBe(
      'Thanks for your message.\nWe reply within one business day.\nSales team',
    )
    expect((await input('Account')).disabled).toBe(true)
    expect((await input('Account')).value).toBe('sales')
  })

  it('prefills the aliases and quota of a plain account', async () => {
    renderApp('/mail/example.com/accounts/info', editRoutes())
    expect(await screen.findByText('contact')).toBeTruthy()
    expect(screen.getByText('hello')).toBeTruthy()
    expect((await input('Quota')).value).toBe('1024')
  })

  it('saves without a password unless one is typed', async () => {
    const app = renderApp('/mail/example.com/accounts/sales', editRoutes())
    await screen.findByText('owner@gmail.com')
    submit('Save')
    await waitFor(() => expect(body(app, 'PUT /mail/example.com/accounts/sales')).toBeTruthy())
    expect(body(app, 'PUT /mail/example.com/accounts/sales')).toEqual({
      password: '',
      quota: 'unlimited',
      aliases: '',
      forwards: 'owner@gmail.com\narchive@example.org',
      discard: false,
      forwardOnly: true,
      autoreply: true,
      autoreplyMessage: 'Thanks for your message.\nWe reply within one business day.\nSales team',
      rate: '200',
      sendTo: '',
    })
    await waitFor(() => expect(app.location()).toBe('/mail/example.com'))
  })

  it('shows an unlimited account as Unlimited and saves it unchanged as "unlimited"', async () => {
    const app = renderApp('/mail/example.com/accounts/sales', editRoutes())
    await screen.findByText('owner@gmail.com')
    expect((await input('Quota')).value).toBe('Unlimited')
    submit('Save')
    await waitFor(() => expect(body(app, 'PUT /mail/example.com/accounts/sales')).toBeTruthy())
    expect(body(app, 'PUT /mail/example.com/accounts/sales')).toMatchObject({ quota: 'unlimited' })
  })

  it('starts an unticked unlimited account with an empty quota, not a hidden default', async () => {
    const app = renderApp('/mail/example.com/accounts/sales', editRoutes())
    await screen.findByText('owner@gmail.com')
    fireEvent.click(await screen.findByRole('checkbox', { name: 'Unlimited' }))
    expect((await input('Quota')).value).toBe('')
    submit('Save')
    await waitFor(() => expect(body(app, 'PUT /mail/example.com/accounts/sales')).toBeTruthy())
    expect(body(app, 'PUT /mail/example.com/accounts/sales')).toMatchObject({ quota: '' })
  })

  it('sends a typed password', async () => {
    const app = renderApp('/mail/example.com/accounts/info', editRoutes())
    await screen.findByText('contact')
    fireEvent.change(await password(), { target: { value: 'N3wPassword' } })
    submit('Save')
    await waitFor(() => expect(body(app, 'PUT /mail/example.com/accounts/info')).toBeTruthy())
    expect(body(app, 'PUT /mail/example.com/accounts/info')).toMatchObject({
      password: 'N3wPassword',
      aliases: 'contact\nhello',
      autoreply: false,
    })
  })

  it('shows the empty auto reply error from the server (Parity 5)', async () => {
    const app = renderApp(
      '/mail/example.com/accounts/info',
      editRoutes(
        {},
        {
          'PUT /mail/example.com/accounts/info': () => ({
            status: 400,
            body: { error: { code: 'invalid', message: 'Error: Field "Message" can not be blank.' } },
          }),
        },
      ),
    )
    await screen.findByText('contact')
    fireEvent.click((await screen.findAllByRole('switch', { name: 'Auto reply' }))[0])
    submit('Save')
    expect(await screen.findByText('Error: Field "Message" can not be blank.')).toBeTruthy()
    expect(body(app, 'PUT /mail/example.com/accounts/info')).toMatchObject({
      autoreply: true,
      autoreplyMessage: '',
    })
    expect(app.location()).toBe('/mail/example.com/accounts/info')
  })

  it('omits the rate for a non-admin and disables the field', async () => {
    const app = renderApp(
      '/mail/example.com/accounts/info',
      editRoutes(
        { role: 'user', name: 'bob' },
        { 'GET /mail/options': () => ({ body: mailOptions({ canSetRate: false }) }) },
      ),
    )
    await screen.findByText('contact')
    expect((await input('Rate limit')).disabled).toBe(true)
    submit('Save')
    await waitFor(() => expect(body(app, 'PUT /mail/example.com/accounts/info')).toBeTruthy())
    expect(body(app, 'PUT /mail/example.com/accounts/info')).not.toHaveProperty('rate')
  })

  it('cannot be saved when the account is suspended', async () => {
    renderApp('/mail/example.net/accounts/contact', editRoutes())
    expect(await screen.findByText('This mail account is suspended')).toBeTruthy()
    expect((screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('shows the API error for a missing account', async () => {
    renderApp(
      '/mail/example.com/accounts/ghost',
      editRoutes(
        {},
        {
          'GET /mail/example.com/accounts/ghost': () => ({
            status: 404,
            body: { error: { code: 'not_found', message: "Error: mail account ghost doesn't exist" } },
          }),
        },
      ),
    )
    expect(await screen.findByText("Error: mail account ghost doesn't exist")).toBeTruthy()
  })
})
