import { Button, DropdownMenu, LinkButton, Select, Table, Text, Toolbar, Tooltip } from '@cloudflare/kumo'
import {
  ArrowSquareOutIcon,
  ArrowsClockwiseIcon,
  ChartLineIcon,
  CheckCircleIcon,
  DownloadSimpleIcon,
  FileTextIcon,
  GlobeIcon,
  LockSimpleIcon,
  MagicWandIcon,
  PauseCircleIcon,
  PencilSimpleIcon,
  PlayCircleIcon,
  PlusIcon,
  TrashIcon,
  XCircleIcon,
} from '@phosphor-icons/react'
import { useMemo, useState } from 'react'
import { Link as RouterLink, useNavigate } from 'react-router'
import { useT, type Translate } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import type { WebDomain } from '../../api/hestia.gen'
import type { WebOptions } from '../../api/types.gen'
import { useWebDomains, useWebMutation, useWebOptions, webSiteDownloadUrl } from '../../api/web'
import { ListConfirmDialog, ResourceDeleteDialog, useConfirm, useResourceDelete } from '../../components/useListConfirm'
import type { PendingConfirm } from '../../components/useListConfirm'
import { PageHeader } from '../../components/PageHeader'
import { StatusBadge } from '../../components/StatusBadge'
import {
  ColumnsMenu,
  SortableHead,
  TableCard,
  TableToolbar,
  rowLink,
  rowMenuTrigger,
  useColumnVisibility,
  useDataTable,
  type ToggleableColumn,
} from '../../components/table'
import { formatBackend, formatMB } from '../../lib/format'
import { useSessionUser } from '../../session/SessionProvider'
import { DnsError, DnsLoading } from '../dns/shared'
import { WebAside } from './WebAside'
import { shortcutTarget } from '../../shortcuts/targets'
import { useRowShortcuts } from '../../shortcuts/useRowShortcuts'

/** The domain's proxy template caches (caching or caching-*), so the NGINX cache can be purged. */
const isCaching = (d: WebDomain) => d.PROXY === 'caching' || d.PROXY.startsWith('caching-')

/** Enabled/disabled mark; grey instead of green/red while the domain is suspended. */
function StateIcon({ on, label, suspended }: { on: boolean; label: string; suspended: boolean }) {
  const t = useT()
  const text = `${label}: ${on ? t('Enabled') : t('Disabled')}`
  const tone = suspended ? 'text-kumo-inactive' : on ? 'text-kumo-success' : 'text-kumo-danger'
  const Icon = on ? CheckCircleIcon : XCircleIcon
  return (
    <Tooltip
      content={text}
      render={
        <span aria-label={text} role="img" className="inline-flex items-center">
          <Icon weight="fill" size={16} className={tone} />
        </span>
      }
    />
  )
}

/** Statistics are off for '' and 'no'. */
const statsOn = (d: WebDomain) => d.STATS !== '' && d.STATS !== 'no'

/** Aliases to show under the name: all but the default www.<domain>. */
const aliasHint = (d: WebDomain) =>
  d.ALIAS.split(',')
    .filter((a) => a !== '' && a !== `www.${d.NAME}`)
    .join(', ')

/** The suspend or unsuspend question for a domain, shared by its menu and the s key. */
function suspendConfirm(d: WebDomain, t: Translate, m: ReturnType<typeof useWebMutation>): PendingConfirm {
  const name = d.NAME
  const suspended = d.SUSPENDED === 'yes'
  return {
    title: suspended ? t('Unsuspend web domain') : t('Suspend web domain'),
    description: suspended
      ? `${t('Are you sure you want to unsuspend domain %s?', name)} ${t('The website will be served again.')}`
      : `${t('Are you sure you want to suspend domain %s?', name)} ${t('Visitors will see the suspended page until it is unsuspended.')}`,
    confirmLabel: suspended ? t('Unsuspend') : t('Suspend'),
    destructive: !suspended,
    done: suspended ? t('%s unsuspended', name) : t('%s suspended', name),
    run: () => (suspended ? m.unsuspendDomain(name) : m.suspendDomain(name)),
  }
}

