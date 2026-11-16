import { backupRoutes, cronRoutes, keysRoutes, usersRoutes } from './accountFixtures'
import { mailRoutes } from './mailFixtures'
import { dbRoutes, dnsRoutes, type Route } from './renderApp'
import { serverRoutes } from './serverFixtures'
import { webRoutes } from './webFixtures'

/** Every feature on, so each list and its sidebar section render. */
export const allFeatures = {
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

/**
 * The list pages with a primary add button, over their fixtures. `add` is where the
 * button leads, or the request it sends when it acts in place.
 */
export type ListPage = {
  name: string
  path: string
  routes: () => Record<string, Route>
  add: { to: string } | { call: string }
}

export const listPages: ListPage[] = [
  { name: 'WebDomainsPage', path: '/web', routes: () => webRoutes(), add: { to: '/web/new' } },
  { name: 'DnsZonesPage', path: '/dns', routes: () => dnsRoutes(), add: { to: '/dns/new' } },
  {
    name: 'DnsRecordsPage',
    path: '/dns/example.com',
    routes: () => dnsRoutes(),
    add: { to: '/dns/example.com/records/new' },
  },
  {
    name: 'MailDomainsPage',
    path: '/mail',
    routes: () => mailRoutes({ features: allFeatures }),
    add: { to: '/mail/new' },
  },
  {
    name: 'MailAccountsPage',
    path: '/mail/example.com',
    routes: () => mailRoutes({ features: allFeatures }),
    add: { to: '/mail/example.com/accounts/new' },
  },
  { name: 'DatabasesPage', path: '/db', routes: () => dbRoutes(), add: { to: '/db/new' } },
  { name: 'CronJobsPage', path: '/cron', routes: () => cronRoutes(), add: { to: '/cron/new' } },
  { name: 'BackupsPage', path: '/backup', routes: () => backupRoutes(), add: { call: 'POST /backup' } },
  {
    name: 'IncrementalPage',
    path: '/backup/incremental',
    routes: () => backupRoutes(),
    add: { call: 'POST /backup/incremental' },
  },
  { name: 'UsersPage', path: '/users', routes: () => usersRoutes(), add: { to: '/users/new' } },
  { name: 'PackagesPage', path: '/packages', routes: () => usersRoutes(), add: { to: '/packages/new' } },
  {
    name: 'FirewallRulesPage',
    path: '/server/firewall',
    routes: () => serverRoutes(),
    add: { to: '/server/firewall/new' },
  },
  {
    name: 'FirewallIpsetsPage',
    path: '/server/firewall/ipsets',
    routes: () => serverRoutes(),
    add: { to: '/server/firewall/ipsets/new' },
  },
  {
    name: 'FirewallBanlistPage',
    path: '/server/firewall/banlist',
    routes: () => serverRoutes(),
    add: { to: '/server/firewall/banlist/new' },
  },
  { name: 'IpListPage', path: '/server/ip', routes: () => serverRoutes(), add: { to: '/server/ip/new' } },
  {
    name: 'AccessKeysPage',
    path: '/account/access-keys',
    routes: () => keysRoutes(),
    add: { to: '/account/access-keys/new' },
  },
  { name: 'SshKeysPage', path: '/account/ssh-keys', routes: () => keysRoutes(), add: { to: '/account/ssh-keys/new' } },
]
