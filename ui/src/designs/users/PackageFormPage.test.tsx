import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { packageOptions, pkg, usersRoutes } from '../../test/accountFixtures'
import { renderApp, type Route } from '../../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const full = {
  web: true, dns: true, db: true, mail: true, cron: true, backup: true, incremental: true, logs: true,
  accessKeys: true, server: true, firewall: true, firewallBans: true,
}
const saved = (name: string): Route => () => ({ body: { status: 'ok', name } })
const unlimitedFor = (label: string) =>
  within(screen.getByLabelText(label).parentElement as HTMLElement).getByRole('checkbox', { name: 'Unlimited' })

describe('add package', () => {
  it('starts with Hestia defaults and posts them', async () => {
    const { calls, location } = renderApp('/packages/new', usersRoutes({ features: full }, { 'POST /packages': saved('gold') }))
    const name = await screen.findByLabelText('Package name')
    fireEvent.change(name, { target: { value: 'gold' } })
    expect((screen.getByLabelText('Quota') as HTMLInputElement).value).toBe('1000')
    expect((screen.getByLabelText('Web aliases') as HTMLInputElement).value).toBe('5')
    expect((screen.getByLabelText('Rate limit') as HTMLInputElement).value).toBe('200')
    fireEvent.click(screen.getByRole('button', { name: 'Add package' }))
    await waitFor(() => expect(calls.some((c) => c.key === 'POST /packages')).toBe(true))
    expect(calls.find((c) => c.key === 'POST /packages')?.body).toEqual({
      name: 'gold',
      webTemplate: 'default',
      backendTemplate: 'default',
      proxyTemplate: 'default',
      dnsTemplate: 'default',
      webDomains: '1',
      webAliases: '5',
      dnsDomains: '1',
      dnsRecords: 'unlimited',
      mailDomains: '1',
      mailAccounts: '5',
      rateLimit: '200',
      databases: '1',
      cronJobs: '1',
      diskQuota: '1000',
      bandwidth: '1000',
      cpuQuota: 'unlimited',
      cpuQuotaPeriod: 'unlimited',
      memoryLimit: 'unlimited',
      swapLimit: 'unlimited',
      backups: '1',
      backupsIncremental: 'no',
      shell: 'nologin',
      nameservers: ['ns1.example.tld', 'ns2.example.tld'],
    })
    await waitFor(() => expect(location()).toBe('/packages'))
    // Hestia's message, its {name} linking to the package (owner ruling 16)
    const link = await screen.findByRole('link', { name: 'gold' })
    expect(link.getAttribute('href')).toBe('/packages/gold')
    expect(link.parentElement?.textContent).toContain('Package gold has been created successfully.')
  })

  it('sends unlimited for a limit switched to Unlimited, and the typed value otherwise', async () => {
    const { calls } = renderApp('/packages/new', usersRoutes({ features: full }, { 'POST /packages': saved('gold') }))
    fireEvent.change(await screen.findByLabelText('Package name'), { target: { value: 'gold' } })
    fireEvent.click(unlimitedFor('Quota'))
    fireEvent.change(screen.getByLabelText('Databases'), { target: { value: '9' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add package' }))
    await waitFor(() => expect(calls.some((c) => c.key === 'POST /packages')).toBe(true))
    const body = calls.find((c) => c.key === 'POST /packages')?.body as Record<string, string>
    expect(body.diskQuota).toBe('unlimited')
    expect(body.databases).toBe('9')
    expect(body.bandwidth).toBe('1000')
  })

  it('offers no unlimited toggle for backups and rate limit', async () => {
    renderApp('/packages/new', usersRoutes({ features: full }))
    await screen.findByLabelText('Package name')
    for (const l of ['Backups', 'Rate limit']) {
      expect(within(screen.getByLabelText(l).parentElement as HTMLElement).queryByRole('checkbox')).toBeNull()
    }
  })

  it('shows the backend, proxy and resource sections only as the server has them', async () => {
    renderApp('/packages/new', usersRoutes({ features: full }))
    await screen.findByLabelText('Package name')
    expect(screen.getByText(/Backend template/)).toBeTruthy()
    expect(screen.getByText(/Proxy template/)).toBeTruthy()
    expect(screen.queryByLabelText('CPU quota')).toBeNull()
    cleanup()
    renderApp(
      '/packages/new',
      usersRoutes(
        { features: full },
        { 'GET /packages/options': () => ({ body: packageOptions({ webBackend: '', proxySystem: '', dnsSystem: '', resourcesLimit: true }) }) },
      ),
    )
    await screen.findByLabelText('Package name')
    expect(screen.queryByText(/Backend template/)).toBeNull()
    expect(screen.queryByText(/Proxy template/)).toBeNull()
    expect(screen.queryByText('Name servers')).toBeNull()
    expect(screen.getByLabelText('CPU quota')).toBeTruthy()
    expect(screen.getByLabelText('Swap limit')).toBeTruthy()
  })

  it('shows the server error and stays on the form', async () => {
    const { location } = renderApp(
      '/packages/new',
      usersRoutes(
        { features: full },
        { 'POST /packages': () => ({ status: 409, body: { error: { code: 'exists', message: 'Error: package gold exists' } } }) },
      ),
    )
    fireEvent.change(await screen.findByLabelText('Package name'), { target: { value: 'gold' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add package' }))
    expect(await screen.findByText('Error: package gold exists')).toBeTruthy()
    expect(location()).toBe('/packages/new')
  })
})

describe('edit package', () => {
  it('loads the stored package and saves under the same name', async () => {
    const { calls, location } = renderApp('/packages/starter-plus', usersRoutes({ features: full }, { 'PUT /packages/starter-plus': saved('starter-plus') }))
    const name = (await screen.findByLabelText('Package name')) as HTMLInputElement
    expect(name.value).toBe('starter-plus')
    expect((screen.getByLabelText('Web domains') as HTMLInputElement).value).toBe('3')
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(calls.some((c) => c.key === 'PUT /packages/starter-plus')).toBe(true))
    expect(await screen.findByText('Changes have been saved.')).toBeTruthy()
    expect(location()).toBe('/packages/starter-plus')
  })

  it('shows unlimited stored values as switched on', async () => {
    renderApp('/packages/default', usersRoutes({ features: full }))
    await screen.findByLabelText('Package name')
    expect(unlimitedFor('Web domains').getAttribute('aria-checked')).toBe('true')
    expect((screen.getByLabelText('Web domains') as HTMLInputElement).disabled).toBe(true)
  })

  it('goes to the new address after a rename', async () => {
    const { calls, location } = renderApp(
      '/packages/starter',
      usersRoutes({ features: full }, { 'PUT /packages/starter': saved('basic'), 'GET /packages/basic': () => ({ body: pkg('basic') }) }),
    )
    fireEvent.change(await screen.findByLabelText('Package name'), { target: { value: 'basic' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(location()).toBe('/packages/basic'))
    expect((calls.find((c) => c.key === 'PUT /packages/starter')?.body as { name: string }).name).toBe('basic')
    expect(((await screen.findByLabelText('Package name')) as HTMLInputElement).value).toBe('basic')
  })

  it('copies the package from the header', async () => {
    const { calls, location } = renderApp('/packages/starter', usersRoutes({ features: full }, { 'POST /packages/starter/copy': () => ({ body: { status: 'ok' } }) }))
    await screen.findByLabelText('Package name')
    fireEvent.click(screen.getByRole('button', { name: 'Copy package' }))
    await waitFor(() => expect(calls.some((c) => c.key === 'POST /packages/starter/copy')).toBe(true))
    await waitFor(() => expect(location()).toBe('/packages'))
  })

  it('says so when the package does not exist or is system', async () => {
    renderApp('/packages/missing', usersRoutes({ features: full }))
    expect(await screen.findByText('There\'s no package named “missing”')).toBeTruthy()
    cleanup()
    renderApp('/packages/system', usersRoutes({ features: full }, { 'GET /packages/system': () => ({ status: 404, body: { error: { code: 'not_found', message: '' } } }) }))
    expect(await screen.findByText('There\'s no package named “system”')).toBeTruthy()
  })

  it('sends no name servers when the server has no DNS system', async () => {
    const { calls } = renderApp(
      '/packages/new',
      usersRoutes(
        { features: full },
        { 'GET /packages/options': () => ({ body: packageOptions({ dnsSystem: '' }) }), 'POST /packages': saved('gold') },
      ),
    )
    fireEvent.change(await screen.findByLabelText('Package name'), { target: { value: 'gold' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add package' }))
    await waitFor(() => expect(calls.some((c) => c.key === 'POST /packages')).toBe(true))
    expect((calls.find((c) => c.key === 'POST /packages')?.body as { nameservers: string[] }).nameservers).toEqual([])
  })
})
