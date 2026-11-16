import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { cronJob, cronRoutes } from '../../test/accountFixtures'
import { renderApp, signedIn } from '../../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const fullFeatures = signedIn({}).user.features
const bob = { name: 'bob', role: 'user', features: fullFeatures }

const openMenu = (job: string) => fireEvent.click(screen.getByRole('button', { name: `Actions for job ${job}` }))
const rowOf = (text: string) => screen.getByText(text, { exact: false }).closest('tr') as HTMLElement

describe('cron list', () => {
  it('opens in the saved sort order (owner ruling 16)', async () => {
    const jobs2 = [cronJob('1', { CMD: 'aa-old', DATE: '2026-01-01' }), cronJob('2', { CMD: 'zz-new', DATE: '2026-09-01' })]
    const order = () => screen.getAllByRole('button', { name: /^Actions for job / }).map((b) => b.getAttribute('aria-label'))
    renderApp('/cron', cronRoutes({ sortOrder: 'name' }, {
      'GET /cron': () => ({ body: { jobs: jobs2, reports: true, usage: { jobs: '2', limit: 'unlimited', suspended: '0' } } }),
    }))
    await screen.findByText('aa-old')
    expect(order()).toEqual(['Actions for job 1', 'Actions for job 2'])
    cleanup()
    renderApp('/cron', cronRoutes({ sortOrder: 'date' }, {
      'GET /cron': () => ({ body: { jobs: jobs2, reports: true, usage: { jobs: '2', limit: 'unlimited', suspended: '0' } } }),
    }))
    await screen.findByText('aa-old')
    expect(order()).toEqual(['Actions for job 2', 'Actions for job 1'])
  })

  it('shows the columns and each command verbatim', async () => {
    const hard = cronJob('3', { CMD: `find /home/bob -name '*.tmp' -exec rm {} \;`, MIN: '0', HOUR: '*/6' })
    const routes = cronRoutes({}, {
      'GET /cron': () => ({ body: { jobs: [cronJob('1'), hard], reports: true, usage: { jobs: '2', limit: 'unlimited', suspended: '0' } } }),
    })
    renderApp('/cron', routes)
    const cmd = await screen.findByText(`find /home/bob -name '*.tmp' -exec rm {} \;`)
    expect(cmd.getAttribute('title') ?? cmd.closest('[title]')?.getAttribute('title')).toBe(`find /home/bob -name '*.tmp' -exec rm {} \;`)
    for (const h of ['Command', 'Schedule', 'Expression', 'Created', 'Status']) {
      expect(screen.getByRole('columnheader', { name: new RegExp(h) })).toBeTruthy()
    }
    const row = rowOf(`find /home/bob`)
    expect(within(row).getByText('0 */6 * * *')).toBeTruthy()
    expect(within(row).getByText('Every 6 hours at minute 00')).toBeTruthy()
    expect(screen.getByText('*/15 * * * *')).toBeTruthy()
  })

  it('styles the suspended row: badge, command not a link', async () => {
    renderApp('/cron', cronRoutes())
    await screen.findByRole('link', { name: /cron\.php/ })
    expect(screen.queryByRole('link', { name: /artisan/ })).toBeNull()
    const row = rowOf('artisan schedule:run')
    expect(within(row).getByText('Suspended')).toBeTruthy()
    expect(within(rowOf('cron.php')).getByText('Active')).toBeTruthy()
  })

  it('hides Edit on suspended rows and offers Unsuspend there', async () => {
    renderApp('/cron', cronRoutes())
    await screen.findByRole('link', { name: /cron\.php/ })
    openMenu('2')
    expect(await screen.findByRole('menuitem', { name: 'Unsuspend' })).toBeTruthy()
    expect(screen.queryByRole('menuitem', { name: 'Edit' })).toBeNull()
    fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('menuitem')).toBeNull())
    openMenu('1')
    expect(await screen.findByRole('menuitem', { name: 'Edit' })).toBeTruthy()
    expect(screen.getByRole('menuitem', { name: 'Suspend' })).toBeTruthy()
  })

  it('shows suspend and unsuspend to admins only', async () => {
    renderApp('/cron', cronRoutes(bob))
    await screen.findByRole('link', { name: /cron\.php/ })
    openMenu('1')
    expect(await screen.findByRole('menuitem', { name: 'Edit' })).toBeTruthy()
    expect(screen.getByRole('menuitem', { name: 'Delete' })).toBeTruthy()
    expect(screen.queryByRole('menuitem', { name: 'Suspend' })).toBeNull()
    expect(screen.queryByRole('menuitem', { name: 'Unsuspend' })).toBeNull()
    fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Escape' })
    // bulk bar
    fireEvent.click(within(rowOf('cron.php')).getByRole('checkbox'))
    expect(await screen.findByRole('button', { name: 'Delete' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Suspend' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Unsuspend' })).toBeNull()
  })

  it('offers bulk suspend to admins and posts the names, then restarts nothing client-side', async () => {
    const routes = cronRoutes({}, { 'POST /cron/bulk': () => ({ body: { status: 'ok', failed: [] } }) })
    const { calls } = renderApp('/cron', routes)
    await screen.findByRole('link', { name: /cron\.php/ })
    fireEvent.click(within(rowOf('cron.php')).getByRole('checkbox'))
    fireEvent.click(await screen.findByRole('button', { name: 'Suspend' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Suspend' }))
    await waitFor(() => expect(calls.find((c) => c.key === 'POST /cron/bulk')).toBeTruthy())
    expect(calls.find((c) => c.key === 'POST /cron/bulk')?.body).toEqual({ action: 'suspend', names: ['1'] })
  })

  it('names the failed items of a bulk delete', async () => {
    const routes = cronRoutes({}, {
      'POST /cron/bulk': () => ({ body: { status: 'ok', failed: [{ name: '1', message: "Error: cron job 1 doesn't exist" }] } }),
    })
    renderApp('/cron', routes)
    await screen.findByRole('link', { name: /cron\.php/ })
    fireEvent.click(within(rowOf('cron.php')).getByRole('checkbox'))
    fireEvent.click(await screen.findByRole('button', { name: 'Delete' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Delete' }))
    expect(await screen.findByText("Error: cron job 1 doesn't exist")).toBeTruthy()
  })

  it('deletes one job after confirming', async () => {
    const routes = cronRoutes({}, { 'DELETE /cron/1': () => ({ body: { status: 'ok' } }) })
    const { calls } = renderApp('/cron', routes)
    await screen.findByRole('link', { name: /cron\.php/ })
    openMenu('1')
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete' }))
    expect(await screen.findByText('Are you sure you want to delete this cron job?')).toBeTruthy()
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(calls.some((c) => c.key === 'DELETE /cron/1')).toBe(true))
  })

  it('reflects the reports flag in the toggle and the bulk button, and posts the change', async () => {
    const routes = cronRoutes({}, { 'POST /cron/reports': () => ({ body: { status: 'ok' } }) })
    const { calls } = renderApp('/cron', routes)
    await screen.findByText(/cron\.php/)
    const sw = screen.getAllByRole('switch', { name: 'Cron notifications' })[0]
    expect(sw.getAttribute('aria-checked')).toBe('true')
    fireEvent.click(sw)
    await waitFor(() => expect(calls.find((c) => c.key === 'POST /cron/reports')?.body).toEqual({ enabled: false }))
    // Hestia's msgid (owner ruling 16)
    expect(await screen.findByText('Cron job email reporting has been successfully disabled.')).toBeTruthy()
  })

  it('shows the toggle off when reports is false', async () => {
    const routes = cronRoutes({}, {
      'GET /cron': () => ({ body: { jobs: [cronJob('1')], reports: false, usage: { jobs: '1', limit: '5', suspended: '0' } } }),
    })
    renderApp('/cron', routes)
    await screen.findByText(/cron\.php/)
    const sw = screen.getAllByRole('switch', { name: 'Cron notifications' })[0]
    expect(sw.getAttribute('aria-checked')).toBe('false')
    expect(screen.getAllByText('1 / 5').length).toBeGreaterThan(0)
  })

  it('reports a failed reports change', async () => {
    const routes = cronRoutes({}, {
      'POST /cron/reports': () => ({ status: 409, body: { error: { code: 'suspended', message: 'Error: user admin is suspended' } } }),
    })
    renderApp('/cron', routes)
    await screen.findByText(/cron\.php/)
    fireEvent.click(screen.getAllByRole('switch', { name: 'Cron notifications' })[0])
    expect(await screen.findByText('Error: user admin is suspended')).toBeTruthy()
  })

  it('shows the server time in the aside', async () => {
    renderApp('/cron', cronRoutes())
    expect(await screen.findByText(/21 Oct 2026,? 12:00/)).toBeTruthy()
    expect(screen.getByText('UTC')).toBeTruthy()
  })

  it('hides write actions when read-only', async () => {
    renderApp('/cron', cronRoutes({ readOnly: true }))
    await screen.findByText(/cron\.php/)
    expect(screen.queryByRole('link', { name: 'Add cron job' })).toBeNull()
    expect(screen.queryByRole('checkbox')).toBeNull()
    expect(screen.queryByRole('button', { name: 'Actions for job 1' })).toBeNull()
    expect(screen.getAllByRole('switch', { name: 'Cron notifications' })[0].hasAttribute('disabled') || screen.getAllByRole('switch', { name: 'Cron notifications' })[0].getAttribute('data-disabled') !== null).toBe(true)
  })

  it('shows an error when the list fails', async () => {
    renderApp('/cron', cronRoutes({}, { 'GET /cron': () => ({ status: 500, body: { error: { code: 'server_error', message: 'Error: boom' } } }) }))
    expect(await screen.findByText('Error: boom')).toBeTruthy()
  })

  it('shows the empty state', async () => {
    renderApp('/cron', cronRoutes({}, { 'GET /cron': () => ({ body: { jobs: [], reports: false, usage: { jobs: '0', limit: 'unlimited', suspended: '' } } }) }))
    expect(await screen.findByText('No cron jobs yet')).toBeTruthy()
  })

  it("mails to the viewed account's contact, not the signed-in admin's", async () => {
    renderApp(
      '/cron',
      cronRoutes(
        { look: 'bob' },
        { 'GET /cron': () => ({ body: { jobs: [], reports: true, usage: { jobs: '0', limit: 'unlimited', suspended: '' }, contact: 'bob@acme-bakery.com' } }) },
      ),
    )
    expect(await screen.findByText('Email the output of every job to bob@acme-bakery.com.')).toBeTruthy()
  })
})
