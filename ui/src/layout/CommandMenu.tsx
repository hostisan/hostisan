import { CommandPalette } from '@cloudflare/kumo'
import {
  DatabaseIcon,
  EnvelopeSimpleIcon,
  KeyboardIcon,
  MagnifyingGlassIcon,
  PlusIcon,
  SignpostIcon,
} from '@phosphor-icons/react'
import { useMemo, useState } from 'react'
import { useNavigate } from 'react-router'
import { useDnsZones } from '../api/dns'
import { useT, type Translate } from '../i18n/I18nProvider'
import { useDatabases } from '../api/db'
import { useMailDomains } from '../api/mail'
import type { Database } from '../api/hestia.gen'
import type { Features } from '../api/types.gen'
import { useSessionUser } from '../session/SessionProvider'
import { isMac } from '../shortcuts/keys'
import { useShortcutsDialog } from '../shortcuts/ShortcutsProvider'
import { useAccountCommands } from './commands/accounts'
import { useServerCommands } from './commands/server'
import type { CommandContribution, CommandGroup, CommandItem } from './commands/types'
import { useWebCommands } from './commands/web'
import { hasFeature, navGroups } from './nav'

const helpId = 'shortcuts:help'

export const commandShortcut = isMac ? '⌘K' : 'Ctrl K'

type Sections = { web: CommandContribution; accounts: CommandContribution; server: CommandContribution }

function buildGroups(
  isAdmin: boolean,
  features: Features | undefined,
  dnsZones: string[],
  databases: Database[],
  mailDomains: string[],
  { web, accounts, server }: Sections,
  t: Translate,
): CommandGroup[] {
  const dnsEnabled = features?.dns ?? false
  const dbEnabled = features?.db ?? false
  const mailEnabled = features?.mail ?? false
  const navPages: CommandItem[] = navGroups
    .filter((g) => !g.adminOnly || isAdmin)
    .flatMap((g) =>
      g.items
        .filter((item) => hasFeature(item, features))
        .flatMap((item) => [
          { id: `page:${item.path}`, title: t(item.label), icon: item.icon, href: item.path },
          ...(item.children ?? [])
            .filter((c) => hasFeature(c, features))
            .map((c) => ({
              id: `page:${c.path}`,
              title: `${t(item.label)}: ${t(c.label)}`,
              icon: item.icon,
              href: c.path,
            })),
        ]),
    )
  const pages = [...navPages, ...accounts.pages, ...web.pages, ...server.pages]

  const actions: CommandItem[] = [
    ...web.actions,
    ...(mailEnabled ? [{ id: 'new:mail', title: t('Add mail domain'), icon: PlusIcon, href: '/mail/new' }] : []),
    ...(dnsEnabled ? [{ id: 'new:dns', title: t('Add DNS zone'), icon: PlusIcon, href: '/dns/new' }] : []),
    ...(dbEnabled ? [{ id: 'new:db', title: t('Add database'), icon: PlusIcon, href: '/db/new' }] : []),
    ...accounts.actions,
    ...server.actions,
  ]

  const resources: CommandGroup[] = [
    ...web.groups,
    {
      id: 'mail',
      label: t('Mail domains'),
      items: mailDomains.map((name) => ({
        id: `mail:${name}`,
        title: name,
        icon: EnvelopeSimpleIcon,
        href: `/mail/${name}`,
      })),
    },
    {
      id: 'dns',
      label: t('DNS zones'),
      items: dnsZones.map((name) => ({
        id: `dns:${name}`,
        title: name,
        icon: SignpostIcon,
        href: `/dns/${name}`,
      })),
    },
    {
      id: 'db',
      label: t('Databases'),
      items: databases.map((d) => ({
        id: `db:${d.DATABASE}`,
        title: d.DATABASE,
        hint: d.TYPE === 'pgsql' ? 'PostgreSQL' : 'MySQL',
        icon: DatabaseIcon,
        href: `/db/${d.DATABASE}`,
      })),
    },
    ...accounts.groups,
    ...server.groups,
  ]

  return [
    { id: 'actions', label: t('Actions'), items: actions },
    { id: 'pages', label: t('Go to'), items: pages },
    ...resources,
  ]
}

/**
 * Global ⌘K / Ctrl K palette (the key lives in ShortcutsProvider): jump to any page or resource, or start a common action.
 */
