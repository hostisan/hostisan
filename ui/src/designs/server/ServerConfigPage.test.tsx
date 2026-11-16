import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { SettingsView } from '../../api/types.gen'
import { renderApp, type Route } from '../../test/renderApp'
import { JOB_ID, jobDone, jobFailed, serverRoutes, settingsView } from '../../test/serverFixtures'

// Kumo's tabs scroll the chosen tab into view, which jsdom doesn't implement
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

type App = { calls: { key: string; body: unknown }[] }
const job: Route = () => ({ status: 202, body: { jobId: JOB_ID } })
const view = (over: Partial<SettingsView>): Record<string, Route> => ({
  'GET /server/settings': () => ({ body: settingsView(over) }),
})
const puts = (app: App) =>
  app.calls.filter((c) => c.key === 'PUT /server/settings').map((c) => c.body as Record<string, unknown>)
const count = (app: App, key: string) => app.calls.filter((c) => c.key === key).length
const tab = async (name: string) => fireEvent.click(await screen.findByRole('tab', { name }))
// The form is loaded once its tabs are there (a loading page has a disabled Save too)
const save = async () => {
  await screen.findByRole('tab', { name: 'General' })
  fireEvent.click(await screen.findByRole('button', { name: 'Save' }))
}
const isDisabled = (el: HTMLElement) => el.hasAttribute('disabled') || el.getAttribute('aria-disabled') === 'true'
const field = (label: RegExp | string) => screen.findByLabelText(label) as Promise<HTMLInputElement>
// A FormSection aside renders twice (one copy per breakpoint), so take the first
const switchOf = async (name: string) => (await screen.findAllByRole('switch', { name }))[0]
async function pick(label: RegExp | string, option: string) {
  fireEvent.click(await screen.findByRole('combobox', { name: label }))
  const opt = await screen.findByRole('option', { name: option })
  fireEvent.pointerDown(opt)
  fireEvent.mouseDown(opt)
  fireEvent.pointerUp(opt)
  fireEvent.mouseUp(opt)
  fireEvent.click(opt)
  await waitFor(() => expect(screen.queryByRole('option')).toBeNull())
}
const routes = (over: Record<string, Route> = {}) => serverRoutes({ 'PUT /server/settings': job, ...over })

