import { ListIcon } from '@phosphor-icons/react'
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { dnsRoutes, renderApp, signedIn } from '../test/renderApp'
import type { CommandContribution } from './commands/types'

const serverStub = vi.hoisted(() => ({ value: null as CommandContribution | null }))

vi.mock('./commands/server', async (importOriginal) => {
  const real = await importOriginal<typeof import('./commands/server')>()
  return { useServerCommands: (open: boolean) => serverStub.value ?? real.useServerCommands(open) }
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  serverStub.value = null
})

const every = {
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

let app: ReturnType<typeof renderApp>

async function openPalette(user: Record<string, unknown>, extra = {}) {
  app = renderApp('/dns', { ...dnsRoutes(user), ...extra })
  await screen.findByText('Hosting')
  fireEvent.keyDown(window, { key: 'k', ctrlKey: true })
  const input = await screen.findByPlaceholderText(/Search pages, domains/)
  await screen.findByText('Add DNS zone')
  return input
}

describe('command palette', () => {
  it('offers Add web domain only with the web feature', async () => {
    await openPalette({ features: every })
    expect(screen.getByText('Add web domain')).toBeTruthy()
    cleanup()
    await openPalette({ features: { ...every, web: false } })
    expect(screen.queryByText('Add web domain')).toBeNull()
  })

  it('follows the cron and access-key features', async () => {
    await openPalette({ features: every })
    expect(screen.getByText('Add cron job')).toBeTruthy()
    expect(screen.getByText('Access keys')).toBeTruthy()
    cleanup()
    await openPalette({ features: { ...every, cron: false, accessKeys: false } })
    expect(screen.queryByText('Add cron job')).toBeNull()
    expect(screen.queryByText('Access keys')).toBeNull()
  })

  it('offers Add user only to an admin who is not looking', async () => {
    await openPalette({ features: every })
    expect(screen.getByText('Add user')).toBeTruthy()
    cleanup()
    await openPalette({ features: every, look: 'bob', landing: '/dns' })
    expect(screen.queryByText('Add user')).toBeNull()
    cleanup()
    await openPalette({ features: every, role: 'user', landing: '/dns' })
    expect(screen.queryByText('Add user')).toBeNull()
  })

  it('lists Server children by the server feature', async () => {
    const input = await openPalette({ features: every })
    fireEvent.change(input, { target: { value: 'ser' } })
    expect(await screen.findByText('Server: Services')).toBeTruthy()
    cleanup()
    const again = await openPalette({ features: { ...every, server: false } })
    fireEvent.change(again, { target: { value: 'ser' } })
    await waitFor(() => expect(screen.queryByText('Server: Services')).toBeNull())
  })

  it('still offers the object search when nothing else matches', async () => {
    const input = await openPalette({ features: every })
    fireEvent.change(input, { target: { value: 'zzz-nothing' } })
    expect(await screen.findByText('Search all objects for zzz-nothing')).toBeTruthy()
    expect(screen.queryByText('Add DNS zone')).toBeNull()
  })

  it('places a track contribution group after Databases', async () => {
    serverStub.value = {
      actions: [],
      pages: [],
      groups: [
        {
          id: 'stub',
          label: 'Stub group',
          items: [{ id: 'stub:1', title: 'extra-thing', icon: ListIcon, href: '/server/stub' }],
        },
      ],
    }
    const input = await openPalette(
      { features: every },
      {
        'GET /db': () => ({ body: { databases: [{ DATABASE: 'extra_db', TYPE: 'mysql' }] } }),
        'GET /bootstrap': () => ({ body: signedIn({ features: every }) }),
      },
    )
    fireEvent.change(input, { target: { value: 'extra' } })
    const dialog = screen.getByRole('dialog')
    await waitFor(() => expect(within(dialog).getByText('Stub group')).toBeTruthy())
    await waitFor(() => expect(within(dialog).getByText('extra_db')).toBeTruthy())
    const db = within(dialog).getByText('Databases')
    const stub = within(dialog).getByText('Stub group')
    expect(db.compareDocumentPosition(stub) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('offers to search all objects for what you typed', async () => {
    const input = await openPalette({ features: every })
    expect(screen.queryByText(/^Search all objects for/)).toBeNull()
    fireEvent.change(input, { target: { value: 'example' } })
    fireEvent.click(await screen.findByText('Search all objects for example'))
    await waitFor(() => expect(app.location()).toBe('/search'))
    expect(app.calls.some((c) => c.key === 'GET /search?q=example')).toBe(true)
  })

  it('opens the DNS page for dns + Enter, with the object search listed last', async () => {
    const input = await openPalette({ features: every })
    fireEvent.change(input, { target: { value: 'dns' } })
    await screen.findByText('Search all objects for dns')
    const titles = screen.getAllByRole('option').map((o) => o.textContent)
    expect(titles[titles.length - 1]).toBe('Search all objects for dns')
    fireEvent.keyDown(input, { key: 'Enter' })
    await waitFor(() => expect(app.location()).toMatch(/^\/dns/))
    expect(app.location()).not.toBe('/search')
    expect(app.calls.some((c) => c.key.startsWith('GET /search'))).toBe(false)
  })
})
