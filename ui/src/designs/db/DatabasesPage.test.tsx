import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { database, dbRoutes, renderApp } from '../../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const open = async (name: string) => {
  fireEvent.click(screen.getByRole('button', { name: `Actions for ${name}` }))
}

describe('databases list', () => {
  it('lists the databases from the API and marks the suspended one', async () => {
    renderApp('/db', dbRoutes())
    expect(await screen.findByRole('link', { name: 'admin_wordpress' })).toBeTruthy()
    // the suspended row is not a link, and carries the Suspended badge
    expect(screen.queryByRole('link', { name: 'admin_reports' })).toBeNull()
    expect(screen.getByText('admin_reports')).toBeTruthy()
    expect(screen.getAllByText('Suspended').length).toBeGreaterThan(0)
  })

  it('shows an error when the list fails', async () => {
    const routes = dbRoutes()
    routes['GET /db'] = () => ({ status: 500, body: { error: { code: 'server_error', message: 'Error: boom' } } })
    renderApp('/db', routes)
    expect(await screen.findByText('Error: boom')).toBeTruthy()
  })

  it('opens newest first for sortOrder date, by name otherwise', async () => {
    // admin_wordpress is newer (2026-06-01) but sorts after admin_reports by name
    const order = () => {
      const rows = screen.getAllByRole('row').map((r) => r.textContent ?? '')
      return rows.findIndex((r) => r.includes('admin_reports')) - rows.findIndex((r) => r.includes('admin_wordpress'))
    }
    renderApp('/db', dbRoutes({ sortOrder: 'name' }))
    await screen.findByRole('link', { name: 'admin_wordpress' })
    expect(order()).toBeLessThan(0) // reports first
    cleanup()
    renderApp('/db', dbRoutes({ sortOrder: 'date' }))
    await screen.findByRole('link', { name: 'admin_wordpress' })
    expect(order()).toBeGreaterThan(0) // wordpress first
  })

  it('shows used / limit and the suspended count in the aside', async () => {
    renderApp('/db', dbRoutes())
    expect(await screen.findByText('2 / 5')).toBeTruthy()
    expect(screen.getByText('Databases (1 suspended)')).toBeTruthy()
  })
})

describe('toolbar', () => {
  it('links phpMyAdmin and phpPgAdmin to the options URLs', async () => {
    renderApp('/db', dbRoutes())
    await screen.findByRole('link', { name: 'admin_wordpress' })
    const my = screen
      .getAllByRole('link', { name: /phpMyAdmin/ })
      .find((a) => a.getAttribute('href')?.startsWith('https://'))
    expect(my?.getAttribute('href')).toBe('https://panel.example/phpmyadmin/')
    const pg = screen
      .getAllByRole('link', { name: /phpPgAdmin/ })
      .find((a) => a.getAttribute('href')?.startsWith('https://'))
    expect(pg?.getAttribute('href')).toBe('https://panel.example/phppgadmin/')
  })

  it('hides a button whose URL is empty', async () => {
    renderApp('/db', dbRoutes({}, { phpPgAdminUrl: '' }))
    await screen.findByRole('link', { name: 'admin_wordpress' })
    await screen.findAllByRole('link', { name: /phpMyAdmin/ })
    expect(screen.queryByRole('link', { name: /phpPgAdmin/ })).toBeNull()
  })

  it('hides both when read-only', async () => {
    renderApp('/db', dbRoutes({ readOnly: true }))
    await screen.findByRole('link', { name: 'admin_wordpress' })
    expect(screen.queryByRole('link', { name: /phpMyAdmin/ })).toBeNull()
    expect(screen.queryByRole('link', { name: /phpPgAdmin/ })).toBeNull()
  })
})

