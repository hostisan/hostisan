// Hestia's list keys on every list page: ↓ then Enter opens the first row, s asks
// before suspending it, and Delete asks before deleting it. No key acts without the
// page's own confirm.
import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { listPages, type ListPage } from '../test/listPages'
import { renderApp } from '../test/renderApp'
import { serverRoutes } from '../test/serverFixtures'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

type RowCase = ListPage & {
  /** How many ↓ presses reach the row under test (1: the first row). */
  downs?: number
  /** Where Enter on the row goes, when the row opens a page. */
  enter?: string
  /** The confirm s opens on the row, when the list can suspend. */
  suspend?: string
  /** The confirm Delete opens on the row, when the list can delete. */
  del?: string
}

/** Lists without an add button that still have row keys. */
const otherLists: ListPage[] = [
  { name: 'ServicesPage', path: '/server/services', routes: () => serverRoutes(), add: { to: '' } },
]

/**
 * Each list's expectations for one row: the first, or the row `downs` presses of ↓
 * reach when the first can't show every key (suspended, the signed-in admin, a
 * built-in package or a protected rule).
 */
const rowCases: Record<string, Pick<RowCase, 'downs' | 'enter' | 'suspend' | 'del'>> = {
  WebDomainsPage: { enter: '/web/example.com', suspend: 'Suspend web domain', del: 'Delete example.com' },
  DnsZonesPage: { enter: '/dns/example.com', suspend: 'Suspend DNS domain', del: 'Delete example.com' },
  DnsRecordsPage: { enter: '/dns/example.com/records/1', suspend: 'Suspend DNS record', del: 'Delete DNS record' },
  MailDomainsPage: { enter: '/mail/example.com', suspend: 'Suspend mail domain', del: 'Delete example.com' },
  MailAccountsPage: {
    enter: '/mail/example.com/accounts/info',
    suspend: 'Suspend mail account',
    del: 'Delete info@example.com',
  },
  DatabasesPage: {
    downs: 2,
    enter: '/db/admin_wordpress',
    suspend: 'Suspend database',
    del: 'Delete admin_wordpress',
  },
  CronJobsPage: { downs: 2, enter: '/cron/1', suspend: 'Suspend', del: 'Delete' },
  BackupsPage: { enter: '/backup/admin.2026-10-19_03-00-02.tar', del: 'Delete' },
  IncrementalPage: { enter: '/backup/incremental/e41f0a29', del: 'Delete' },
  UsersPage: { downs: 2, enter: '/users/bob', suspend: 'Suspend user', del: 'Delete user' },
  PackagesPage: { downs: 2, enter: '/packages/starter', del: 'Delete package' },
  FirewallRulesPage: { downs: 2, enter: '/server/firewall/9', suspend: 'Suspend rule?', del: 'Delete rule?' },
  FirewallIpsetsPage: { del: 'Delete IP list?' },
  FirewallBanlistPage: { del: 'Unban IP address?' },
  IpListPage: { enter: '/server/ip/10.0.0.5', del: 'Delete IP address?' },
  AccessKeysPage: { enter: '/account/access-keys/Kp4Lm7Nq2Rs5Tv8Wx1Yz', del: 'Delete access key' },
  SshKeysPage: { del: 'Delete SSH key' },
  // Hestia's services keys: s restarts, Delete stops a running service.
  ServicesPage: { enter: '/server/services/apache2', suspend: 'Restart apache2?', del: 'Stop apache2?' },
}

const cases: RowCase[] = [...listPages, ...otherLists]
  .filter((p) => p.name in rowCases)
  .map((p) => ({ ...p, ...rowCases[p.name] }))

async function rowFocused(page: RowCase) {
  const app = renderApp(page.path, page.routes())
  await waitFor(() => expect(document.querySelectorAll('tbody tr').length).toBeGreaterThan(0), { timeout: 10000 })
  for (let i = 0; i < (page.downs ?? 1); i++) {
    expect(fireEvent.keyDown(document.body, { key: 'ArrowDown' })).toBe(false)
    await waitFor(() => expect(document.querySelectorAll('tbody tr')[i]?.hasAttribute('data-row-focused')).toBe(true))
  }
  return app
}

/** Requests that change something: none may go out before the confirm is answered. */
const writes = (app: ReturnType<typeof renderApp>) => app.calls.filter((c) => !c.key.startsWith('GET '))

describe('row keys on every list', () => {
  it('covers every list with an add button', () => {
    expect(Object.keys(rowCases).sort()).toEqual([...listPages, ...otherLists].map((p) => p.name).sort())
  })

  it.each(cases.filter((c) => c.enter))(
    'down then enter opens the row: $name',
    async (page) => {
      const app = await rowFocused(page)
      fireEvent.keyDown(document.body, { key: 'Enter' })
      await waitFor(() => expect(app.location()).toBe(page.enter))
    },
    20000,
  )

  it.each(cases.filter((c) => c.suspend))(
    's suspends with confirm: $name',
    async (page) => {
      const app = await rowFocused(page)
      fireEvent.keyDown(document.body, { key: 's' })
      expect(await screen.findByRole('dialog', { name: page.suspend }, { timeout: 10000 })).toBeTruthy()
      expect(writes(app)).toEqual([])
    },
    20000,
  )

  it.each(cases.filter((c) => c.del))(
    'delete asks first: $name',
    async (page) => {
      const app = await rowFocused(page)
      fireEvent.keyDown(document.body, { key: 'Delete' })
      expect(await screen.findByRole('dialog', { name: page.del }, { timeout: 10000 })).toBeTruthy()
      expect(writes(app)).toEqual([])
    },
    20000,
  )
})
