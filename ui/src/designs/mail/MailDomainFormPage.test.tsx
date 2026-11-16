import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { mailDomainDetail, mailOptions, mailRoutes, mailSsl } from '../../test/mailFixtures'
import { renderApp, type Route } from '../../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const ok: Route = () => ({ body: { status: 'ok' } })

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
// A FormSection aside renders twice (one copy per breakpoint), so take the first
const switchOf = async (name: string) => (await screen.findAllByRole('switch', { name }))[0] as HTMLInputElement
const body = (app: { calls: { key: string; body: unknown }[] }, key: string) =>
  app.calls.find((c) => c.key === key)?.body

function addRoutes(over: Record<string, Route> = {}, options = mailOptions(), user = {}) {
  return mailRoutes(user, {
    'GET /mail/options': () => ({ body: options }),
    'POST /mail': ok,
    ...over,
  })
}

describe('add mail domain', () => {
  it('shows the admin gate first and the form after Continue', async () => {
    renderApp('/mail/new', addRoutes({}, mailOptions({ adminGate: true })))
    expect(await screen.findByText("You're signed in as admin")).toBeTruthy()
    expect(screen.queryByLabelText(/^Domain/)).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
    expect(await field(/^Domain/)).toBeTruthy()
  })

  it('has no gate for a non-admin account', async () => {
    renderApp('/mail/new', addRoutes())
    expect(await field(/^Domain/)).toBeTruthy()
    expect(screen.queryByText("You're signed in as admin")).toBeNull()
  })

  it('defaults the webmail client to Roundcube and offers Disabled last', async () => {
    renderApp('/mail/new', addRoutes())
    const select = await screen.findByRole('combobox', { name: /^Webmail client/ })
    expect(select.textContent).toContain('Roundcube')
    fireEvent.click(select)
    expect((await screen.findAllByRole('option')).map((o) => o.textContent)).toEqual([
      'Roundcube',
      'SnappyMail',
      'Disabled',
    ])
  })

  it('defaults to the first client when Roundcube is not installed', async () => {
    renderApp('/mail/new', addRoutes({}, mailOptions({ webmailClients: ['snappymail'] })))
    expect((await screen.findByRole('combobox', { name: /^Webmail client/ })).textContent).toContain('SnappyMail')
  })

  it('hides the webmail select without an IMAP system and sends no client', async () => {
    const app = renderApp('/mail/new', addRoutes({}, mailOptions({ imap: false })))
    fireEvent.change(await field(/^Domain/), { target: { value: 'example.org' } })
    expect(screen.queryByRole('combobox', { name: /^Webmail client/ })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Add mail domain' }))
    await waitFor(() => expect(app.location()).toBe('/mail'))
    expect((body(app, 'POST /mail') as { webmail: string }).webmail).toBe('')
  })

  it('hides the webmail select when the system has no client and sends no client', async () => {
    const app = renderApp('/mail/new', addRoutes({}, mailOptions({ webmail: false, webmailClients: [] })))
    fireEvent.change(await field(/^Domain/), { target: { value: 'example.org' } })
    expect(screen.queryByRole('combobox', { name: /^Webmail client/ })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Add mail domain' }))
    await waitFor(() => expect(app.location()).toBe('/mail'))
    expect((body(app, 'POST /mail') as { webmail: string }).webmail).toBe('')
  })

  it('hides antispam, reject and antivirus when the system is off and leaves them out of the request', async () => {
    const app = renderApp('/mail/new', addRoutes({}, mailOptions({ antispam: false, antivirus: false })))
    fireEvent.change(await field(/^Domain/), { target: { value: 'example.org' } })
    expect(screen.queryByRole('switch', { name: 'Spam filter' })).toBeNull()
    expect(screen.queryByRole('checkbox', { name: 'Reject spam' })).toBeNull()
    expect(screen.queryByRole('switch', { name: 'Anti-virus' })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Add mail domain' }))
    await waitFor(() => expect(app.location()).toBe('/mail'))
    const sent = body(app, 'POST /mail') as Record<string, unknown>
    for (const k of ['antispam', 'reject', 'antivirus']) expect(k in sent).toBe(false)
  })

  it('shows the DKIM selector only when DKIM is on', async () => {
    renderApp('/mail/new', addRoutes())
    expect(((await field(/^DKIM selector/)) as HTMLInputElement).value).toBe('mail')
    fireEvent.click(await switchOf('DKIM support'))
    await waitFor(() => expect(screen.queryByLabelText(/^DKIM selector/)).toBeNull())
  })

  it('posts the normalised request, toasts with a link to the domain, then goes to the list', async () => {
    const app = renderApp('/mail/new', addRoutes())
    fireEvent.change(await field(/^Domain/), { target: { value: ' WWW.Example.org ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add mail domain' }))
    await waitFor(() => expect(app.location()).toBe('/mail'))
    expect(body(app, 'POST /mail')).toEqual({
      domain: ' WWW.Example.org ',
      webmail: 'roundcube',
      antispam: true,
      reject: true,
      antivirus: true,
      dkim: true,
      dkimSelector: 'mail',
    })
    const link = await screen.findByRole('link', { name: 'example.org' })
    expect(link.getAttribute('href')).toBe('/mail/example.org')
    expect(screen.getByText(/has been created successfully\./)).toBeTruthy()
  })

  it('sends an empty webmail for Disabled', async () => {
    const app = renderApp('/mail/new', addRoutes())
    fireEvent.change(await field(/^Domain/), { target: { value: 'example.org' } })
    await pick(/^Webmail client/, 'Disabled')
    fireEvent.click(screen.getByRole('button', { name: 'Add mail domain' }))
    await waitFor(() => expect(app.location()).toBe('/mail'))
    expect((body(app, 'POST /mail') as { webmail: string }).webmail).toBe('')
  })

  it('sends the relay block with port 587 by default', async () => {
    const app = renderApp('/mail/new', addRoutes())
    fireEvent.change(await field(/^Domain/), { target: { value: 'example.org' } })
    expect(screen.queryByLabelText('Host')).toBeNull()
    fireEvent.click(await switchOf('SMTP relay'))
    fireEvent.change(await field('Host'), { target: { value: 'smtp.relay.test' } })
    expect((await field('Port')).value).toBe('587')
    fireEvent.change(await field('Username'), { target: { value: 'relayuser' } })
    fireEvent.change(await field('Password'), { target: { value: 'secret-pass' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add mail domain' }))
    await waitFor(() => expect(app.location()).toBe('/mail'))
    expect((body(app, 'POST /mail') as { smtpRelay: unknown }).smtpRelay).toEqual({
      host: 'smtp.relay.test',
      port: '587',
      user: 'relayuser',
      password: 'secret-pass',
    })
  })

  it('shows the translated blank-field error and keeps the typed values', async () => {
    const app = renderApp(
      '/mail/new',
      addRoutes({
        'POST /mail': () => ({
          status: 400,
          body: { error: { code: 'invalid', message: 'Field "%s" can not be blank.', args: ['Password'] } },
        }),
      }),
    )
    fireEvent.change(await field(/^Domain/), { target: { value: 'example.org' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add mail domain' }))
    expect(await screen.findByText('Field "Password" can not be blank.')).toBeTruthy()
    expect((screen.getByLabelText(/^Domain/) as HTMLInputElement).value).toBe('example.org')
    expect(app.location()).toBe('/mail/new')
  })

  it('disables Add in a read-only session', async () => {
    renderApp('/mail/new', addRoutes({}, mailOptions(), { readOnly: true }))
    await field(/^Domain/)
    expect((screen.getByRole('button', { name: 'Add mail domain' }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('shows an error when the options fail', async () => {
    renderApp(
      '/mail/new',
      addRoutes({
        'GET /mail/options': () => ({ status: 500, body: { error: { code: 'x', message: 'Error: nope' } } }),
      }),
    )
    expect(await screen.findByText('Error: nope')).toBeTruthy()
  })
})

const saveButton = () => screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement

function editRoutes(over: Record<string, Route> = {}, user = {}, options = mailOptions()) {
  return mailRoutes(user, {
    'GET /mail/options': () => ({ body: options }),
    'PUT /mail/example.com': ok,
    'PUT /mail/example.com/ssl': ok,
    'PUT /mail/example.net': ok,
    ...over,
  })
}

describe('edit mail domain', () => {
  it('prefills from the domain', async () => {
    renderApp('/mail/example.net/edit', editRoutes())
    expect((await field(/^Domain/)).value).toBe('example.net')
    expect((await field(/^Catch-all email/)).value).toBe('boss@example.net')
    expect((await field('Rate limit')).value).toBe('100')
    expect(screen.getByRole('combobox', { name: /^Webmail client/ }).textContent).toContain('Disabled')
    expect((await switchOf('Spam filter')).getAttribute('aria-checked')).toBe('false')
    expect(screen.getByText('This mail domain is suspended')).toBeTruthy()
  })

  it('disables the domain, and the rate unless canSetRate', async () => {
    renderApp('/mail/example.com/edit', editRoutes())
    expect((await field(/^Domain/)).disabled).toBe(true)
    expect((await field('Rate limit')).disabled).toBe(false)
    cleanup()
    renderApp('/mail/example.com/edit', editRoutes({}, {}, mailOptions({ canSetRate: false })))
    expect((await field('Rate limit')).disabled).toBe(true)
  })

  it('saves every offered field, then goes to the list with a toast, and does not touch SSL', async () => {
    const app = renderApp('/mail/example.com/edit', editRoutes())
    fireEvent.change(await field(/^Catch-all email/), { target: { value: 'hello@example.com' } })
    fireEvent.click(saveButton())
    await waitFor(() => expect(app.location()).toBe('/mail'))
    expect(body(app, 'PUT /mail/example.com')).toEqual({
      webmail: 'roundcube',
      catchall: 'hello@example.com',
      rate: '',
      antispam: true,
      reject: true,
      antivirus: true,
      dkim: true,
      dkimSelector: 'mail',
      smtpRelay: null,
    })
    expect(app.calls.some((c) => c.key === 'PUT /mail/example.com/ssl')).toBe(false)
    expect(await screen.findByText('Changes have been saved.')).toBeTruthy()
  })

  it('leaves out the fields the server does not offer', async () => {
    const app = renderApp(
      '/mail/example.com/edit',
      editRoutes(
        {},
        {},
        mailOptions({ antispam: false, antivirus: false, webmail: false, webmailClients: [], canSetRate: false }),
      ),
    )
    await field(/^Catch-all email/)
    fireEvent.click(saveButton())
    await waitFor(() => expect(app.location()).toBe('/mail'))
    expect(body(app, 'PUT /mail/example.com')).toEqual({
      catchall: '',
      dkim: true,
      dkimSelector: 'mail',
      smtpRelay: null,
    })
  })

  it('hides the webmail select and leaves webmail out without an IMAP system', async () => {
    const app = renderApp('/mail/example.com/edit', editRoutes({}, {}, mailOptions({ imap: false })))
    await field(/^Catch-all email/)
    expect(screen.queryByRole('combobox', { name: /^Webmail client/ })).toBeNull()
    fireEvent.click(saveButton())
    await waitFor(() => expect(app.location()).toBe('/mail'))
    expect(body(app, 'PUT /mail/example.com')).not.toHaveProperty('webmail')
  })

  it('sends "" for a domain with no webmail client that stays Disabled', async () => {
    const app = renderApp('/mail/example.net/edit', editRoutes())
    await field(/^Catch-all email/)
    fireEvent.click(saveButton())
    await waitFor(() => expect(app.location()).toBe('/mail'))
    const sent = body(app, 'PUT /mail/example.net') as Record<string, unknown>
    expect(sent.webmail).toBe('')
    expect(sent.antispam).toBe(false)
    expect(sent.rate).toBe('100')
  })

  it('sends the stored relay without a password when nothing changed, and null when switched off', async () => {
    const relayed = mailDomainDetail('example.com', {
      U_SMTP_RELAY: 'true',
      U_SMTP_RELAY_HOST: 'smtp.relay.test',
      U_SMTP_RELAY_PORT: '2525',
      U_SMTP_RELAY_USERNAME: 'relayuser',
    })
    const withRelay = {
      'GET /mail/example.com': () => ({ body: { domain: relayed, ssl: mailSsl('example.com') } }),
    }
    let app = renderApp('/mail/example.com/edit', editRoutes(withRelay))
    expect((await field('Host')).value).toBe('smtp.relay.test')
    expect((await field('Password')).value).toBe('')
    expect(
      screen.getByText(
        "The stored password can't be kept. Type it again whenever you change the host, port or username.",
      ),
    ).toBeTruthy()
    fireEvent.click(saveButton())
    await waitFor(() => expect(app.location()).toBe('/mail'))
    expect((body(app, 'PUT /mail/example.com') as { smtpRelay: unknown }).smtpRelay).toEqual({
      host: 'smtp.relay.test',
      port: '2525',
      user: 'relayuser',
      password: '',
    })
    cleanup()
    app = renderApp('/mail/example.com/edit', editRoutes(withRelay))
    fireEvent.click(await switchOf('SMTP relay'))
    fireEvent.click(saveButton())
    await waitFor(() => expect(app.location()).toBe('/mail'))
    expect((body(app, 'PUT /mail/example.com') as { smtpRelay: unknown }).smtpRelay).toBeNull()
  })

  it('shows the certificate info list', async () => {
    renderApp('/mail/example.com/edit', editRoutes())
    expect(await screen.findByText('Issued to')).toBeTruthy()
    expect(screen.getByText('mail.example.com')).toBeTruthy()
    expect(screen.getByText("C=US, O=Let's Encrypt, CN=E6")).toBeTruthy()
    expect(screen.getByText('ecdsa-with-SHA384')).toBeTruthy()
  })

  it("requires the certificate and key when Let's Encrypt is turned off with SSL on", async () => {
    renderApp('/mail/example.com/edit', editRoutes())
    expect(screen.queryByLabelText(/^SSL private key/)).toBeNull()
    fireEvent.click(await screen.findByRole('checkbox', { name: /Let's Encrypt/ }))
    const crt = (await screen.findByLabelText(/^SSL certificate(?! authority)/)) as HTMLTextAreaElement
    const key = (await screen.findByLabelText(/^SSL private key/)) as HTMLTextAreaElement
    expect(crt.required).toBe(true)
    expect(key.required).toBe(true)
    expect((screen.getByLabelText(/^SSL certificate authority/) as HTMLTextAreaElement).required).toBe(false)
  })

  it('links to the self-signed generator next to the certificate field, coming back to mail', async () => {
    renderApp('/mail/example.com/edit', editRoutes())
    await screen.findByRole('checkbox', { name: /Let's Encrypt/ })
    expect(screen.queryByRole('link', { name: 'Generate self-signed SSL certificate' })).toBeNull()
    fireEvent.click(screen.getByRole('checkbox', { name: /Let's Encrypt/ }))
    const link = await screen.findByRole('link', { name: 'Generate self-signed SSL certificate' })
    expect(link.getAttribute('href')).toBe('/web/example.com/ssl/generate?back=/mail/example.com/edit')
  })

  it("warns that the prefilled Let's Encrypt certificate stops renewing once Let's Encrypt is off", async () => {
    const hint = /It will no longer renew automatically/
    renderApp('/mail/example.com/edit', editRoutes())
    const leBox = await screen.findByRole('checkbox', { name: /Let's Encrypt/ })
    expect(screen.queryByText(hint)).toBeNull()
    fireEvent.click(leBox)
    expect(
      await screen.findByText("This is the current Let's Encrypt certificate. It will no longer renew automatically."),
    ).toBeTruthy()
    // A pasted certificate replaces it
    fireEvent.change(screen.getByLabelText(/^SSL certificate(?! authority)/), { target: { value: 'CRT-PEM' } })
    expect(screen.queryByText(hint)).toBeNull()
    cleanup()
    // A pasted certificate that was never from Let's Encrypt
    const manual = mailDomainDetail('example.com', { SSL: 'yes', LETSENCRYPT: 'no' })
    renderApp(
      '/mail/example.com/edit',
      editRoutes({ 'GET /mail/example.com': () => ({ body: { domain: manual, ssl: mailSsl('example.com') } }) }),
    )
    await screen.findByLabelText(/^SSL private key/)
    expect(screen.queryByText(hint)).toBeNull()
  })

  it('saves the SSL section through its own endpoint after the settings', async () => {
    const app = renderApp('/mail/example.com/edit', editRoutes())
    fireEvent.click(await screen.findByRole('checkbox', { name: /Let's Encrypt/ }))
    const crt = (await screen.findByLabelText(/^SSL certificate(?! authority)/)) as HTMLTextAreaElement
    fireEvent.change(crt, { target: { value: 'CRT-PEM' } })
    fireEvent.change(screen.getByLabelText(/^SSL private key/), { target: { value: 'KEY-PEM' } })
    fireEvent.click(saveButton())
    await waitFor(() => expect(app.location()).toBe('/mail'))
    const keys = app.calls.map((c) => c.key).filter((k) => k.startsWith('PUT '))
    expect(keys).toEqual(['PUT /mail/example.com', 'PUT /mail/example.com/ssl'])
    expect(body(app, 'PUT /mail/example.com/ssl')).toEqual({
      ssl: true,
      letsencrypt: false,
      keyAlgo: 'ecdsa-256',
      crt: 'CRT-PEM',
      key: 'KEY-PEM',
      ca: mailSsl('example.com').CA,
    })
  })

  it('turns SSL off through the SSL endpoint', async () => {
    const app = renderApp('/mail/example.com/edit', editRoutes())
    fireEvent.click(await switchOf('Enable SSL for this domain'))
    fireEvent.click(saveButton())
    await waitFor(() => expect(app.location()).toBe('/mail'))
    expect((body(app, 'PUT /mail/example.com/ssl') as { ssl: boolean }).ssl).toBe(false)
  })

  it('does not save SSL when the settings fail, and shows the error', async () => {
    const app = renderApp(
      '/mail/example.com/edit',
      editRoutes({
        'PUT /mail/example.com': () => ({
          status: 400,
          body: { error: { code: 'invalid', message: 'Error: invalid catchall format :: x' } },
        }),
      }),
    )
    fireEvent.click(await switchOf('Enable SSL for this domain'))
    fireEvent.click(saveButton())
    expect(await screen.findByText('Error: invalid catchall format :: x')).toBeTruthy()
    expect(app.calls.some((c) => c.key === 'PUT /mail/example.com/ssl')).toBe(false)
    expect(app.location()).toBe('/mail/example.com/edit')
  })

  it('is read-only in a read-only session', async () => {
    renderApp('/mail/example.com/edit', editRoutes({}, { readOnly: true }))
    await field(/^Catch-all email/)
    expect(saveButton().disabled).toBe(true)
  })

  it('shows not found for an unknown domain', async () => {
    renderApp('/mail/missing.example/edit', editRoutes())
    expect(await screen.findByText(/doesn't exist/)).toBeTruthy()
  })
})