describe('row actions', () => {
  it('offers SSO only on MySQL rows and opens it in a new tab', async () => {
    renderApp('/db', dbRoutes())
    await screen.findByRole('link', { name: 'admin_wordpress' })
    const sso = await screen.findByRole('link', { name: 'Open admin_wordpress in phpMyAdmin' })
    expect(sso.getAttribute('href')).toBe('/api/v1/db/admin_wordpress/phpmyadmin')
    expect(sso.getAttribute('target')).toBe('_blank')
    expect(sso.getAttribute('rel')).toContain('noopener')
    expect(screen.queryByRole('link', { name: /Open admin_reports/ })).toBeNull()
  })

  it('hides SSO when ssoEnabled is false', async () => {
    renderApp('/db', dbRoutes({}, { ssoEnabled: false }))
    await screen.findByRole('link', { name: 'admin_wordpress' })
    expect(screen.queryByRole('link', { name: /Open admin_wordpress in/ })).toBeNull()
  })

  it('download is a plain link', async () => {
    renderApp('/db', dbRoutes())
    await screen.findByRole('link', { name: 'admin_wordpress' })
    await open('admin_wordpress')
    const item = await screen.findByRole('menuitem', { name: 'Download database' })
    expect(item.getAttribute('href')).toBe('/api/v1/db/admin_wordpress/download')
  })

  it('offers Suspend and Unsuspend to the admin', async () => {
    renderApp('/db', dbRoutes())
    await screen.findByRole('link', { name: 'admin_wordpress' })
    await open('admin_wordpress')
    expect(await screen.findByRole('menuitem', { name: 'Suspend' })).toBeTruthy()
    fireEvent.keyDown(screen.getByRole('menu'), { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('menu')).toBeNull())
    await open('admin_reports')
    expect(await screen.findByRole('menuitem', { name: 'Unsuspend' })).toBeTruthy()
  })

  it('offers a non-admin Delete but no Suspend or Unsuspend', async () => {
    renderApp('/db', dbRoutes({ role: 'user', name: 'bob' }))
    await screen.findByRole('link', { name: 'admin_wordpress' })
    for (const name of ['admin_wordpress', 'admin_reports']) {
      await open(name)
      expect(await screen.findByRole('menuitem', { name: 'Delete' })).toBeTruthy()
      expect(screen.queryByRole('menuitem', { name: 'Suspend' })).toBeNull()
      expect(screen.queryByRole('menuitem', { name: 'Unsuspend' })).toBeNull()
      fireEvent.keyDown(screen.getByRole('menu'), { key: 'Escape' })
      await waitFor(() => expect(screen.queryByRole('menu')).toBeNull())
    }
  })

  it('suspends through the API and refetches', async () => {
    const routes = dbRoutes()
    routes['POST /db/admin_wordpress/suspend'] = () => ({ body: { status: 'ok' } })
    const app = renderApp('/db', routes)
    await screen.findByRole('link', { name: 'admin_wordpress' })
    await open('admin_wordpress')
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Suspend' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Suspend' }))
    await waitFor(() => expect(app.calls.some((c) => c.key === 'POST /db/admin_wordpress/suspend')).toBe(true))
    await waitFor(() => expect(app.calls.filter((c) => c.key === 'GET /db').length).toBeGreaterThan(1))
  })

  it('deletes after typing the name', async () => {
    const routes = dbRoutes()
    routes['DELETE /db/admin_wordpress'] = () => ({ body: { status: 'ok' } })
    const app = renderApp('/db', routes)
    await screen.findByRole('link', { name: 'admin_wordpress' })
    await open('admin_wordpress')
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.change(within(dialog).getByRole('textbox'), { target: { value: 'admin_wordpress' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete database' }))
    await waitFor(() => expect(app.calls.some((c) => c.key === 'DELETE /db/admin_wordpress')).toBe(true))
  })
})

describe('bulk', () => {
  it('shows only Delete for a non-admin', async () => {
    renderApp('/db', dbRoutes({ role: 'user', name: 'bob' }))
    await screen.findByRole('link', { name: 'admin_wordpress' })
    fireEvent.click(screen.getAllByRole('checkbox')[1])
    expect(await screen.findByRole('button', { name: 'Delete' })).toBeTruthy()
    for (const n of ['Rebuild', 'Suspend', 'Unsuspend']) expect(screen.queryByRole('button', { name: n })).toBeNull()
  })

  it('shows Rebuild, Suspend and Unsuspend to the admin', async () => {
    renderApp('/db', dbRoutes())
    await screen.findByRole('link', { name: 'admin_wordpress' })
    fireEvent.click(screen.getAllByRole('checkbox')[1])
    for (const n of ['Rebuild', 'Suspend', 'Unsuspend', 'Delete'])
      expect(await screen.findByRole('button', { name: n })).toBeTruthy()
  })

  it('names the failed database after a bulk delete', async () => {
    const routes = dbRoutes()
    routes['POST /db/bulk'] = () => ({
      body: {
        status: 'ok',
        failed: [{ name: 'admin_reports', message: "Error: database admin_reports doesn't exist" }],
      },
    })
    const app = renderApp('/db', routes)
    await screen.findByRole('link', { name: 'admin_wordpress' })
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select all databases' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Delete' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete' }))
    expect(await screen.findByText("Error: database admin_reports doesn't exist")).toBeTruthy()
    expect(screen.getAllByText('admin_reports').length).toBeGreaterThan(0)
    const call = app.calls.find((c) => c.key === 'POST /db/bulk')
    expect(call?.body).toEqual({
      action: 'delete',
      names: expect.arrayContaining(['admin_wordpress', 'admin_reports']),
    })
  })
})

describe('read-only', () => {
  it('hides add, bulk and row actions', async () => {
    renderApp('/db', dbRoutes({ readOnly: true }))
    await screen.findByRole('link', { name: 'admin_wordpress' })
    expect(screen.queryByRole('link', { name: 'Add database' })).toBeNull()
    expect(screen.queryByRole('checkbox')).toBeNull()
    expect(screen.queryByRole('link', { name: /Open admin_wordpress in/ })).toBeNull()
    await open('admin_wordpress')
    expect(await screen.findByRole('menuitem', { name: 'Download database' })).toBeTruthy()
    for (const name of ['Suspend', 'Delete', 'Edit database'])
      expect(screen.queryByRole('menuitem', { name })).toBeNull()
  })
})

describe('sidebar and palette', () => {
  it('hides Databases when the package has no database feature', async () => {
    renderApp('/web', dbRoutes({ features: { dns: true, db: false } }))
    await screen.findByText('Hosting')
    expect(screen.queryByRole('link', { name: 'Databases' })).toBeNull()
  })
  it('shows Databases otherwise', async () => {
    renderApp('/web', dbRoutes())
    expect(await screen.findByRole('link', { name: 'Databases' })).toBeTruthy()
  })
  it('fetches databases for the palette only once opened', async () => {
    const app = renderApp('/web', dbRoutes())
    await screen.findByText('Hosting')
    expect(app.calls.some((c) => c.key === 'GET /db')).toBe(false)
    fireEvent.keyDown(window, { key: 'k', ctrlKey: true })
    await waitFor(() => expect(app.calls.some((c) => c.key === 'GET /db')).toBe(true))
  })
})

describe('databases tools slice', () => {
  const hint = 'phpMyAdmin sign-in needs the API turned on and 127.0.0.1 allowed.'

  it('shows the single-sign-on hint above the list for admins', async () => {
    renderApp('/db', dbRoutes({}, { ssoHint: hint }))
    const text = await screen.findByText(hint)
    const table = await screen.findByRole('table')
    expect(text.compareDocumentPosition(table) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(document.body.textContent).not.toMatch(/Hestia/)
  })

  it('shows no hint when the options carry none', async () => {
    renderApp('/db', dbRoutes())
    await screen.findByRole('link', { name: 'admin_wordpress' })
    expect(screen.queryByText(hint)).toBeNull()
  })

  it('selects a range with shift-click', async () => {
    const routes = dbRoutes()
    routes['GET /db'] = () => ({
      body: {
        databases: ['a', 'b', 'c', 'd'].map((n) => database(`admin_${n}`)),
        usage: { databases: '4', databasesLimit: '5', suspended: '0' },
      },
    })
    renderApp('/db', routes)
    await screen.findByRole('link', { name: 'admin_a' })
    const box = (n: string) => screen.getByRole('checkbox', { name: new RegExp(`admin_${n}$`) })
    fireEvent.click(box('a'))
    fireEvent.click(box('d'), { shiftKey: true })
    for (const n of ['a', 'b', 'c', 'd']) expect(box(n).getAttribute('aria-checked')).toBe('true')
  })
})

describe('no mock data', () => {
  it('nothing under designs/db imports mocks/session or mocks/db', () => {
    const files = import.meta.glob('./*.{ts,tsx}', { query: '?raw', import: 'default', eager: true }) as Record<
      string,
      string
    >
    for (const [file, src] of Object.entries(files)) {
      if (file.includes('.test.')) continue
      expect(src, file).not.toMatch(/mocks\/session/)
      expect(src, file).not.toMatch(/mocks\/db/)
    }
  })
})
