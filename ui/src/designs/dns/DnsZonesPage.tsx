import { useConfirm, ListConfirmDialog, useResourceDelete, ResourceDeleteDialog } from '../../components/useListConfirm'
import type { PendingConfirm } from '../../components/useListConfirm'
import { Badge, Button, DropdownMenu, LinkButton, Select, Table, Text, Toolbar, Tooltip } from '@cloudflare/kumo'
import {
  ArrowsClockwiseIcon,
  CheckCircleIcon,
  KeyIcon,
  ListBulletsIcon,
  PauseCircleIcon,
  PencilSimpleIcon,
  PlayCircleIcon,
  PlusCircleIcon,
  PlusIcon,
  SignpostIcon,
  TrashIcon,
  XCircleIcon,
} from '@phosphor-icons/react'
import { useMemo, useState } from 'react'
import { Link as RouterLink, useNavigate } from 'react-router'
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
import { useDnsMutation, useDnsZones } from '../../api/dns'
import type { DNSDomain } from '../../api/hestia.gen'
import { useT, type Translate } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { useSessionUser } from '../../session/SessionProvider'
import { DnsAside } from './DnsAside'
import { DnsError, DnsLoading } from './shared'
import { shortcutTarget } from '../../shortcuts/targets'
import { useRowShortcuts } from '../../shortcuts/useRowShortcuts'

export function DnssecIndicator({ enabled }: { enabled: boolean }) {
  const t = useT()
  return enabled ? (
    <span className="inline-flex items-center gap-1.5 text-kumo-success">
      <CheckCircleIcon weight="fill" />
      <Text as="span">{t('Enabled')}</Text>
    </span>
  ) : (
    <span className="inline-flex items-center gap-1.5 text-kumo-subtle">
      <XCircleIcon />
      <Text as="span" variant="secondary">
        {t('Disabled')}
      </Text>
    </span>
  )
}

const statusFilters = {
  all: msgid('Show all'),
  active: msgid('Active'),
  suspended: msgid('Suspended'),
}

// SOA is shown on the zone's records page; it is the same server for almost every zone
const optionalColumns: ToggleableColumn[] = [
  { key: 'ip', label: msgid('IP address') },
  { key: 'template', label: msgid('Template') },
  { key: 'ttl', label: 'TTL' },
  { key: 'created', label: msgid('Created') },
]

type Row = DNSDomain

/** The suspend or unsuspend question for a zone, shared by its menu and the s key. */
function suspendConfirm(
  d: Row,
  t: Translate,
  run: (name: string, suspended: boolean) => Promise<unknown>,
): PendingConfirm {
  const name = d.NAME
  const suspended = d.SUSPENDED === 'yes'
  return {
    title: suspended ? t('Unsuspend DNS domain') : t('Suspend DNS domain'),
    description: suspended
      ? t('Are you sure you want to unsuspend domain %s?', name)
      : t('Are you sure you want to suspend domain %s?', name),
    confirmLabel: suspended ? t('Unsuspend') : t('Suspend'),
    destructive: !suspended,
    done: suspended ? t('%s unsuspended', name) : t('%s suspended', name),
    run: () => run(name, suspended),
  }
}

