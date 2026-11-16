import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { JobStatus } from '../../api/types.gen'
import { renderApp, type Route } from '../../test/renderApp'
import { webAppForm, webRoutes } from '../../test/webFixtures'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const path = '/web/example.com/apps/wordpress'
const form = (over: Parameters<typeof webAppForm>[1] = {}, id = 'wordpress'): Record<string, Route> => ({
  [`GET /web/example.com/apps/${id}`]: () => ({ body: webAppForm(id, over) }),
})
const job = (over: Partial<JobStatus> = {}): JobStatus => ({
  id: 'j1',
  title: 'Install WordPress',
  state: 'running',
  step: 'Downloading WordPress',
  steps: [],
  error: null,
  result: null,
  started: '2026-10-21T10:00:00Z',
  finished: '',
  ...over,
})
const started: Record<string, Route> = {
  'POST /web/example.com/apps/wordpress': () => ({ status: 202, body: { jobId: 'j1' } }),
}
const jobRoute = (...replies: JobStatus[]): Record<string, Route> => {
  let n = 0
  return { 'GET /web/example.com/apps/jobs/j1': () => ({ body: replies[Math.min(n++, replies.length - 1)] }) }
}
const install = () => fireEvent.click(screen.getByRole('button', { name: 'Install' }))
const field = (label: string) => screen.getByLabelText(label) as HTMLInputElement

describe('setup form', () => {
  it('renders the fields by type', async () => {
    renderApp(
      path,
      webRoutes(
        {},
        form({
          fields: [
            { key: 'site_name', label: 'Site Name', type: 'text', value: 'WordPress Blog', placeholder: '', options: [] },
            { key: 'password', label: 'Password', type: 'password', value: '', placeholder: '', options: [] },
            {
              key: 'language',
              label: 'Language',
              type: 'select',
              value: 'fr_FR',
              placeholder: '',
              options: [
                { value: 'en_US', label: 'English' },
                { value: 'fr_FR', label: 'French' },
              ],
            },
            { key: 'xmlrpc', label: 'Disable XML-RPC?', type: 'boolean', value: 'true', placeholder: '', options: [] },
          ],
        }),
      ),
    )
    expect(await screen.findByRole('heading', { name: 'Install WordPress' })).toBeTruthy()
    expect(field('Site Name').value).toBe('WordPress Blog')
    expect(screen.getAllByRole('button', { name: 'Generate password' }).length).toBeGreaterThan(0)
    expect(screen.getByRole('combobox', { name: 'Language' }).textContent).toContain('French')
    expect(screen.getByRole('checkbox', { name: 'Disable XML-RPC?' }).getAttribute('aria-checked')).toBe('true')
  })

  it('selects the highest PHP version', async () => {
    renderApp(path, webRoutes())
    await screen.findByRole('heading', { name: 'Install WordPress' })
    expect(screen.getByRole('combobox', { name: 'PHP version' }).textContent).toContain('PHP 8.3')
  })

  it('prefills the database block from the default, with Create database checked', async () => {
    renderApp(path, webRoutes())
    await screen.findByRole('heading', { name: 'Install WordPress' })
    expect(screen.getByRole('checkbox', { name: 'Create database' }).getAttribute('aria-checked')).toBe('true')
    expect(field('Database name').value).toBe('word_d3v9')
    expect(field('Database user').value).toBe('word_d3v9')
    expect(screen.getByRole('combobox', { name: 'Database host' }).textContent).toContain('localhost')
  })

  it('has no database block for an app without one', async () => {
    renderApp(path, webRoutes({}, form({ database: null })))
    await screen.findByRole('heading', { name: 'Install WordPress' })
    expect(screen.queryByLabelText('Database name')).toBeNull()
    expect(screen.queryByRole('checkbox', { name: 'Create database' })).toBeNull()
  })

  it('warns about the data loss when public_html is not clean', async () => {
    renderApp(path, webRoutes({}, form({ docrootClean: false })))
    expect(await screen.findByText('Data loss warning!')).toBeTruthy()
    cleanup()
    renderApp(path, webRoutes())
    await screen.findByRole('heading', { name: 'Install WordPress' })
    expect(screen.queryByText('Data loss warning!')).toBeNull()
  })

  it('shows App not found for a 404 with no message (unknown installer)', async () => {
    renderApp(
      '/web/example.com/apps/nope',
      webRoutes({}, { 'GET /web/example.com/apps/nope': () => ({ status: 404, body: { error: { code: 'not_found', message: '' } } }) }),
    )
    expect(await screen.findByText('App not found')).toBeTruthy()
    expect(screen.getByRole('heading', { name: 'Quick install app' })).toBeTruthy()
  })

  it('shows the real message for a 404 about a missing web folder', async () => {
    renderApp(
      path,
      webRoutes({}, {
        'GET /web/example.com/apps/wordpress': () => ({
          status: 404,
          body: {
            error: {
              code: 'not_found',
              message: 'Web domain path "%s" not found for domain "%s"',
              args: ['/home/admin/web/example.com/public_html', 'example.com'],
            },
          },
        }),
      }),
    )
    expect(
      await screen.findByText(/Web domain path "\/home\/admin\/web\/example\.com\/public_html" not found for domain "example\.com"|Web domain path .* not found for domain/),
    ).toBeTruthy()
    expect(screen.queryByText('App not found')).toBeNull()
  })

  it('shows an unknown domain\'s command error as it is', async () => {
    renderApp(
      path,
      webRoutes({}, {
        'GET /web/example.com/apps/wordpress': () => ({
          status: 404,
          body: { error: { code: 'not_found', message: "Error: web domain example.com doesn't exist" } },
        }),
      }),
    )
    expect(await screen.findByText("Error: web domain example.com doesn't exist")).toBeTruthy()
    expect(screen.queryByText('App not found')).toBeNull()
  })
})

