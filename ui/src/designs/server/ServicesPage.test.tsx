import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { JOB_ID, jobDone, jobFailed, jobRunning, serverRoutes, service, sysInfo } from '../../test/serverFixtures'
import { renderApp, signedIn } from '../../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const features = {
  web: true,
  dns: true,
  db: true,
  mail: true,
  cron: true,
  backup: true,
  incremental: true,
  logs: true,
  accessKeys: true,
  server: true,
  firewall: true,
  firewallBans: true,
}
const withFeatures = (over: Record<string, boolean> = {}) => ({
  'GET /bootstrap': () => ({ body: signedIn({ features: { ...features, ...over } }) }),
})
const job = { status: 202, body: { jobId: JOB_ID } }
const pick = (name: string) => within(screen.getByText(name).closest('tr')!).getByRole('checkbox')
const main = () => within(screen.getByRole('main'))
const openMenu = async (name: string) => fireEvent.click(await screen.findByRole('button', { name: `Actions for ${name}` }))

describe('services page', () => {
  it('shows a dash, not a bare %, when a running service has no CPU figure', async () => {
    const services = [service('nginx', { cpu: '' })]
    renderApp(
      '/server/services',
      serverRoutes({
        'GET /server/services': () => ({ body: { info: sysInfo(), release: 'release', services, firewall: true } }),
      }),
    )
    const row = (await screen.findByText('nginx')).closest('tr')!
    const cells = within(row).getAllByRole('cell').map((c) => c.textContent)
    expect(cells).not.toContain('%')
    expect(cells).toContain('–')
  })

  it('summarises the server under the panel name, never Hestia', async () => {
    renderApp('/server/services', serverRoutes())
    expect(await screen.findByText('server1.acme-hosting.net')).toBeTruthy()
    expect(main().getByText('Hostisan')).toBeTruthy()
    expect(screen.getByText(/v1\.10\.5/)).toBeTruthy()
    expect(screen.queryByText(/hestia/i)).toBeNull()
    expect(document.body.textContent).not.toMatch(/hestia/i)
  })

  it('links Configure, Graphs, Updates, Firewall, Logs and offers Restart server', async () => {
    renderApp('/server/services', serverRoutes())
    await screen.findByText('apache2')
    const href = (name: string) => main().getByRole('link', { name }).getAttribute('href')
    expect(href('Configure')).toBe('/server/config')
    expect(href('Graphs')).toBe('/server/graphs')
    expect(href('Updates')).toBe('/server/updates')
    expect(href('Firewall')).toBe('/server/firewall')
    expect(href('Logs')).toBe('/logs?user=system')
    expect(screen.getByRole('button', { name: 'Restart server' })).toBeTruthy()
  })

  it('hides the Firewall link without the firewall feature', async () => {
    renderApp('/server/services', serverRoutes(withFeatures({ firewall: false })))
    await screen.findByText('apache2')
    expect(main().queryByRole('link', { name: 'Firewall' })).toBeNull()
  })

  it('shows CPU in percent, memory in MB and dims a stopped row', async () => {
    renderApp('/server/services', serverRoutes())
    const row = (await screen.findByText('mariadb')).closest('tr')!
    expect(within(row).getByText('0.5%')).toBeTruthy()
    expect(within(row).getByText('187 MB')).toBeTruthy()
    expect(within(row).getByText('Running')).toBeTruthy()
    const stopped = screen.getByText('vsftpd').closest('tr')!
    expect(stopped.className).toMatch(/opacity/)
    expect(within(stopped).getByText('Stopped')).toBeTruthy()
  })

  it('restarts a service from its row and shows the job failure', async () => {
    const routes = serverRoutes({
      'POST /server/services/nginx/restart': () => job,
      [`GET /server/jobs/${JOB_ID}`]: () => ({ body: jobFailed('Restart "%s" failed', ['Restart nginx'], 'service_failed', ['nginx']) }),
    })
    const { calls } = renderApp('/server/services', routes)
    await openMenu('nginx')
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Restart' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Restart' }))
    await waitFor(() => expect(calls.some((c) => c.key === 'POST /server/services/nginx/restart')).toBe(true))
    expect(await screen.findByText('Restart "nginx" failed')).toBeTruthy()
  })

  it('starts a stopped service with one confirm', async () => {
    const { calls } = renderApp('/server/services', serverRoutes({ 'POST /server/services/vsftpd/start': () => job }))
    await openMenu('vsftpd')
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Start' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Start' }))
    await waitFor(() => expect(calls.some((c) => c.key === 'POST /server/services/vsftpd/start')).toBe(true))
  })

  it('stops an ordinary service after a plain confirm', async () => {
    const { calls } = renderApp('/server/services', serverRoutes({ 'POST /server/services/cron/stop': () => job }))
    await openMenu('cron')
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Stop' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Stop' }))
    await waitFor(() => expect(calls.find((c) => c.key === 'POST /server/services/cron/stop')?.body).toEqual({}))
  })

  it('needs the name typed to stop ssh, and sends it as confirm', async () => {
    const { calls } = renderApp('/server/services', serverRoutes({ 'POST /server/services/ssh/stop': () => job }))
    await openMenu('ssh')
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Stop' }))
    const stop = await screen.findByRole('button', { name: 'Stop ssh' })
    expect((stop as HTMLButtonElement).disabled).toBe(true)
    fireEvent.change(screen.getByLabelText('Type ssh to confirm'), { target: { value: 'ssh' } })
    expect((stop as HTMLButtonElement).disabled).toBe(false)
    fireEvent.click(stop)
    await waitFor(() => expect(calls.find((c) => c.key === 'POST /server/services/ssh/stop')?.body).toEqual({ confirm: 'ssh' }))
  })

  it('runs a bulk start and keeps ssh and iptables out of bulk stop', async () => {
    const { calls } = renderApp('/server/services', serverRoutes({ 'POST /server/services/bulk': () => job }))
    await screen.findByText('apache2')
    fireEvent.click(pick('vsftpd'))
    fireEvent.click(pick('php8.3-fpm'))
    fireEvent.click(screen.getByRole('button', { name: 'Start' }))
    await waitFor(() =>
      expect(calls.find((c) => c.key === 'POST /server/services/bulk')?.body).toEqual({
        action: 'start',
        names: ['php8.3-fpm', 'vsftpd'],
      }),
    )
    cleanup()
    renderApp('/server/services', serverRoutes())
    await screen.findByText('apache2')
    fireEvent.click(pick('ssh'))
    expect((screen.getByRole('button', { name: 'Stop' }) as HTMLButtonElement).disabled).toBe(true)
    expect(screen.getByText('Stop "ssh" from its own row.')).toBeTruthy()
  })

  it('follows a running job step by step', async () => {
    renderApp(
      '/server/services',
      serverRoutes({
        'POST /server/services/bulk': () => job,
        [`GET /server/jobs/${JOB_ID}`]: () => ({ body: jobRunning('Restart nginx') }),
      }),
    )
    await screen.findByText('apache2')
    fireEvent.click(pick('nginx'))
    fireEvent.click(screen.getByRole('button', { name: 'Restart' }))
    expect((await screen.findAllByText('Restart nginx')).length).toBeGreaterThan(0)
  })

  it('asks for the hostname before restarting the server', async () => {
    const { calls } = renderApp('/server/services', serverRoutes({ 'POST /server/reboot': () => job }))
    await screen.findByText('apache2')
    fireEvent.click(screen.getByRole('button', { name: 'Restart server' }))
    const confirm = await screen.findByRole('button', { name: 'Restart server', hidden: false })
    const buttons = screen.getAllByRole('button', { name: 'Restart server' })
    const submit = buttons[buttons.length - 1] as HTMLButtonElement
    expect(confirm).toBeTruthy()
    expect(submit.disabled).toBe(true)
    fireEvent.change(screen.getByLabelText('Type server1.acme-hosting.net to confirm'), {
      target: { value: sysInfo().HOSTNAME },
    })
    expect(submit.disabled).toBe(false)
    fireEvent.click(submit)
    await waitFor(() =>
      expect(calls.find((c) => c.key === 'POST /server/reboot')?.body).toEqual({ hostname: 'server1.acme-hosting.net' }),
    )
  })

  it('opens the firewall page from the iptables row and has no editor for web-terminal', async () => {
    renderApp('/server/services', serverRoutes())
    await openMenu('iptables')
    expect(((await screen.findByRole('menuitem', { name: 'Edit configuration' })) as HTMLElement).getAttribute('href')).toBe(
      '/server/firewall',
    )
    fireEvent.keyDown(document.body, { key: 'Escape' })
  })

  it('shows the API error when the list fails', async () => {
    renderApp(
      '/server/services',
      serverRoutes({
        'GET /server/services': () => ({ status: 403, body: { error: { code: 'forbidden', message: 'Forbidden' } } }),
      }),
    )
    expect(await screen.findByText('Forbidden')).toBeTruthy()
  })

  it('words the confirm as a whole sentence per verb', async () => {
    renderApp('/server/services', serverRoutes())
    await openMenu('cron')
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Stop' }))
    expect(await screen.findByText('Are you sure you want to stop the cron service?')).toBeTruthy()
  })

  it('shows each failed bulk item as its name and filled message', async () => {
    renderApp(
      '/server/services',
      serverRoutes({
        'POST /server/services/bulk': () => job,
        [`GET /server/jobs/${JOB_ID}`]: () => ({
          body: jobDone({ status: 'ok', failed: [{ name: 'nginx', message: 'Start "%s" failed', args: ['nginx'] }] }),
        }),
      }),
    )
    await screen.findByText('apache2')
    fireEvent.click(pick('nginx'))
    fireEvent.click(screen.getByRole('button', { name: 'Restart' }))
    expect(await screen.findByText('nginx: Start "nginx" failed')).toBeTruthy()
  })
})