describe('Configure Server sections', () => {
  it('shows a section only when its system is present and sends no fields for a missing one', async () => {
    const base = settingsView()
    const app = renderApp(
      '/server/config',
      routes(
        view({
          dnsSystem: '',
          webBackend: '',
          phpVersions: [],
          defaultPhp: '',
          mail: { ...base.mail, system: '' },
          db: { ...base.db, system: '', mysql: false, pgsql: false, mysqlLocal: false },
        }),
      ),
    )
    await screen.findByRole('tab', { name: 'General' })
    for (const gone of ['DNS server', 'Mail server', 'Databases']) {
      expect(screen.queryByRole('tab', { name: gone })).toBeNull()
    }
    await save()
    await waitFor(() => expect(puts(app)).toHaveLength(1))
    const sent = puts(app)[0]
    for (const key of [
      'phpVersions',
      'defaultPhp',
      'webmailAlias',
      'smtpRelay',
      'smtpRelayHost',
      'smtpRelayPort',
      'smtpRelayUser',
      'smtpRelayPassword',
      'pmaAlias',
      'pgaAlias',
      'pmaSso',
      'pmaRestrict',
      'mysqlPassword',
    ]) {
      expect(sent, key).not.toHaveProperty(key)
    }
    expect(sent).toMatchObject({ hostname: 'server1.acme-hosting.net', timezone: 'UTC' })
  })

  it('shows PHP versions with their users and disables the protected ones', async () => {
    const app = renderApp('/server/config', routes())
    await tab('Web server')
    const protectedBox = await screen.findByRole('checkbox', { name: 'PHP 8.2' })
    expect(protectedBox.getAttribute('aria-disabled')).toBe('true')
    expect(screen.getByText('Used by example.com (admin)')).toBeTruthy()
    expect(screen.getByRole('checkbox', { name: 'PHP 8.3' }).getAttribute('aria-disabled')).not.toBe('true')
    // remove 8.3, install 8.4
    fireEvent.click(screen.getByRole('checkbox', { name: 'PHP 8.3' }))
    fireEvent.click(screen.getByRole('checkbox', { name: 'PHP 8.4' }))
    await save()
    await waitFor(() => expect(puts(app)).toHaveLength(1))
    expect(puts(app)[0]).toMatchObject({ phpVersions: ['8.2', '8.4'], defaultPhp: '8.2' })
  })

  it('shows System Protection only to the root admin', async () => {
    renderApp('/server/config', routes())
    await tab('Security')
    expect(await screen.findByText('System protection')).toBeTruthy()
    cleanup()
    renderApp('/server/config', routes(view({ canEditProtection: false })))
    await tab('Security')
    await screen.findByText('Login')
    expect(screen.queryByText('System protection')).toBeNull()
  })

  it('sends the protection switches only when the section is shown', async () => {
    const app = renderApp('/server/config', routes())
    await tab('Security')
    fireEvent.click(await switchOf('Hide account from other administrators'))
    await save()
    await waitFor(() => expect(puts(app)).toHaveLength(1))
    expect(puts(app)[0]).toMatchObject({ protectedAdmin: false, hideAdmin: true, hideServices: false })
    cleanup()
    const other = renderApp('/server/config', routes(view({ canEditProtection: false })))
    await save()
    await waitFor(() => expect(puts(other)).toHaveLength(1))
    for (const key of ['protectedAdmin', 'hideAdmin', 'hideServices']) expect(puts(other)[0]).not.toHaveProperty(key)
  })

  it('shows the allowed API addresses when either API is on', async () => {
    const base = settingsView().security
    renderApp('/server/config', routes(view({ security: { ...base, apiSystem: '0', legacyApi: false } })))
    await tab('Security')
    await screen.findByText('Login')
    expect(screen.queryByLabelText(/Allowed IP addresses for API/)).toBeNull()
    await pick('Enable legacy API access', 'Yes')
    expect(await screen.findByLabelText(/Allowed IP addresses for API/)).toBeTruthy()
    cleanup()
    renderApp('/server/config', routes())
    await tab('Security')
    expect(await screen.findByLabelText(/Allowed IP addresses for API/)).toBeTruthy()
  })

  it('has no documentation links and names no product in the phpMyAdmin restrict label', async () => {
    renderApp('/server/config', routes())
    await tab('Databases')
    expect(await screen.findByText('Restrict phpMyAdmin to panel login')).toBeTruthy()
    expect(
      screen.getByText(
        "Blocks phpMyAdmin's own login form; only reachable via the phpMyAdmin link inside the panel (requires SSO above).",
      ),
    ).toBeTruthy()
    for (const name of [
      'General',
      'Updates',
      'Web server',
      'DNS server',
      'Mail server',
      'Databases',
      'Backups',
      'SSL',
      'Security',
      'Policies',
      'Plugins',
    ]) {
      await tab(name)
      expect(document.querySelector('a[href*="hestiacp.com"]'), name).toBeNull()
      expect(screen.queryByText(/hestia/i), name).toBeNull()
      expect(document.body.textContent, name).not.toMatch(/hestia/i)
    }
  })

  it('links the MySQL and PostgreSQL support rows to their editors', async () => {
    renderApp('/server/config', routes())
    await tab('Databases')
    const mysql = await screen.findByRole('link', { name: 'Configure mysql' })
    expect(mysql.getAttribute('href')).toBe('/server/services/mysql')
    expect(screen.getByRole('link', { name: 'Configure postgresql' }).getAttribute('href')).toBe(
      '/server/services/postgresql',
    )
  })

  it('offers no self-signed certificate generator on the SSL section (owner ruling 3)', async () => {
    renderApp('/server/config', routes())
    await tab('SSL')
    await field('SSL certificate')
    expect(screen.queryByRole('link', { name: /self-signed/i })).toBeNull()
    expect(document.querySelector('a[href*="ssl/generate"], a[href*="generate/ssl"]')).toBeNull()
  })

  it('does not allow restricting phpMyAdmin while single sign on is off', async () => {
    const base = settingsView().db
    renderApp('/server/config', routes(view({ db: { ...base, pmaSso: false, pmaRestrict: false } })))
    await tab('Databases')
    const restrict = await screen.findByRole('switch', { name: 'Restrict phpMyAdmin to panel login' })
    expect(isDisabled(restrict)).toBe(true)
  })
})

