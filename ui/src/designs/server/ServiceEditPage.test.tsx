import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderApp } from '../../test/renderApp'
import { JOB_ID, editorView, jobDone, jobFailed, serverRoutes } from '../../test/serverFixtures'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const job = { status: 202, body: { jobId: JOB_ID } }
const save = () => screen.findByRole('button', { name: 'Save' }) as Promise<HTMLButtonElement>
const file = (id: string, path: string, over = {}) => ({ id, path, content: `# ${path}\n`, hash: id.padEnd(64, '0'), ...over })

describe('service editor', () => {
  it('shows each file labelled with its path and the nginx basic options first', async () => {
    renderApp('/server/services/nginx', serverRoutes())
    expect(await screen.findByLabelText('worker_processes')).toBeTruthy()
    expect((screen.getByLabelText('worker_connections') as HTMLInputElement).value).toBe('1024')
    fireEvent.click(screen.getByRole('tab', { name: 'Advanced options' }))
    expect(await screen.findByLabelText('/etc/nginx/nginx.conf')).toBeTruthy()
  })

  it('sends only the changed directives from basic mode', async () => {
    const { calls } = renderApp('/server/services/nginx', serverRoutes({ 'PUT /server/editors/nginx': () => job }))
    const input = await screen.findByLabelText('worker_connections')
    fireEvent.change(input, { target: { value: '2048' } })
    fireEvent.click(await save())
    await waitFor(() =>
      expect(calls.find((c) => c.key === 'PUT /server/editors/nginx')?.body).toEqual({
        files: [],
        directives: { worker_connections: '2048' },
        restart: true,
      }),
    )
  })

  it('sends the file text and hash from advanced mode, honouring the restart checkbox', async () => {
    const { calls } = renderApp('/server/services/nginx', serverRoutes({ 'PUT /server/editors/nginx': () => job }))
    fireEvent.click(await screen.findByRole('tab', { name: 'Advanced options' }))
    fireEvent.change(await screen.findByLabelText('/etc/nginx/nginx.conf'), { target: { value: 'events {}\n' } })
    const restart = screen.getByRole('checkbox', { name: 'Restart the service after saving' })
    fireEvent.click(restart)
    // nginx -t still tests the file, so there is nothing to warn about.
    expect(screen.queryByText(/Nothing will test the new configuration/)).toBeNull()
    fireEvent.click(await save())
    await waitFor(() =>
      expect(calls.find((c) => c.key === 'PUT /server/editors/nginx')?.body).toEqual({
        files: [{ id: 'nginx', content: 'events {}\n', hash: 'a'.repeat(64) }],
        restart: false,
      }),
    )
  })

  it('shows a read error on its file and disables Save', async () => {
    const view = editorView({
      service: 'apache2',
      family: 'plain',
      directives: undefined,
      files: [file('apache2', '/etc/apache2/apache2.conf'), { id: 'x', path: '/etc/apache2/bad.conf', content: '', hash: '', error: 'Error: cannot read' }],
    })
    renderApp('/server/services/apache2', serverRoutes({ 'GET /server/editors/apache2': () => ({ body: view }) }))
    expect(await screen.findByText('Error: cannot read')).toBeTruthy()
    expect(screen.getByText('/etc/apache2/bad.conf')).toBeTruthy()
    expect((await save()).disabled).toBe(true)
  })

  it('blocks saving PHP and explains why', async () => {
    const view = editorView({
      service: 'php8.2-fpm',
      title: 'PHP 8.2',
      family: 'php',
      saveBlocked: "Saving would overwrite every php.ini on this server. Edit this file over SSH.",
      files: [file('php', '/etc/php/8.2/fpm/php.ini')],
    })
    renderApp('/server/services/php8.2-fpm', serverRoutes({ 'GET /server/editors/php8.2-fpm': () => ({ body: view }) }))
    expect(await screen.findByText(/Saving would overwrite every php.ini/)).toBeTruthy()
    expect((await save()).disabled).toBe(true)
  })

  it('shows the cron crontab read-only without calling PUT', async () => {
    const view = editorView({
      service: 'cron',
      family: 'cron',
      readOnly: true,
      restart: false,
      directives: undefined,
      files: [file('cron', '/etc/crontab')],
    })
    const { calls } = renderApp('/server/services/cron', serverRoutes({ 'GET /server/editors/cron': () => ({ body: view }) }))
    expect(await screen.findByText(/Crontab can only be edited via SSH/)).toBeTruthy()
    expect((screen.getByLabelText('/etc/crontab') as HTMLTextAreaElement).readOnly).toBe(true)
    expect((await save()).disabled).toBe(true)
    expect(screen.queryByRole('checkbox', { name: 'Restart the service after saving' })).toBeNull()
    expect(calls.some((c) => c.key.startsWith('PUT'))).toBe(false)
  })

  it('shows the restored and service-state banner when the restart fails', async () => {
    const failed = jobFailed(
      'The new configuration failed to start, so the previous file was restored.',
      ['Save /etc/nginx/nginx.conf', 'Restart nginx'],
      'restart_failed',
      ['running'],
    )
    renderApp(
      '/server/services/nginx',
      serverRoutes({ 'PUT /server/editors/nginx': () => job, [`GET /server/jobs/${JOB_ID}`]: () => ({ body: failed }) }),
    )
    fireEvent.change(await screen.findByLabelText('worker_connections'), { target: { value: '9' } })
    fireEvent.click(await save())
    expect(await screen.findByText(/previous file was restored/)).toBeTruthy()
    expect(await screen.findByText('Service state: Running')).toBeTruthy()
  })

  it('reports a finished save', async () => {
    renderApp(
      '/server/services/nginx',
      serverRoutes({
        'PUT /server/editors/nginx': () => job,
        [`GET /server/jobs/${JOB_ID}`]: () => ({ body: jobDone({ changed: ['nginx'], unchanged: [], serviceState: 'running' }) }),
      }),
    )
    fireEvent.change(await screen.findByLabelText('worker_connections'), { target: { value: '9' } })
    fireEvent.click(await save())
    expect(await screen.findByText('Changes have been saved.')).toBeTruthy()
  })

  it('warns before and after saving untested when the service has no checker', async () => {
    const view = editorView({
      service: 'vsftpd',
      title: 'VSFTPD',
      family: 'plain',
      directives: undefined,
      syntaxCheck: false,
      files: [file('vsftpd', '/etc/vsftpd.conf')],
    })
    renderApp(
      '/server/services/vsftpd',
      serverRoutes({
        'GET /server/editors/vsftpd': () => ({ body: view }),
        'PUT /server/editors/vsftpd': () => job,
        [`GET /server/jobs/${JOB_ID}`]: () => ({
          body: jobDone({ changed: ['vsftpd'], unchanged: [], serviceState: '', untested: true }),
        }),
      }),
    )
    fireEvent.change(await screen.findByLabelText('/etc/vsftpd.conf'), { target: { value: 'listen=YES\n' } })
    expect(screen.queryByText(/Nothing will test the new configuration/)).toBeNull()
    fireEvent.click(screen.getByRole('checkbox', { name: 'Restart the service after saving' }))
    expect(screen.getByText(/Nothing will test the new configuration/)).toBeTruthy()
    fireEvent.click(await save())
    expect(
      await screen.findByText(
        'Changes have been saved, but nothing tested the new configuration. Restart the service to check that it starts.',
      ),
    ).toBeTruthy()
    expect(screen.queryByText('Changes have been saved.')).toBeNull()
  })

  it("shows the checker's output when the new file has errors", async () => {
    const failed = jobFailed(
      'The new configuration has errors, so the previous file was restored.\n%s',
      ['Save /etc/nginx/nginx.conf'],
      'config_invalid',
      ['nginx: [emerg] unknown directive "bogus" in /etc/nginx/nginx.conf:3'],
    )
    renderApp(
      '/server/services/nginx',
      serverRoutes({ 'PUT /server/editors/nginx': () => job, [`GET /server/jobs/${JOB_ID}`]: () => ({ body: failed }) }),
    )
    fireEvent.change(await screen.findByLabelText('worker_connections'), { target: { value: '9' } })
    fireEvent.click(await save())
    const alert = await screen.findByText(/The new configuration has errors, so the previous file was restored\./)
    expect(alert.textContent).toContain('unknown directive "bogus" in /etc/nginx/nginx.conf:3')
    expect(screen.queryByText('Changes have been saved.')).toBeNull()
  })

  it('shows a rejected save such as a changed file', async () => {
    renderApp(
      '/server/services/nginx',
      serverRoutes({
        'PUT /server/editors/nginx': () => ({ status: 409, body: { error: { code: 'changed', message: 'The file changed while you were editing it.' } } }),
      }),
    )
    fireEvent.change(await screen.findByLabelText('worker_connections'), { target: { value: '9' } })
    fireEvent.click(await save())
    expect(await screen.findByText('The file changed while you were editing it.')).toBeTruthy()
  })

  it('opens the panel cron editor back to server configuration', async () => {
    const view = editorView({
      service: 'hestiaweb',
      family: 'panel-cron',
      restart: false,
      directives: undefined,
      files: [file('hestiaweb', '/var/spool/cron/crontabs/hestiaweb')],
    })
    renderApp('/server/services/hestiaweb', serverRoutes({ 'GET /server/editors/hestiaweb': () => ({ body: view }) }))
    expect(await screen.findByLabelText('/var/spool/cron/crontabs/hestiaweb')).toBeTruthy()
    expect(screen.getByRole('link', { name: /Configure server/ }).getAttribute('href')).toBe('/server/config')
  })

  it('shows not found for an unknown service', async () => {
    renderApp(
      '/server/services/nope',
      serverRoutes({ 'GET /server/editors/nope': () => ({ status: 404, body: { error: { code: 'not_found', message: 'Not found' } } }) }),
    )
    expect(await screen.findByText('There is no service called nope')).toBeTruthy()
  })

  it('links Configure PHP to the default php.ini editor from the web server editors', async () => {
    renderApp('/server/services/nginx', serverRoutes())
    expect((await screen.findByRole('link', { name: 'Configure PHP' })).getAttribute('href')).toBe('/server/services/php')
  })

  it('does not drop edits when switching between basic and advanced', async () => {
    renderApp('/server/services/nginx', serverRoutes())
    fireEvent.change(await screen.findByLabelText('worker_connections'), { target: { value: '9' } })
    expect(screen.getByText(/Save your changes before switching/)).toBeTruthy()
    fireEvent.click(screen.getByRole('tab', { name: 'Advanced options' }))
    expect((screen.getByLabelText('worker_connections') as HTMLInputElement).value).toBe('9')
  })

  it('offers to reload the file after a changed conflict', async () => {
    let reads = 0
    const { calls } = renderApp(
      '/server/services/nginx',
      serverRoutes({
        'GET /server/editors/nginx': () => ({ body: editorView({ directives: { worker_connections: String(1000 + reads++) } }) }),
        'PUT /server/editors/nginx': () => ({ status: 409, body: { error: { code: 'changed', message: 'The file changed while you were editing it.' } } }),
      }),
    )
    fireEvent.change(await screen.findByLabelText('worker_connections'), { target: { value: '9' } })
    fireEvent.click(await save())
    fireEvent.click(await screen.findByRole('button', { name: 'Reload file' }))
    await waitFor(() => expect(calls.filter((c) => c.key === 'GET /server/editors/nginx').length).toBeGreaterThan(1))
    await waitFor(() => expect(screen.queryByText('The file changed while you were editing it.')).toBeNull())
  })
})
