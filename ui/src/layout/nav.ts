import type { Icon } from '@phosphor-icons/react'
import type { Features } from '../api/types.gen'
import type { Translate } from '../i18n/I18nProvider'
import { msgid } from '../i18n/msgid'
import {
  ArchiveIcon,
  ChartBarIcon,
  ClockIcon,
  DatabaseIcon,
  EnvelopeSimpleIcon,
  FolderOpenIcon,
  GlobeIcon,
  HardDrivesIcon,
  PackageIcon,
  ScrollIcon,
  SignpostIcon,
  TerminalWindowIcon,
  UsersIcon,
} from '@phosphor-icons/react'

export type NavLeaf = { label: string; path: string; feature?: keyof Features }

export type NavItem = {
  label: string
  path: string
  icon: Icon
  children?: NavLeaf[]
  /** Hidden when the viewed account's package/server lacks this feature. */
  feature?: keyof Features
}

export type NavGroup = {
  label: string
  adminOnly?: boolean
  items: NavItem[]
}

// Sections map one-to-one onto Hestia's panel so parity stays easy to check. Labels are
// msgids: translate them where they are shown.
export const navGroups: NavGroup[] = [
  {
    label: msgid('Hosting'),
    items: [
      { label: msgid('Web'), path: '/web', icon: GlobeIcon, feature: 'web' },
      { label: msgid('DNS'), path: '/dns', icon: SignpostIcon, feature: 'dns' },
      { label: msgid('Mail'), path: '/mail', icon: EnvelopeSimpleIcon, feature: 'mail' },
      { label: msgid('Databases'), path: '/db', icon: DatabaseIcon, feature: 'db' },
      { label: msgid('Cron jobs'), path: '/cron', icon: ClockIcon, feature: 'cron' },
      { label: msgid('Backups'), path: '/backup', icon: ArchiveIcon, feature: 'backup' },
    ],
  },
  {
    label: msgid('Administration'),
    adminOnly: true,
    items: [
      { label: msgid('Users'), path: '/users', icon: UsersIcon },
      { label: msgid('Packages'), path: '/packages', icon: PackageIcon },
      {
        label: msgid('Server'),
        path: '/server',
        icon: HardDrivesIcon,
        feature: 'server',
        children: [
          { label: msgid('Services'), path: '/server/services' },
          { label: msgid('Configuration'), path: '/server/config' },
          { label: msgid('Information'), path: '/server/info' },
          { label: msgid('IP addresses'), path: '/server/ip' },
          { label: msgid('Firewall'), path: '/server/firewall', feature: 'firewall' },
          { label: msgid('Updates'), path: '/server/updates' },
          { label: msgid('Graphs'), path: '/server/graphs' },
          { label: msgid('White label'), path: '/server/white-label' },
          { label: msgid('Preview features'), path: '/server/preview' },
        ],
      },
    ],
  },
  {
    label: msgid('Tools'),
    items: [
      { label: msgid('Logs'), path: '/logs', icon: ScrollIcon, feature: 'logs' },
      { label: msgid('Statistics'), path: '/stats', icon: ChartBarIcon },
      { label: msgid('Web terminal'), path: '/terminal', icon: TerminalWindowIcon, feature: 'terminal' },
      { label: msgid('File manager'), path: '/files', icon: FolderOpenIcon, feature: 'fileManager' },
    ],
  },
]

/** The sections the 1–6 keys open, in order (Hestia's help text: 1 = web … 6 = backups). */
export const numberedSections: NavItem[] = ['/web', '/dns', '/mail', '/db', '/cron', '/backup'].map(
  (path) => navGroups[0].items.find((i) => i.path === path) as NavItem,
)

export function findNavLabel(pathname: string): { section?: string; page?: string } {
  for (const group of navGroups) {
    for (const item of group.items) {
      const child = item.children?.find((c) => pathname.startsWith(c.path))
      if (child) return { section: item.label, page: child.label }
      if (pathname.startsWith(item.path)) return { page: item.label }
    }
  }
  return {}
}

// Pages that aren't in the sidebar, for breadcrumbs.
const extraPages: NavLeaf[] = [
  { label: msgid('Account'), path: '/account' },
  { label: msgid('Search'), path: '/search' },
  { label: msgid('Notifications'), path: '/notifications' },
  // The empty-package page (decision 30): named after its heading.
  { label: msgid('Nothing to manage yet'), path: '/error' },
]

// Readable names for URL segments under a section (ids and names are shown as-is).
const segmentLabels: Record<string, string> = {
  new: msgid('New'),
  edit: msgid('Edit'),
  ssl: msgid('SSL certificate'),
  generate: msgid('Generate'),
  apps: msgid('Quick install'),
  logs: msgid('Logs'),
  dns: msgid('DNS records'),
  dnssec: msgid('DNSSEC'),
  auth: msgid('Auth log'),
  'ssh-keys': msgid('SSH keys'),
  'access-keys': msgid('Access keys'),
  exclusions: msgid('Exclusions'),
  incremental: msgid('Incremental'),
  files: msgid('Files'),
  banlist: msgid('Banlist'),
  ipsets: msgid('IP lists'),
  preview: msgid('Preview features'),
}

// URL segments with no page of their own (e.g. /dns/<zone>/records/<id>); skipped in the trail.
const structuralSegments = new Set(['records', 'accounts'])

export type Crumb = { label: string; href: string }

/**
 * Breadcrumb trail for a path, e.g. Web › acme-bakery.com › Logs. Page and segment
 * names go through `t`; names taken from the URL (domains, ids) are shown as they are.
 */
export function breadcrumbsFor(pathname: string, t: Translate = (s) => s): Crumb[] {
  const items = navGroups.flatMap((g) => g.items.flatMap((i) => [i, ...(i.children ?? [])])).concat(extraPages)
  // Longest matching nav path wins, so /server/firewall beats /server.
  const base = items
    .filter((i) => pathname === i.path || pathname.startsWith(i.path + '/'))
    .sort((a, b) => b.path.length - a.path.length)[0]
  if (!base) return []
  const parent = navGroups.flatMap((g) => g.items).find((i) => i.children?.some((c) => c.path === base.path))
  const crumbs: Crumb[] = parent ? [{ label: t(parent.label), href: parent.path }] : []
  crumbs.push({ label: t(base.label), href: base.path })
  let href = base.path
  for (const segment of pathname.slice(base.path.length).split('/').filter(Boolean)) {
    href += '/' + segment
    if (structuralSegments.has(segment)) continue
    const decoded = decodeURIComponent(segment)
    const named = segmentLabels[decoded]
    crumbs.push({ label: named ? t(named) : decoded, href })
  }
  return crumbs
}

/** Whether a nav item is available to an account with these features. */
export const hasFeature = (item: NavItem | NavLeaf, features?: Features) => !item.feature || !!features?.[item.feature]