describe('Configure Server secrets', () => {
  it('starts every secret empty and sends one only when typed', async () => {
    const base = settingsView()
    const app = renderApp(
      '/server/config',
      routes(
        view({
          mail: { ...base.mail, relay: true, relayHost: 'smtp.example.net', relayPort: '587', relayUser: 'mailer' },
        }),
      ),
    )
    await tab('Mail server')
    expect((await field('SMTP relay password')).value).toBe('')
    await tab('Databases')
    expect((await field('MySQL root password')).value).toBe('')
    await tab('Backups')
    expect((await field('Remote backup password')).value).toBe('')
    await tab('SSL')
    const key = await field(/SSL private key/)
    expect(key.value).toBe('')
    expect(key.closest('div')?.textContent ?? document.body.textContent).toContain(
      'Leave empty to keep the current key',
    )
    await save()
    await waitFor(() => expect(puts(app)).toHaveLength(1))
    for (const k of ['smtpRelayPassword', 'mysqlPassword', 'backupPassword', 'b2Key', 'sslKey']) {
      expect(puts(app)[0], k).not.toHaveProperty(k)
    }
    // typed secrets are sent
    await tab('Mail server')
    fireEvent.change(await field('SMTP relay password'), { target: { value: 'relay-secret' } })
    await tab('Databases')
    fireEvent.change(await field('MySQL root password'), { target: { value: 'root-secret' } })
    await tab('Backups')
    fireEvent.change(await field('Remote backup password'), { target: { value: 'backup-secret' } })
    await tab('SSL')
    fireEvent.change(await field(/SSL private key/), { target: { value: '-----BEGIN PRIVATE KEY-----' } })
    await save()
    await waitFor(() => expect(puts(app)).toHaveLength(2))
    expect(puts(app)[1]).toMatchObject({
      smtpRelayPassword: 'relay-secret',
      mysqlPassword: 'root-secret',
      backupPassword: 'backup-secret',
      sslKey: '-----BEGIN PRIVATE KEY-----',
    })
  })

  it('says an SFTP key cannot be pasted and shows the refusal', async () => {
    const keyNote = "SFTP keys can't be pasted here."
    renderApp(
      '/server/config',
      routes({
        'PUT /server/settings': () => ({
          status: 400,
          body: {
            error: {
              code: 'invalid',
              message:
                "SFTP keys can't be pasted here. Copy the key file to the server over SSH and add the backup host with v-add-backup-host.",
            },
          },
        }),
      }),
    )
    await tab('Backups')
    expect(document.body.textContent).toContain(keyNote)
    // FTP has no keys, so no note
    await pick('Protocol', 'FTP')
    await waitFor(() => expect(document.body.textContent).not.toContain(keyNote))
    await pick('Protocol', 'SFTP')
    fireEvent.change(await field('Remote backup password'), { target: { value: '-----BEGIN OPENSSH PRIVATE KEY-----' } })
    await save()
    expect((await screen.findByRole('alert')).textContent).toContain(keyNote)
  })

  it('sends the Backblaze key only when typed', async () => {
    const base = settingsView().backup
    const app = renderApp(
      '/server/config',
      routes(
        view({
          backup: {
            ...base,
            remote: {
              ...base.remote,
              type: 'b2',
              bucket: 'bk',
              keyId: 'id1',
              host: '',
              port: '',
              username: '',
              path: '',
            },
          },
        }),
      ),
    )
    await tab('Backups')
    expect((await field('Application key')).value).toBe('')
    fireEvent.change(await field('Application key'), { target: { value: 'b2-secret' } })
    await save()
    await waitFor(() => expect(puts(app)).toHaveLength(1))
    expect(puts(app)[0]).toMatchObject({ b2Key: 'b2-secret', backupRemote: { type: 'b2', bucket: 'bk', keyId: 'id1' } })
  })

  it('removes the remote when it is switched off', async () => {
    const app = renderApp('/server/config', routes())
    await tab('Backups')
    fireEvent.click(await switchOf('Remote backup'))
    await save()
    await waitFor(() => expect(puts(app)).toHaveLength(1))
    expect(puts(app)[0]).toMatchObject({ backupRemote: { type: '' } })
  })
})

