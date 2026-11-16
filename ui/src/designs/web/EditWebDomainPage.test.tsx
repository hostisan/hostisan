import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { WebDomainView, WebEditRequest, WebOptions, WebSSLRequest } from '../../api/types.gen'
import { renderApp, type Route } from '../../test/renderApp'
import { webDomainDetail, webDomainView, webOptions, webRoutes, webSsl } from '../../test/webFixtures'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const ok: Route = () => ({ body: { status: 'ok' } })
const field = (label: RegExp | string) => screen.findByLabelText(label) as Promise<HTMLInputElement>
const switchOf = async (name: string) => (await screen.findAllByRole('switch', { name }))[0] as HTMLInputElement
const body = <T,>(app: { calls: { key: string; body: unknown }[] }, key: string) =>
  app.calls.find((c) => c.key === key)?.body as T | undefined

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

function routes(
  view: WebDomainView,
  options: Partial<WebOptions> = {},
  over: Record<string, Route> = {},
  user: Record<string, unknown> = {},
) {
  const name = view.domain.NAME
  return webRoutes(user, {
    [`GET /web/${name}`]: () => ({ body: view }),
    'GET /web/options': () => ({ body: webOptions(options) }),
    [`PUT /web/${name}`]: ok,
    [`PUT /web/${name}/ssl`]: ok,
    ...over,
  })
}

const com = () =>
  webDomainView(
    webDomainDetail('example.com', {
      ALIAS: 'www.example.com,blog.example.com',
      FTP_USER: 'admin_dev',
      FTP_PATH: '/public_html',
      REDIRECT: 'www.example.com',
      REDIRECT_CODE: '302',
      CUSTOM_DOCROOT: '/home/admin/web/example.com/public_html/shop/',
      SSL_KEY_ALGO: 'ecdsa-384',
    }),
    { docroot: { enabled: true, domain: 'example.com', folder: 'shop' } },
    webSsl('example.com'),
  )

const save = () => fireEvent.click(screen.getByRole('button', { name: 'Save' }))

describe('edit web domain: prefill', () => {
  it('fills the form from the domain view', async () => {
    renderApp('/web/example.com', routes(com()))
    fireEvent.click(await screen.findByText('Advanced options'))
    expect(await screen.findByDisplayValue('shop')).toBeTruthy()
    expect((await field('Domain')).value).toBe('example.com')
    expect(screen.getByText('www.example.com')).toBeTruthy()
    expect(screen.getByDisplayValue('dev')).toBeTruthy()
    expect(screen.getByDisplayValue('/public_html')).toBeTruthy()
    expect(screen.getByRole('combobox', { name: /Key type/ }).textContent).toContain('ECDSA P-384')
    expect(
      screen.getByRole('radio', { name: /Redirect visitors to www.example.com/ }).hasAttribute('data-checked'),
    ).toBe(true)
    expect(screen.getByRole('combobox', { name: /Status code/ }).textContent).toContain('302')
  })

  it('shows the domain-not-found state', async () => {
    renderApp('/web/missing.example', routes(com()))
    expect(await screen.findByText('Web domain not found')).toBeTruthy()
  })
})