/** Where a domain's statistics live, when it has them. */
const statsUrl = (d: WebDomain) => `${d.SSL === 'yes' ? 'https' : 'http'}://${d.NAME}/vstats/`

function RowActions({
  d,
  options,
  readOnly,
  isAdmin,
  ask,
  onDelete,
  m,
}: {
  d: WebDomain
  options: WebOptions | undefined
  readOnly: boolean
  /** Suspend and unsuspend are admin-only on the server. */
  isAdmin: boolean
  ask: (p: PendingConfirm) => void
  onDelete: (name: string) => void
  m: ReturnType<typeof useWebMutation>
}) {
  const navigate = useNavigate()
  const t = useT()
  const name = d.NAME
  const suspended = d.SUSPENDED === 'yes'
  return (
    <DropdownMenu>
      <DropdownMenu.Trigger render={rowMenuTrigger(t('Actions for %s', name))} />
      <DropdownMenu.Content align="end">
        {statsOn(d) && (
          <DropdownMenu.LinkItem icon={ChartLineIcon} href={statsUrl(d)} target="_blank" rel="noopener noreferrer">
            {t('Statistics')}
          </DropdownMenu.LinkItem>
        )}
        <DropdownMenu.LinkItem icon={ArrowSquareOutIcon} href={`http://${name}/`} target="_blank" rel="noopener noreferrer">
          {t('Visit')}
        </DropdownMenu.LinkItem>
        {!readOnly && (
          <>
            {!suspended && (
              <>
                <DropdownMenu.Item icon={PencilSimpleIcon} onClick={() => navigate(`/web/${name}`)}>
                  {t('Edit domain')}
                </DropdownMenu.Item>
                <DropdownMenu.LinkItem icon={DownloadSimpleIcon} href={webSiteDownloadUrl(name)}>
                  {t('Download site')}
                </DropdownMenu.LinkItem>
              </>
            )}
            <DropdownMenu.Item icon={FileTextIcon} onClick={() => navigate(`/web/${name}/logs`)}>
              {t('View logs')}
            </DropdownMenu.Item>
            {d.SSL === 'yes' && (
              <DropdownMenu.Item icon={LockSimpleIcon} onClick={() => navigate(`/web/${name}/ssl`)}>
                {t('SSL certificate')}
              </DropdownMenu.Item>
            )}
            {options?.appInstaller !== false && (
              <DropdownMenu.Item icon={MagicWandIcon} onClick={() => navigate(`/web/${name}/apps`)}>
                {t('Quick install app')}
              </DropdownMenu.Item>
            )}
            {options?.purge && (isCaching(d) || options.nginx) && (
              <DropdownMenu.Item
                icon={ArrowsClockwiseIcon}
                onClick={() =>
                  ask({
                    title: t('Purge NGINX cache'),
                    description: t('Purge the NGINX cache for %s?', name),
                    confirmLabel: t('Purge cache'),
                    done: t('NGINX cache has been purged successfully.'),
                    run: () => m.purgeCache(name),
                  })
                }
              >
                {t('Purge NGINX cache')}
              </DropdownMenu.Item>
            )}
            <DropdownMenu.Separator />
            {isAdmin && (
              <DropdownMenu.Item
                icon={suspended ? PlayCircleIcon : PauseCircleIcon}
                onClick={() => ask(suspendConfirm(d, t, m))}
              >
                {suspended ? t('Unsuspend') : t('Suspend')}
              </DropdownMenu.Item>
            )}
            <DropdownMenu.Item icon={TrashIcon} variant="danger" onClick={() => onDelete(name)}>
              {t('Delete')}
            </DropdownMenu.Item>
          </>
        )}
      </DropdownMenu.Content>
    </DropdownMenu>
  )
}