describe('Configure Server save', () => {
  it('echoes the stored suspended-user policy while preview features are off', async () => {
    const base = settingsView()
    const app = renderApp(
      '/server/config',
      routes(view({ previewFeatures: false, policies: { ...base.policies, viewSuspended: true } })),
    )
    await tab('Policies')
    expect(screen.queryByRole('switch', { name: /Allow suspended users to log in/ })).toBeNull()
    await save()
    await waitFor(() => expect(puts(app)).toHaveLength(1))
    expect(puts(app)[0]).toMatchObject({ policies: { viewSuspended: true } })
  })

  it('sends the language for all users only with a changed language', async () => {
    const app = renderApp('/server/config', routes())
    await save()
    await waitFor(() => expect(puts(app)).toHaveLength(1))
    expect(puts(app)[0]).not.toHaveProperty('languageForAll')
    expect(puts(app)[0]).toMatchObject({ themeForAll: false, language: 'en' })
  })

  it('follows the job, lists what was applied and the notes, and reloads the session', async () => {
    const app = renderApp(
      '/server/config',
      routes({
        [`GET /server/jobs/${JOB_ID}`]: () => ({
          body: jobDone(
            {
              applied: ['Change hostname', 'Install PHP 8.4'],
              notes: ['A reboot is required to enable quota.'],
              refresh: false,
            },
            ['Change hostname', 'Install PHP 8.4'],
          ),
        }),
      }),
    )
    await screen.findByRole('tab', { name: 'General' })
    const before = count(app, 'GET /bootstrap')
    await save()
    const applied = await screen.findByRole('list', { name: 'Applied changes' })
    expect(within(applied).getByText('Change hostname')).toBeTruthy()
    // PHP job steps are translated with the version as argument
    expect(within(applied).getByText('Install PHP 8.4')).toBeTruthy()
    expect(screen.getByText('A reboot is required to enable quota.')).toBeTruthy()
    // every successful save refreshes the session, not only when refresh is set
    await waitFor(() => expect(count(app, 'GET /bootstrap')).toBeGreaterThan(before))
  })

  it('reloads the page when the server says so', async () => {
    const reload = vi.fn()
    vi.stubGlobal('location', { ...window.location, reload })
    renderApp(
      '/server/config',
      routes({
        [`GET /server/jobs/${JOB_ID}`]: () => ({
          body: jobDone({ applied: ['Change default PHP version'], notes: [], refresh: true }),
        }),
      }),
    )
    await save()
    await waitFor(() => expect(reload).toHaveBeenCalled())
  })

  it('shows what was applied before the failure, and the failure', async () => {
    renderApp(
      '/server/config',
      routes({
        [`GET /server/jobs/${JOB_ID}`]: () => ({
          body: jobFailed('Error: invalid timezone', ['Change hostname', 'Remove PHP 8.3', 'Change timezone']),
        }),
      }),
    )
    await save()
    const applied = await screen.findByRole('list', { name: 'Applied changes' })
    expect(within(applied).getByText('Change hostname')).toBeTruthy()
    expect(within(applied).getByText('Remove PHP 8.3')).toBeTruthy()
    // the failing step is not "applied"
    expect(within(applied).queryByText('Change timezone')).toBeNull()
    expect(screen.getByText('Error: invalid timezone')).toBeTruthy()
  })

  it('shows the firewall lockout refusal before any job starts', async () => {
    const app = renderApp(
      '/server/config',
      routes({
        'PUT /server/settings': () => ({
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
    await save()
    expect((await screen.findByRole('alert')).textContent).toContain("can't be turned on")
    expect(app.calls.some((c) => c.key.startsWith('GET /server/jobs/'))).toBe(false)
  })

  it('shows a validation refusal with its argument', async () => {
    renderApp(
      '/server/config',
      routes({
        'PUT /server/settings': () => ({
          status: 400,
          body: { error: { code: 'invalid', message: 'Invalid IP address: %s', args: ['999.1.1.1'] } },
        }),
      }),
    )
    await save()
    expect(await screen.findByText('Invalid IP address: 999.1.1.1')).toBeTruthy()
  })
})