describe('edit web domain: visibility', () => {
  it('shows templates only with canEditTemplates', async () => {
    renderApp('/web/example.com', routes(com(), { canEditTemplates: false }))
    await field('Domain')
    expect(screen.queryByText('Server templates')).toBeNull()
  })

  it('shows FastCGI only with nginx, Backend only with webBackend, Proxy only with a proxy', async () => {
    renderApp('/web/example.com', routes(com(), { nginx: false, webBackend: '' }))
    fireEvent.click(await screen.findByText('Advanced options'))
    await screen.findAllByText('Server templates')
    expect(screen.queryByText(/Enable FastCGI cache/)).toBeNull()
    expect(screen.queryByText(/Backend template/)).toBeNull()
    expect(screen.getByRole('combobox', { name: /Proxy template/ })).toBeTruthy()
    cleanup()
    const noProxy = webDomainView(webDomainDetail('example.com', { PROXY: '' }))
    renderApp('/web/example.com', routes(noProxy, { nginx: true }))
    fireEvent.click(await screen.findByText('Advanced options'))
    expect(await screen.findByText(/Enable FastCGI cache/)).toBeTruthy()
    expect(screen.getByText(/Backend template/)).toBeTruthy()
    expect(screen.queryByRole('combobox', { name: /Proxy template/ })).toBeNull()
  })

  it('shows FTP only with the ftp option', async () => {
    renderApp('/web/example.com', routes(com(), { ftp: false }))
    await field('Domain')
    expect(screen.queryByText('FTP accounts')).toBeNull()
  })

  it('keeps only letters, digits and underscore in a new FTP name and shows the prefix', async () => {
    const view = webDomainView(webDomainDetail('example.com'))
    renderApp('/web/example.com', routes(view))
    fireEvent.click(await screen.findByText('Advanced options'))
    fireEvent.click(await switchOf('Additional FTP account(s)'))
    expect(await screen.findByText(/Prefix admin_ will be added/)).toBeTruthy()
    const name = await field(/^Username/)
    fireEvent.change(name, { target: { value: 'a-b c_9!' } })
    expect(name.value).toBe('abc_9')
  })

  it('shows the Purge button live with the FastCGI box and the proxy template', async () => {
    renderApp('/web/example.com', routes(com(), { nginx: true }))
    fireEvent.click(await screen.findByText('Advanced options'))
    expect(screen.queryByRole('button', { name: /Purge/ })).toBeNull()
    fireEvent.click(await screen.findByRole('checkbox', { name: /Enable FastCGI cache/ }))
    expect(await screen.findByRole('button', { name: /Purge/ })).toBeTruthy()
    fireEvent.click(await screen.findByRole('checkbox', { name: /Enable FastCGI cache/ }))
    expect(screen.queryByRole('button', { name: /Purge/ })).toBeNull()
    await pick(/Proxy template/, 'caching')
    expect(screen.getByRole('button', { name: /Purge/ })).toBeTruthy()
  })

  it('shows Quick install unless appInstaller is false', async () => {
    renderApp('/web/example.com', routes(com()))
    expect(await screen.findByRole('link', { name: /Quick install/ })).toBeTruthy()
    cleanup()
    renderApp('/web/example.com', routes(com(), { appInstaller: false }))
    await field('Domain')
    expect(screen.queryByRole('link', { name: /Quick install/ })).toBeNull()
  })

  it('offers Suspend to admins only', async () => {
    renderApp('/web/example.com', routes(com()))
    expect(await screen.findByRole('button', { name: /^Suspend/ })).toBeTruthy()
    cleanup()
    renderApp('/web/example.com', routes(com(), {}, {}, { role: 'user', name: 'bob' }))
    await field('Domain')
    expect(screen.queryByRole('button', { name: /^Suspend/ })).toBeNull()
  })

  it('offers Unsuspend to admins on a suspended domain only', async () => {
    const view = webDomainView(webDomainDetail('example.com', { SUSPENDED: 'yes' }))
    renderApp('/web/example.com', routes(view))
    expect(await screen.findByRole('button', { name: 'Unsuspend' })).toBeTruthy()
    cleanup()
    renderApp('/web/example.com', routes(view, {}, {}, { role: 'user', name: 'bob' }))
    await field('Domain')
    expect(screen.queryByRole('button', { name: 'Unsuspend' })).toBeNull()
  })

  it("shows the Let's Encrypt alias warning and the certificate info list", async () => {
    renderApp('/web/example.com', routes(com()))
    expect(await screen.findByText(/Let's Encrypt will obtain a new SSL certificate/)).toBeTruthy()
    expect(screen.getByText('Current certificate')).toBeTruthy()
    expect(screen.getByText('Issued to')).toBeTruthy()
  })

  it("requires the certificate fields once Let's Encrypt is turned off", async () => {
    renderApp('/web/example.com', routes(com()))
    fireEvent.click(await screen.findByRole('checkbox', { name: /Use Let's Encrypt/ }))
    const crt = await field('SSL certificate')
    expect(crt.required).toBe(true)
    expect((await field('SSL private key')).required).toBe(true)
  })
})

describe('edit web domain: saving', () => {
  it('sends the whole settings body and no SSL request when SSL is untouched', async () => {
    const app = renderApp('/web/example.com', routes(com(), { canEditTemplates: false }))
    await field('Domain')
    save()
    await waitFor(() => expect(app.location()).toBe('/web'))
    expect(await screen.findByText('Changes have been saved.')).toBeTruthy() // Hestia's msgid (ruling 19)
    const put = body<WebEditRequest>(app, 'PUT /web/example.com')!
    expect(put).toMatchObject({
      ip: '203.0.113.10',
      aliases: 'www.example.com\nblog.example.com',
      stats: 'awstats',
      statsAuth: false,
      redirect: true,
      redirectTarget: 'www.example.com',
      redirectCode: '302',
      docroot: true,
      docrootDomain: 'example.com',
      docrootFolder: 'shop',
    })
    expect(put.templates).toBeUndefined()
    expect(put.ftp).toEqual([
      {
        name: 'dev',
        password: '',
        path: '/public_html',
        prevPath: '/public_html',
        email: '',
        isNew: false,
        delete: false,
      },
    ])
    expect(body(app, 'PUT /web/example.com/ssl')).toBeUndefined()
  })

  it('fills empty proxy extensions with the server default list (owner ruling 6)', async () => {
    const view = webDomainView(webDomainDetail('example.com', { PROXY_EXT: '' }))
    const app = renderApp('/web/example.com', routes(view, { defaultProxyExt: ['css', 'js', 'png'] }))
    await field('Domain')
    save()
    await waitFor(() => expect(app.location()).toBe('/web'))
    expect(body<WebEditRequest>(app, 'PUT /web/example.com')!.templates?.proxyExt).toBe('css\njs\npng')
  })

  it('leaves out template fields that are not offered', async () => {
    const view = webDomainView(webDomainDetail('example.com', { PROXY: '' }))
    const app = renderApp('/web/example.com', routes(view, { nginx: false, webBackend: '' }))
    await field('Domain')
    save()
    await waitFor(() => expect(app.location()).toBe('/web'))
    expect(body<WebEditRequest>(app, 'PUT /web/example.com')!.templates).toEqual({ template: 'default' })
  })

  it('sends the SSL request after the settings when the section changed', async () => {
    const app = renderApp('/web/example.com', routes(com()))
    fireEvent.click(await screen.findByRole('checkbox', { name: /Enable HTTP Strict/ }))
    save()
    await waitFor(() => expect(app.location()).toBe('/web'))
    const keys = app.calls.map((c) => c.key).filter((k) => k.startsWith('PUT'))
    expect(keys).toEqual(['PUT /web/example.com', 'PUT /web/example.com/ssl'])
    expect(body<WebSSLRequest>(app, 'PUT /web/example.com/ssl')).toMatchObject({
      ssl: true,
      letsencrypt: true,
      hsts: true,
      reissue: false,
    })
  })

  it("asks for a new certificate when the aliases change with Let's Encrypt on", async () => {
    const app = renderApp('/web/example.com', routes(com()))
    const aliases = await field(/^Aliases/)
    fireEvent.change(aliases, { target: { value: 'shop.example.com' } })
    fireEvent.keyDown(aliases, { key: 'Enter' })
    save()
    await waitFor(() => expect(app.location()).toBe('/web'))
    expect(body<WebEditRequest>(app, 'PUT /web/example.com')!.aliases).toContain('shop.example.com')
    expect(body<WebSSLRequest>(app, 'PUT /web/example.com/ssl')!.reissue).toBe(true)
  })

  it('stops before the SSL request when the settings save fails, and shows the error', async () => {
    const app = renderApp(
      '/web/example.com',
      routes(
        com(),
        {},
        {
          'PUT /web/example.com': () => ({
            status: 400,
            body: { error: { code: 'invalid', message: 'Invalid %s', args: ['Password'] } },
          }),
        },
      ),
    )
    fireEvent.click(await screen.findByRole('checkbox', { name: /Enable HTTP Strict/ }))
    save()
    expect(await screen.findByText(/Invalid/)).toBeTruthy()
    expect(body(app, 'PUT /web/example.com/ssl')).toBeUndefined()
    expect(app.location()).toBe('/web/example.com')
  })

  it('does not resend finished steps when Save is retried after the SSL request failed', async () => {
    let sslCalls = 0
    const app = renderApp(
      '/web/example.com',
      routes(
        com(),
        {},
        {
          'PUT /web/example.com/ssl': () => {
            sslCalls += 1
            return sslCalls === 1
              ? { status: 400, body: { error: { code: 'invalid', message: 'Certificate failed' } } }
              : { body: { status: 'ok' } }
          },
        },
      ),
    )
    const aliases = await field(/^Aliases/)
    fireEvent.change(aliases, { target: { value: 'shop.example.com' } })
    fireEvent.keyDown(aliases, { key: 'Enter' })
    fireEvent.click(await screen.findByText('Advanced options'))
    fireEvent.click(await screen.findByRole('button', { name: /Add FTP account/ }))
    const names = await screen.findAllByLabelText(/^Username/)
    fireEvent.change(names[names.length - 1], { target: { value: 'up' } })
    fireEvent.change((await screen.findAllByLabelText('Password')).pop()!, { target: { value: 'secret-pass-1' } })
    save()
    expect(await screen.findByText('Certificate failed')).toBeTruthy()
    save()
    await waitFor(() => expect(app.location()).toBe('/web'))
    const puts = app.calls.filter((c) => c.key === 'PUT /web/example.com')
    expect(puts).toHaveLength(2)
    expect((puts[0].body as WebEditRequest).ftp.some((r) => r.isNew)).toBe(true)
    expect((puts[1].body as WebEditRequest).ftp.some((r) => r.isNew)).toBe(false)
    const ssls = app.calls.filter((c) => c.key === 'PUT /web/example.com/ssl')
    expect((ssls[1].body as WebSSLRequest).reissue).toBe(true)
  })

  it('requires a password for a new stats login and a new FTP row', async () => {
    renderApp('/web/example.com', routes(webDomainView(webDomainDetail('example.com', { STATS_USER: 'old' }))))
    const user = await field('Username')
    expect((screen.getAllByLabelText(/^Password/)[0] as HTMLInputElement).placeholder).toBe('Unchanged')
    fireEvent.change(user, { target: { value: 'newer' } })
    const pw = screen.getAllByLabelText(/^Password/)[0] as HTMLInputElement
    expect(pw.required).toBe(true)
    expect(pw.placeholder).toBe('')
  })

  it('marks every existing FTP row delete when FTP is unticked', async () => {
    const app = renderApp('/web/example.com', routes(com()))
    fireEvent.click(await screen.findByText('Advanced options'))
    fireEvent.click(await switchOf('Additional FTP account(s)'))
    save()
    await waitFor(() => expect(app.location()).toBe('/web'))
    expect(body<WebEditRequest>(app, 'PUT /web/example.com')!.ftp).toMatchObject([{ name: 'dev', delete: true }])
  })

  it('offers the credentials email on an existing FTP row only with a new password (owner ruling 7)', async () => {
    const view = webDomainView(webDomainDetail('example.com', { FTP_USER: 'admin_dev', FTP_PATH: '/public_html' }))
    const app = renderApp('/web/example.com', routes(view))
    fireEvent.click(await screen.findByText('Advanced options'))
    await screen.findByDisplayValue('dev')
    expect(screen.queryByLabelText(/^Send FTP credentials/)).toBeNull()
    fireEvent.change((await screen.findAllByLabelText('Password'))[0], { target: { value: 'new-pass-1' } })
    fireEvent.change(await field(/^Send FTP credentials/), { target: { value: 'a@b.example' } })
    save()
    await waitFor(() => expect(app.location()).toBe('/web'))
    expect(body<WebEditRequest>(app, 'PUT /web/example.com')!.ftp).toMatchObject([
      { name: 'dev', password: 'new-pass-1', email: 'a@b.example', isNew: false },
    ])
  })

  it('starts the FTP path hint at the custom document root domain (owner ruling 8)', async () => {
    const view = webDomainView(
      webDomainDetail('example.com', { FTP_USER: 'admin_dev', FTP_PATH: '/uploads', CUSTOM_DOCROOT: '/home/admin/web/shop.example/public_html/' }),
      { docroot: { enabled: true, domain: 'shop.example', folder: '' } },
    )
    renderApp('/web/example.com', routes(view))
    fireEvent.click(await screen.findByText('Advanced options'))
    expect(await screen.findByText('/home/admin/web/shop.example/uploads')).toBeTruthy()
  })

  it('sends a new FTP row with its password and email', async () => {
    const app = renderApp('/web/example.com', routes(webDomainView(webDomainDetail('example.com'))))
    fireEvent.click(await screen.findByText('Advanced options'))
    fireEvent.click(await switchOf('Additional FTP account(s)'))
    fireEvent.change(await field(/^Username/), { target: { value: 'up' } })
    fireEvent.change((await screen.findAllByLabelText('Password'))[0], { target: { value: 'secret-pass-1' } })
    fireEvent.change(await field(/^Send FTP credentials/), { target: { value: 'a@b.example' } })
    save()
    await waitFor(() => expect(app.location()).toBe('/web'))
    expect(body<WebEditRequest>(app, 'PUT /web/example.com')!.ftp).toEqual([
      {
        name: 'up',
        password: 'secret-pass-1',
        path: '',
        prevPath: '',
        email: 'a@b.example',
        isNew: true,
        delete: false,
      },
    ])
  })
})

describe('edit web domain: actions', () => {
  it('suspends after confirmation', async () => {
    const app = renderApp('/web/example.com', routes(com(), {}, { 'POST /web/example.com/suspend': ok }))
    fireEvent.click(await screen.findByRole('button', { name: /^Suspend/ }))
    fireEvent.click(await screen.findByRole('button', { name: 'Suspend', hidden: false }))
    await waitFor(() => expect(app.calls.some((c) => c.key === 'POST /web/example.com/suspend')).toBe(true))
  })
})

describe('edit web domain: delete', () => {
  it('does not fetch the deleted domain again', async () => {
    let gone = false
    const app = renderApp(
      '/web/example.com',
      routes(com(), {}, {
        'GET /web/example.com': () => (gone ? { status: 404, body: { error: { code: 'not_found' } } } : { body: com() }),
        'DELETE /web/example.com': () => {
          gone = true
          return { body: { status: 'ok' } }
        },
      }),
    )
    await screen.findByLabelText('Web statistics')
    const header = (await screen.findByRole('heading', { level: 1 })).closest('header') ?? document.body
    fireEvent.click((await within(header as HTMLElement).findAllByRole('button', { name: 'Delete' }))[0])
    fireEvent.change(await screen.findByLabelText('Type example.com to confirm deletion'), {
      target: { value: 'example.com' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Delete web domain' }))
    await waitFor(() => expect(app.location()).toBe('/web'))
    await waitFor(() => expect(app.calls.filter((c) => c.key === 'GET /web').length).toBeGreaterThan(1))
    const keys = app.calls.map((c) => c.key)
    const after = keys.slice(keys.indexOf('DELETE /web/example.com') + 1)
    expect(after.filter((k) => k.startsWith('GET /web/example.com'))).toEqual([])
  }, 20000)
})