export function CommandMenu({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const navigate = useNavigate()
  const t = useT()
  const [search, setSearch] = useState('')
  const user = useSessionUser()
  const isAdmin = user?.role === 'admin' && user.look === ''
  const dnsEnabled = user?.features.dns ?? false
  const dnsZones = useDnsZones({ enabled: open && dnsEnabled })
  const zoneNames = useMemo(() => (dnsZones.data?.zones ?? []).map((z) => z.NAME), [dnsZones.data])
  const dbEnabled = user?.features.db ?? false
  const dbList = useDatabases({ enabled: open && dbEnabled })
  const mailEnabled = user?.features.mail ?? false
  const databases = useMemo(() => dbList.data?.databases ?? [], [dbList.data])
  const mailList = useMailDomains({ enabled: open && mailEnabled })
  const mailDomains = useMemo(() => (mailList.data?.domains ?? []).map((d) => d.NAME), [mailList.data])
  const web = useWebCommands(open)
  const accounts = useAccountCommands(open)
  const server = useServerCommands(open)
  const shortcuts = useShortcutsDialog()
  const groups = useMemo(() => {
    const all = buildGroups(isAdmin, user?.features, zoneNames, databases, mailDomains, { web, accounts, server }, t)
    // The keyboard help opens in place rather than navigating (O7).
    const help: CommandItem = {
      id: helpId,
      title: t('Shortcuts'),
      icon: KeyboardIcon,
      href: '',
      keywords: 'keyboard keys',
    }
    return all.map((g) => (g.id === 'actions' ? { ...g, items: [...g.items, help] } : g))
  }, [isAdmin, user?.features, zoneNames, databases, mailDomains, web, accounts, server, t])

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase()
    if (!q) return groups.filter((g) => g.id === 'actions' || g.id === 'pages')
    const everything: CommandGroup = {
      id: 'search',
      label: t('Search'),
      items: [
        {
          id: 'search:all',
          title: t('Search all objects for {%s}').replace('{%s}', search.trim()),
          icon: MagnifyingGlassIcon,
          href: `/search?q=${encodeURIComponent(search.trim())}`,
        },
      ],
    }
    const matches = groups
      .map((g) => ({
        ...g,
        items: g.items.filter((i) => `${i.title} ${i.hint ?? ''} ${i.keywords ?? ''}`.toLowerCase().includes(q)),
      }))
      .filter((g) => g.items.length > 0)
    return [...matches, everything]
  }, [groups, search, t])

  const go = (item: CommandItem, newTab = false) => {
    if (item.id === helpId) shortcuts.setOpen(true)
    else if (newTab) window.open(item.href, '_blank')
    else navigate(item.href)
    onOpenChange(false)
    setSearch('')
  }

  return (
    <CommandPalette.Root
      open={open}
      onOpenChange={(next) => {
        onOpenChange(next)
        if (!next) setSearch('')
      }}
      items={filtered}
      value={search}
      onValueChange={setSearch}
      itemToStringValue={(group: CommandGroup) => group.label}
      getSelectableItems={(gs: CommandGroup[]) => gs.flatMap((g) => g.items)}
      onSelect={(item: CommandItem, { newTab }: { newTab: boolean }) => go(item, newTab)}
    >
      <CommandPalette.Input placeholder={t('Search pages, domains, users or actions…')} />
      <CommandPalette.List>
        <CommandPalette.Results>
          {(group: CommandGroup) => (
            <CommandPalette.Group key={group.id} items={group.items}>
              <CommandPalette.GroupLabel>{group.label}</CommandPalette.GroupLabel>
              <CommandPalette.Items>
                {(item: CommandItem) => (
                  <CommandPalette.Item key={item.id} value={item} onClick={() => go(item)}>
                    <span className="flex w-full items-center gap-3">
                      <item.icon size={18} className="shrink-0 text-kumo-subtle" />
                      <span className="truncate">{item.title}</span>
                      {item.hint && <span className="ml-auto shrink-0 text-kumo-subtle">{item.hint}</span>}
                    </span>
                  </CommandPalette.Item>
                )}
              </CommandPalette.Items>
            </CommandPalette.Group>
          )}
        </CommandPalette.Results>
        <CommandPalette.Empty>{t('Nothing found')}</CommandPalette.Empty>
      </CommandPalette.List>
      <CommandPalette.Footer>
        <span className="flex items-center gap-2">
          <kbd className="rounded border border-kumo-hairline bg-kumo-base px-1.5 py-0.5 text-xs">↑↓</kbd>
          <span>{t('Navigate')}</span>
        </span>
        <span className="flex items-center gap-2">
          <kbd className="rounded border border-kumo-hairline bg-kumo-base px-1.5 py-0.5 text-xs">↵</kbd>
          <span>{t('Open')}</span>
        </span>
        <span className="flex items-center gap-2">
          <kbd className="rounded border border-kumo-hairline bg-kumo-base px-1.5 py-0.5 text-xs">esc</kbd>
          <span>{t('Close')}</span>
        </span>
      </CommandPalette.Footer>
    </CommandPalette.Root>
  )
}