const statusFilters = { all: msgid('Show all'), active: msgid('Active'), suspended: msgid('Suspended') }

const optionalColumns: ToggleableColumn[] = [
  { key: 'ip', label: msgid('IP address') },
  { key: 'backend', label: msgid('Backend') },
  { key: 'bandwidth', label: msgid('Bandwidth') },
  { key: 'created', label: msgid('Created') },
]

/** Hestia sorts IPs by their dotless digits; compare them as numbers. */
const ipKey = (ip: string) => Number(ip.replace(/\D/g, '')) || 0

export function WebDomainsPage() {
  const t = useT()
  const user = useSessionUser()
  const readOnly = user?.readOnly ?? false
  // The signed-in role, even while looking at another user
  const isAdmin = user?.role === 'admin'
  const list = useWebDomains()
  const options = useWebOptions().data
  const m = useWebMutation()
  const navigate = useNavigate()
  const [status, setStatus] = useState<keyof typeof statusFilters>('all')
  const confirm = useConfirm()
  const del = useResourceDelete()
  const columns = useColumnVisibility(
    'web-domains',
    optionalColumns.map((c) => ({ ...c, label: t(c.label) })),
  )
  const show = columns.isVisible
  const displayIps = list.data?.displayIps
  const displayIp = (d: WebDomain) => displayIps?.[d.IP] ?? d.IP

  const all = useMemo<WebDomain[]>(() => list.data?.domains ?? [], [list.data])
  const byStatus = useMemo(
    () => (status === 'all' ? all : all.filter((d) => (d.SUSPENDED === 'yes') === (status === 'suspended'))),
    [all, status],
  )
  const table = useDataTable({
    rows: byStatus,
    getId: (d) => d.NAME,
    search: (d, q) => d.NAME.includes(q) || d.ALIAS.toLowerCase().includes(q) || displayIp(d).includes(q),
    sorts: {
      name: (d) => d.NAME,
      ip: (d) => ipKey(d.IP),
      disk: (d) => Number(d.U_DISK),
      bandwidth: (d) => Number(d.U_BANDWIDTH),
      created: (d) => `${d.DATE} ${d.TIME}`,
    },
    // The API sends Hestia's file order; open in the user's own order (PREF_UI_SORT)
    initialSort: user?.sortOrder === 'date' ? { key: 'created', dir: 'desc' } : { key: 'name', dir: 'asc' },
  })

  // Hestia's web list keys: w statistics, l logs, Enter edit, s suspend, Delete.
  const keys = useRowShortcuts(table.rows, (d) => d.NAME, (d) => ({
    enter: () => navigate(`/web/${d.NAME}`),
    w: statsOn(d) ? () => window.open(statsUrl(d), '_blank', 'noopener,noreferrer') : undefined,
    ...(!readOnly && {
      l: () => navigate(`/web/${d.NAME}/logs`),
      s: isAdmin ? () => confirm.ask(suspendConfirm(d, t, m)) : undefined,
      delete: () => del.ask(d.NAME),
    }),
  }))

  /** `question` is a msgid with one %s: the domain, or the count of domains. */
  const bulk = (action: string, title: string, label: string, question: string, destructive = false) => {
    const n = table.selectedCount
    const subject = n === 1 ? table.selectedRows[0].NAME : t('%s web domains', n)
    confirm.ask({
      title,
      description: t(question, subject),
      confirmLabel: label,
      destructive,
      done: t('%s applied to %s', label, subject),
      onDone: table.clearSelection,
      run: () => m.bulkDomains({ action, names: table.selectedRows.map((d) => d.NAME) }),
    })
  }

  return (
    <div className="grid gap-6">
      <WebAside />
      <PageHeader
        title={t('Web domains')}
        description={t('Websites hosted on this server, with their SSL, PHP and traffic settings.')}
        actions={
          !readOnly && (
            <LinkButton href="/web/new" variant="primary" icon={PlusIcon} {...shortcutTarget('add')}>
              {t('Add web domain')}
            </LinkButton>
          )
        }
      />

      <div className="grid gap-3">
        <TableToolbar
          search={{
            label: t('Search web domains'),
            placeholder: t('Search domains, aliases or IPs'),
            value: table.query,
            onChange: table.setQuery,
          }}
          filters={
            <Select
              aria-label={t('Filter by status')}
              value={status}
              onValueChange={(v) => setStatus(v as keyof typeof statusFilters)}
              items={{ all: t(statusFilters.all), active: t(statusFilters.active), suspended: t(statusFilters.suspended) }}
              render={<Toolbar.Button />}
            />
          }
          selectedCount={readOnly ? 0 : table.selectedCount}
          onClearSelection={table.clearSelection}
          bulkActions={
            <>
              {options?.canRebuild && (
                <Button size="sm" icon={ArrowsClockwiseIcon} onClick={() =>
                    bulk('rebuild', t('Rebuild web domains'), t('Rebuild'), msgid('Are you sure you want to rebuild %s?'))
                  }
                >
                  {t('Rebuild')}
                </Button>
              )}
              {options?.purge && (
                <Button
                  size="sm"
                  onClick={() => bulk('purge', t('Purge NGINX cache'), t('Purge cache'), msgid('Purge the NGINX cache for %s?'))}
                >
                  {t('Purge cache')}
                </Button>
              )}
              {isAdmin && (
                <>
                  <Button size="sm" icon={PauseCircleIcon} onClick={() =>
                      bulk('suspend', t('Suspend web domains'), t('Suspend'), msgid('Are you sure you want to suspend %s?'), true)
                    }
                  >
                    {t('Suspend')}
                  </Button>
                  <Button size="sm" icon={PlayCircleIcon} onClick={() =>
                      bulk('unsuspend', t('Unsuspend web domains'), t('Unsuspend'), msgid('Are you sure you want to unsuspend %s?'))
                    }
                  >
                    {t('Unsuspend')}
                  </Button>
                </>
              )}
              <Button
                size="sm"
                variant="secondary-destructive"
                icon={TrashIcon}
                onClick={() =>
                  bulk('delete', t('Delete web domains'), t('Delete'), msgid('Are you sure you want to delete %s?'), true)
                }
              >
                {t('Delete')}
              </Button>
            </>
          }
        />

        {list.isPending ? (
          <DnsLoading />
        ) : list.isError ? (
          <DnsError error={list.error} />
        ) : (
          <TableCard
            rowShortcuts={keys}
            noun={table.filteredCount === 1 ? t('web domain') : t('web domains')}
            filteredCount={table.filteredCount}
            totalCount={all.length}
            onClearSearch={() => {
              table.setQuery('')
              setStatus('all')
            }}
            emptyState={{
              icon: <GlobeIcon size={40} className="text-kumo-inactive" />,
              title: t('No web domains yet'),
              description: t('Add a domain to start hosting a website on this server.'),
              action: !readOnly && (
                <LinkButton href="/web/new" variant="primary" icon={PlusIcon}>
                  {t('Add web domain')}
                </LinkButton>
              ),
            }}
          >
            <Table className="whitespace-nowrap">
              <Table.Header>
                <Table.Row>
                  {!readOnly && (
                    <Table.CheckHead
                      checked={table.allSelected}
                      indeterminate={table.someSelected && !table.allSelected}
                      onCheckedChange={table.toggleAll}
                      label={t('Select all web domains')}
                    />
                  )}
                  <SortableHead sortKey="name" sort={table.sort} onSort={table.toggleSort} sticky="left">
                    {t('Domain')}
                  </SortableHead>
                  {show('ip') && (
                    <SortableHead sortKey="ip" sort={table.sort} onSort={table.toggleSort}>
                      {t('IP address')}
                    </SortableHead>
                  )}
                  {show('backend') && <Table.Head>{t('Backend')}</Table.Head>}
                  <SortableHead sortKey="disk" sort={table.sort} onSort={table.toggleSort} align="right">
                    {t('Disk')}
                  </SortableHead>
                  {show('bandwidth') && (
                    <SortableHead sortKey="bandwidth" sort={table.sort} onSort={table.toggleSort} align="right">
                      {t('Bandwidth')}
                    </SortableHead>
                  )}
                  <Table.Head className="text-center">SSL</Table.Head>
                  <Table.Head className="text-center">{t('Statistics')}</Table.Head>
                  {show('created') && (
                    <SortableHead sortKey="created" sort={table.sort} onSort={table.toggleSort}>
                      {t('Created')}
                    </SortableHead>
                  )}
                  <Table.Head>{t('Status')}</Table.Head>
                  <Table.Head sticky="right" className="w-px text-right">
                    <ColumnsMenu visibility={columns} />
                  </Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {table.rows.map((d) => {
                  const hint = aliasHint(d)
                  const suspended = d.SUSPENDED === 'yes'
                  return (
                    <Table.Row
                      key={d.NAME}
                      variant={table.isSelected(d.NAME) ? 'selected' : 'default'}
                      {...rowLink(() => navigate(`/web/${d.NAME}`))}
                      {...keys.rowProps(d.NAME)}
                    >
                      {!readOnly && (
                        <Table.CheckCell
                          checked={table.isSelected(d.NAME)}
                          onCheckedChange={(_, details) => table.toggle(d.NAME, details)}
                          label={t('Select %s', d.NAME)}
                        />
                      )}
                      <Table.Cell sticky="left">
                        <div className="flex flex-col gap-0.5">
                          {readOnly ? (
                            <span className="font-medium text-kumo-default">{d.NAME}</span>
                          ) : (
                            <RouterLink to={`/web/${d.NAME}`} className="font-medium text-kumo-default hover:underline">
                              {d.NAME}
                            </RouterLink>
                          )}
                          {hint !== '' && (
                            <Text variant="secondary" as="span">
                              {hint}
                            </Text>
                          )}
                        </div>
                      </Table.Cell>
                      {show('ip') && (
                        <Table.Cell>
                          <Text variant="mono" as="span">
                            {displayIp(d)}
                          </Text>
                        </Table.Cell>
                      )}
                      {show('backend') && <Table.Cell>{formatBackend(d.BACKEND)}</Table.Cell>}
                      <Table.Cell className="text-right tabular-nums">{formatMB(d.U_DISK)}</Table.Cell>
                      {show('bandwidth') && (
                        <Table.Cell className="text-right tabular-nums">{formatMB(d.U_BANDWIDTH)}</Table.Cell>
                      )}
                      <Table.Cell className="text-center">
                        <StateIcon on={d.SSL === 'yes'} label="SSL" suspended={suspended} />
                      </Table.Cell>
                      <Table.Cell className="text-center">
                        <StateIcon on={statsOn(d)} label={t('Statistics')} suspended={suspended} />
                      </Table.Cell>
                      {show('created') && <Table.Cell className="tabular-nums text-kumo-subtle">{d.DATE}</Table.Cell>}
                      <Table.Cell>
                        <StatusBadge suspended={suspended} />
                      </Table.Cell>
                      <Table.Cell sticky="right" className="text-right">
                        <RowActions
                          d={d}
                          options={options}
                          readOnly={readOnly}
                          isAdmin={isAdmin}
                          ask={confirm.ask}
                          onDelete={del.ask}
                          m={m}
                        />
                      </Table.Cell>
                    </Table.Row>
                  )
                })}
              </Table.Body>
            </Table>
          </TableCard>
        )}
      </div>

      <ListConfirmDialog confirm={confirm} />
      <ResourceDeleteDialog state={del} resourceType={t('web domain')} onDelete={m.deleteDomain} />
    </div>
  )
}