describe('installing', () => {
  const fill = async () => {
    await screen.findByRole('heading', { name: 'Install WordPress' })
    fireEvent.change(field('Email'), { target: { value: 'me@example.com' } })
    fireEvent.change(field('Password'), { target: { value: 'Sup3r-secret-pw' } })
  }

  it('posts the WebAppInstallRequest and shows progress from the job', async () => {
    const app = renderApp(path, webRoutes({}, { ...started, ...jobRoute(job()) }))
    await fill()
    install()
    await waitFor(() => expect(app.calls.some((c) => c.key === 'POST /web/example.com/apps/wordpress')).toBe(true))
    expect(app.calls.find((c) => c.key === 'POST /web/example.com/apps/wordpress')?.body).toEqual({
      values: {
        site_name: 'WordPress Blog',
        username: 'wpadmin',
        email: 'me@example.com',
        password: 'Sup3r-secret-pw',
        language: 'en_US',
      },
      phpVersion: '8.3',
      database: { create: true, host: 'localhost', name: 'word_d3v9', user: 'word_d3v9', password: '' },
    })
    const status = await screen.findByRole('status', { name: 'Installation progress' })
    expect(status.textContent).toContain('Downloading WordPress')
    expect((screen.getByRole('button', { name: 'Install' }) as HTMLButtonElement).disabled).toBe(true)
    expect(app.calls.some((c) => c.key === 'GET /web/example.com/apps/jobs/j1')).toBe(true)
  })

  it('sends booleans as "true" or "" and omits the database for an app without one', async () => {
    const app = renderApp(
      path,
      webRoutes({}, {
        ...started,
        ...jobRoute(job()),
        ...form({
          database: null,
          fields: [{ key: 'xmlrpc', label: 'Disable XML-RPC?', type: 'boolean', value: 'true', placeholder: '', options: [] }],
        }),
      }),
    )
    await screen.findByRole('heading', { name: 'Install WordPress' })
    fireEvent.click(screen.getByRole('checkbox', { name: 'Disable XML-RPC?' }))
    install()
    await waitFor(() => expect(app.calls.some((c) => c.key === 'POST /web/example.com/apps/wordpress')).toBe(true))
    expect(app.calls.find((c) => c.key === 'POST /web/example.com/apps/wordpress')?.body).toEqual({
      values: { xmlrpc: '' },
      phpVersion: '8.3',
    })
  })

  it('toasts the app name and returns to the apps list when the job is done', async () => {
    const app = renderApp(path, webRoutes({}, { ...started, ...jobRoute(job(), job({ state: 'done', finished: 'x' })) }))
    await fill()
    install()
    expect(await screen.findByText('WordPress installed successfully.', {}, { timeout: 6000 })).toBeTruthy()
    await waitFor(() => expect(app.location()).toBe('/web/example.com/apps'))
  })

  it("keeps the form and shows the job's error when it fails", async () => {
    const app = renderApp(
      path,
      webRoutes({}, {
        ...started,
        ...jobRoute(job({ state: 'failed', error: { code: 'failed', message: 'Error: composer exited with %s', args: ['1'] } })),
      }),
    )
    await fill()
    install()
    expect(await screen.findByText('Error: composer exited with 1')).toBeTruthy()
    expect(app.location()).toBe(path)
    expect((screen.getByRole('button', { name: 'Install' }) as HTMLButtonElement).disabled).toBe(false)
    expect(field('Email').value).toBe('me@example.com')
  })

  it('shows the busy message on 409', async () => {
    renderApp(
      path,
      webRoutes({}, {
        'POST /web/example.com/apps/wordpress': () => ({
          status: 409,
          body: { error: { code: 'busy', message: 'An installation is already running for this domain.' } },
        }),
      }),
    )
    await fill()
    install()
    expect(await screen.findByText('An installation is already running for this domain.')).toBeTruthy()
    expect((screen.getByRole('button', { name: 'Install' }) as HTMLButtonElement).disabled).toBe(false)
  })

  it('says the status is unknown when the job is lost', async () => {
    renderApp(
      path,
      webRoutes({}, {
        ...started,
        'GET /web/example.com/apps/jobs/j1': () => ({
          status: 404,
          body: { error: { code: 'not_found', message: 'Error: job not found' } },
        }),
      }),
    )
    await fill()
    install()
    expect(await screen.findByText('Installation status is unknown')).toBeTruthy()
    expect((screen.getByRole('button', { name: 'Install' }) as HTMLButtonElement).disabled).toBe(false)
  })
})

describe('sources', () => {
  it('the web list, add, logs and installer screens import nothing from mocks', async () => {
    const files = import.meta.glob(
      [
        './WebAppsPage.tsx',
        './SetupWebAppPage.tsx',
        './WebLogsPage.tsx',
        './AddWebDomainPage.tsx',
        './WebDomainsPage.tsx',
        './WebAside.tsx',
        '../../layout/commands/web.ts',
      ],
      { query: '?raw', import: 'default', eager: true },
    ) as Record<string, string>
    expect(Object.keys(files)).toHaveLength(7)
    for (const [file, src] of Object.entries(files)) expect(src, file).not.toMatch(/\/mocks\//)
  })
})