function RowActions({
  d,
  ask,
  onDelete,
  readOnly,
  isAdmin,
  run,
}: {
  d: Row
  ask: (p: PendingConfirm) => void
  onDelete: (name: string) => void
  readOnly: boolean
  /** Suspend and unsuspend are admin-only on the server. */
  isAdmin: boolean
  run: (name: string, suspended: boolean) => Promise<unknown>
}) {
  const navigate = useNavigate()
  const t = useT()
  const suspended = d.SUSPENDED === 'yes'
  const name = d.NAME
  return (
    <DropdownMenu>
      <DropdownMenu.Trigger render={rowMenuTrigger(t('Actions for %s', name))} />
      <DropdownMenu.Content align="end">
        <DropdownMenu.Item icon={ListBulletsIcon} onClick={() => navigate(`/dns/${name}`)}>
          {t('DNS records')}
        </DropdownMenu.Item>
        {!readOnly && !suspended && (
          <>
            <DropdownMenu.Item icon={PlusCircleIcon} onClick={() => navigate(`/dns/${name}/records/new`)}>
              {t('Add DNS record')}
            </DropdownMenu.Item>
            <DropdownMenu.Item icon={PencilSimpleIcon} onClick={() => navigate(`/dns/${name}/edit`)}>
              {t('Edit DNS domain')}
            </DropdownMenu.Item>
            {d.DNSSEC === 'yes' && (
              <DropdownMenu.Item icon={KeyIcon} onClick={() => navigate(`/dns/${name}/dnssec`)}>
                {t('View public DNSSEC key')}
              </DropdownMenu.Item>
            )}
          </>
        )}
        {!readOnly && (
          <>
            <DropdownMenu.Separator />
            {isAdmin && (
              <DropdownMenu.Item
                icon={suspended ? PlayCircleIcon : PauseCircleIcon}
                onClick={() => ask(suspendConfirm(d, t, run))}
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

/** Each bulk action's confirm title, question and button, as msgids. */
const bulkActions = {
  rebuild: {
    title: msgid('Rebuild DNS zones'),
    ask: msgid('Are you sure you want to rebuild %s?'),
    label: msgid('Rebuild'),
  },
  suspend: {
    title: msgid('Suspend DNS zones'),
    ask: msgid('Are you sure you want to suspend %s?'),
    label: msgid('Suspend'),
  },
  unsuspend: {
    title: msgid('Unsuspend DNS zones'),
    ask: msgid('Are you sure you want to unsuspend %s?'),
    label: msgid('Unsuspend'),
  },
  delete: {
    title: msgid('Delete DNS zones'),
    ask: msgid('Are you sure you want to delete %s?'),
    label: msgid('Delete'),
  },
}

export function DnsZonesPage() {
  const navigate = useNavigate()
  const t = useT()
  const user = useSessionUser()
  const readOnly = user?.readOnly ?? false
  // The signed-in role, even while looking at another user (Parity 5)
  const isAdmin = user?.role === 'admin'
  const zones = useDnsZones()
  const m = useDnsMutation()
  const [status, setStatus] = useState<keyof typeof statusFilters>('all')
  const confirm = useConfirm()
  const del = useResourceDelete()
  const statusItems = { all: t(statusFilters.all), active: t(statusFilters.active), suspended: t(statusFilters.suspended) }
  const columns = useColumnVisibility(
    'dns-zones',
    optionalColumns.map((c) => ({ ...c, label: t(c.label) })),
  )
  const show = columns.isVisible

  const all = useMemo<Row[]>(() => zones.data?.zones ?? [], [zones.data])
  const byStatus = useMemo(
    () => (status === 'all' ? all : all.filter((d) => (d.SUSPENDED === 'yes') === (status === 'suspended'))),
    [all, status],
  )
  const table = useDataTable({
    rows: byStatus,
    getId: (d) => d.NAME,
    search: (d, q) => d.NAME.includes(q) || d.IP.includes(q) || d.TPL.includes(q) || d.SRC.includes(q),
    sorts: {
      name: (d) => d.NAME,
      ip: (d) => d.IP,
      records: (d) => Number(d.RECORDS),
      ttl: (d) => Number(d.TTL),
      expire: (d) => d.EXP,
      created: (d) => `${d.DATE} ${d.TIME}`,
    },
    // The API sends Hestia's file order; open in the user's own order (PREF_UI_SORT).
    initialSort: user?.sortOrder === 'date' ? { key: 'created', dir: 'desc' } : { key: 'name', dir: 'asc' },
  })

  const suspendRun = (name: string, suspended: boolean) => (suspended ? m.unsuspendZone(name) : m.suspendZone(name))
  // Hestia's DNS list keys: n add record, l records, Enter open, s suspend, Delete.
  const keys = useRowShortcuts(table.rows, (d) => d.NAME, (d) => ({
    enter: () => navigate(`/dns/${d.NAME}`),
    l: () => navigate(`/dns/${d.NAME}`),
    ...(!readOnly && {
      n: d.SUSPENDED === 'yes' ? undefined : () => navigate(`/dns/${d.NAME}/records/new`),
      s: isAdmin ? () => confirm.ask(suspendConfirm(d, t, suspendRun)) : undefined,
      delete: () => del.ask(d.NAME),
    }),
  }))

  const bulk = (action: keyof typeof bulkActions, destructive = false) => {
    const n = table.selectedCount
    const subject = n === 1 ? table.selectedRows[0].NAME : t('%s DNS zones', n)
    const text = bulkActions[action]
    confirm.ask({
      title: t(text.title),
      description: t(text.ask, subject),
      confirmLabel: t(text.label),
      destructive,
      done: t('%s applied to %s', t(text.label), subject),
      onDone: table.clearSelection,
      run: () => m.bulkZones({ action, names: table.selectedRows.map((d) => d.NAME) }),
    })
  }

  return (
    <div className="grid gap-6">
      <DnsAside />
      <PageHeader
        title="DNS"
        description={t('Authoritative DNS zones served by this server, with their records and DNSSEC keys.')}
        actions={
          !readOnly && (
            <LinkButton href="/dns/new" variant="primary" icon={PlusIcon} {...shortcutTarget('add')}>
              {t('Add DNS domain')}
            </LinkButton>
          )
        }
      />

      <div className="grid gap-3">
        <TableToolbar
          search={{
            label: t('Search DNS zones'),
            placeholder: t('Search zones, IPs or templates'),
            value: table.query,
            onChange: table.setQuery,
          }}
          filters={
            <Select
              aria-label={t('Filter by status')}
              value={status}
              onValueChange={(v) => setStatus(v as keyof typeof statusFilters)}
              items={statusItems}
              render={<Toolbar.Button />}
            />
          }
          selectedCount={readOnly ? 0 : table.selectedCount}
          onClearSelection={table.clearSelection}
          bulkActions={
            <>
              {isAdmin && (
                <Button size="sm" icon={ArrowsClockwiseIcon} onClick={() => bulk('rebuild')}>
                  {t('Rebuild')}
                </Button>
              )}
              {isAdmin && (
                <>
                  <Button size="sm" icon={PauseCircleIcon} onClick={() => bulk('suspend', true)}>
                    {t('Suspend')}
                  </Button>
                  <Button size="sm" icon={PlayCircleIcon} onClick={() => bulk('unsuspend')}>
                    {t('Unsuspend')}
                  </Button>
                </>
              )}
              <Button size="sm" variant="secondary-destructive" icon={TrashIcon} onClick={() => bulk('delete', true)}>
                {t('Delete')}
              </Button>
            </>
          }
        />

        {zones.isPending ? (
          <DnsLoading />
        ) : zones.isError ? (
          <DnsError error={zones.error} />
        ) : (
          <TableCard
            rowShortcuts={keys}
            noun={{ one: t('DNS zone'), other: t('DNS zones') }}
            filteredCount={table.filteredCount}
            totalCount={all.length}
            onClearSearch={() => {
              table.setQuery('')
              setStatus('all')
            }}
            emptyState={{
              icon: <SignpostIcon size={40} className="text-kumo-inactive" />,
              title: t('No DNS zones yet'),
              description: t('Add a DNS domain to start serving records for it from this server.'),
              action: !readOnly && (
                <LinkButton href="/dns/new" variant="primary" icon={PlusIcon}>
                  {t('Add DNS domain')}
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
                      label={t('Select all DNS zones')}
                    />
                  )}
                  <SortableHead sortKey="name" sort={table.sort} onSort={table.toggleSort} sticky="left">
                    {t('Name')}
                  </SortableHead>
                  {show('ip') && (
                    <SortableHead sortKey="ip" sort={table.sort} onSort={table.toggleSort}>
                      {t('IP address')}
                    </SortableHead>
                  )}
                  <SortableHead sortKey="records" sort={table.sort} onSort={table.toggleSort} align="right">
                    {t('Records')}
                  </SortableHead>
                  {show('template') && <Table.Head>{t('Template')}</Table.Head>}
                  {show('ttl') && (
                    <SortableHead sortKey="ttl" sort={table.sort} onSort={table.toggleSort} align="right">
                      TTL
                    </SortableHead>
                  )}
                  <Table.Head>DNSSEC</Table.Head>
                  <SortableHead sortKey="expire" sort={table.sort} onSort={table.toggleSort}>
                    {t('Expires')}
                  </SortableHead>
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
                {table.rows.map((d) => (
                  <Table.Row
                    key={d.NAME}
                    variant={table.isSelected(d.NAME) ? 'selected' : 'default'}
                    {...rowLink(() => navigate(`/dns/${d.NAME}`))}
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
                      <span className="inline-flex items-center gap-2">
                        <RouterLink to={`/dns/${d.NAME}`} className="font-medium text-kumo-default hover:underline">
                          {d.NAME}
                        </RouterLink>
                        {d.SRC && (
                          <Tooltip
                            content={t('Synced from %s', d.SRC)}
                            render={
                              <span>
                                <Badge variant="outline">{t('Cluster')}</Badge>
                              </span>
                            }
                          />
                        )}
                      </span>
                    </Table.Cell>
                    {show('ip') && (
                      <Table.Cell>
                        <Text variant="mono" as="span">
                          {d.IP}
                        </Text>
                      </Table.Cell>
                    )}
                    <Table.Cell className="text-right tabular-nums">{d.RECORDS || '0'}</Table.Cell>
                    {show('template') && (
                      <Table.Cell>
                        <Badge variant="neutral">{d.TPL}</Badge>
                      </Table.Cell>
                    )}
                    {show('ttl') && <Table.Cell className="text-right tabular-nums">{d.TTL}</Table.Cell>}
                    <Table.Cell>
                      <DnssecIndicator enabled={d.DNSSEC === 'yes'} />
                    </Table.Cell>
                    <Table.Cell className="tabular-nums">
                      <time dateTime={d.EXP}>{d.EXP}</time>
                    </Table.Cell>
                    {show('created') && <Table.Cell className="tabular-nums text-kumo-subtle">{d.DATE}</Table.Cell>}
                    <Table.Cell>
                      <StatusBadge suspended={d.SUSPENDED === 'yes'} />
                    </Table.Cell>
                    <Table.Cell sticky="right" className="text-right">
                      <RowActions
                        d={d}
                        ask={confirm.ask}
                        onDelete={del.ask}
                        readOnly={readOnly}
                        isAdmin={isAdmin}
                        run={suspendRun}
                      />
                    </Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </Table>
          </TableCard>
        )}
      </div>

      <ListConfirmDialog confirm={confirm} />
      <ResourceDeleteDialog state={del} resourceType={t('DNS domain')} onDelete={m.deleteZone} />
    </div>
  )
}
