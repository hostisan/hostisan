import { useConfirm, ListConfirmDialog } from '../../components/useListConfirm'
import type { PendingConfirm } from '../../components/useListConfirm'
import { Banner, Button, DropdownMenu, LinkButton, Select, Table, Toolbar } from '@cloudflare/kumo'
import {
  KeyIcon,
  ListBulletsIcon,
  PauseCircleIcon,
  PencilSimpleIcon,
  PlayCircleIcon,
  PlusIcon,
  TrashIcon,
  WarningIcon,
} from '@phosphor-icons/react'
import { useMemo, useState } from 'react'
import { Link as RouterLink, useNavigate, useParams } from 'react-router'
import { PageHeader } from '../../components/PageHeader'
import { StatusBadge } from '../../components/StatusBadge'
import { SortableHead, TableCard, TableToolbar, rowLink, rowMenuTrigger, useDataTable } from '../../components/table'
import { useDnsMutation, useDnsRecords } from '../../api/dns'
import { ApiError } from '../../api/client'
import { apiErrorText } from '../../api/errors'
import type { DNSRecord } from '../../api/hestia.gen'
import { useT, type Translate } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { useSessionUser } from '../../session/SessionProvider'
import { DnsLoading, RecordTypeBadge, dnsRecordTypes } from './shared'
import { shortcutTarget } from '../../shortcuts/targets'
import { useRowShortcuts } from '../../shortcuts/useRowShortcuts'

const typeOrder: Record<string, number> = Object.fromEntries(dnsRecordTypes.map((t, i) => [t, i]))

type DnsMutation = ReturnType<typeof useDnsMutation>

/** The suspend or unsuspend question for a record, shared by its menu and the s key. */
function suspendConfirm(r: DNSRecord, domain: string, t: Translate, m: DnsMutation): PendingConfirm {
  const suspended = r.SUSPENDED === 'yes'
  return {
    title: suspended ? t('Unsuspend DNS record') : t('Suspend DNS record'),
    description: suspended
      ? t('Are you sure you want to unsuspend record %s?', r.RECORD)
      : t('Are you sure you want to suspend record %s?', r.RECORD),
    confirmLabel: suspended ? t('Unsuspend') : t('Suspend'),
    destructive: !suspended,
    done: suspended ? t('Record %s unsuspended', r.RECORD) : t('Record %s suspended', r.RECORD),
    run: () => (suspended ? m.unsuspendRecord(domain, r.ID) : m.suspendRecord(domain, r.ID)),
  }
}

/** The delete question for a record, shared by its menu and the Delete key. */
function deleteConfirm(r: DNSRecord, domain: string, t: Translate, m: DnsMutation): PendingConfirm {
  return {
    title: t('Delete DNS record'),
    description: t('Are you sure you want to delete record %s?', r.RECORD),
    confirmLabel: t('Delete'),
    destructive: true,
    done: t('Record %s deleted', r.RECORD),
    run: () => m.deleteRecord(domain, r.ID),
  }
}

function RowActions({
  r,
  domain,
  ask,
  m,
  isAdmin,
}: {
  r: DNSRecord
  domain: string
  ask: (p: PendingConfirm) => void
  /** Suspend and unsuspend are admin-only on the server. */
  isAdmin: boolean
  m: DnsMutation
}) {
  const navigate = useNavigate()
  const t = useT()
  const suspended = r.SUSPENDED === 'yes'
  const label = `${r.RECORD} ${r.TYPE}`
  return (
    <DropdownMenu>
      <DropdownMenu.Trigger render={rowMenuTrigger(t('Actions for record %s', label))} />
      <DropdownMenu.Content align="end">
        {!suspended && (
          <DropdownMenu.Item icon={PencilSimpleIcon} onClick={() => navigate(`/dns/${domain}/records/${r.ID}`)}>
            {t('Edit DNS record')}
          </DropdownMenu.Item>
        )}
        {isAdmin && (
          <DropdownMenu.Item
            icon={suspended ? PlayCircleIcon : PauseCircleIcon}
            onClick={() => ask(suspendConfirm(r, domain, t, m))}
          >
            {suspended ? t('Unsuspend') : t('Suspend')}
          </DropdownMenu.Item>
        )}
        {(isAdmin || !suspended) && <DropdownMenu.Separator />}
        <DropdownMenu.Item
          icon={TrashIcon}
          variant="danger"
          onClick={() => ask(deleteConfirm(r, domain, t, m))}
        >
          {t('Delete')}
        </DropdownMenu.Item>
      </DropdownMenu.Content>
    </DropdownMenu>
  )
}

/** Each bulk action's confirm title, questions and button, as msgids. */
const bulkActions = {
  suspend: {
    title: msgid('Suspend DNS records'),
    one: msgid('Are you sure you want to suspend record %s?'),
    many: msgid('Are you sure you want to suspend %s?'),
    label: msgid('Suspend'),
  },
  unsuspend: {
    title: msgid('Unsuspend DNS records'),
    one: msgid('Are you sure you want to unsuspend record %s?'),
    many: msgid('Are you sure you want to unsuspend %s?'),
    label: msgid('Unsuspend'),
  },
  delete: {
    title: msgid('Delete DNS records'),
    one: msgid('Are you sure you want to delete record %s?'),
    many: msgid('Are you sure you want to delete %s?'),
    label: msgid('Delete'),
  },
}

export function DnsRecordsPage() {
  const { domain = '' } = useParams()
  const navigate = useNavigate()
  const t = useT()
  const user = useSessionUser()
  const query = useDnsRecords(domain)
  const m = useDnsMutation()
  const zone = query.data?.zone
  const readOnly = user?.readOnly ?? false
  // The signed-in role decides which bulk and row actions the server accepts (Parity 5)
  const isAdmin = user?.role === 'admin'
  const zoneSuspended = zone?.SUSPENDED === 'yes'
  const editable = !readOnly && !zoneSuspended
  const [type, setType] = useState('all')
  const confirm = useConfirm()

  const all = useMemo(() => query.data?.records ?? [], [query.data])
  const byType = useMemo(() => (type === 'all' ? all : all.filter((r) => r.TYPE === type)), [all, type])
  const typeItems = useMemo(
    () => ({
      all: t('All types'),
      ...Object.fromEntries(dnsRecordTypes.filter((k) => all.some((r) => r.TYPE === k)).map((k) => [k, k])),
    }),
    [all, t],
  )
  // Hestia lists records in zone order (by ID); headers re-sort from there
  const table = useDataTable({
    rows: byType,
    getId: (r) => r.ID,
    search: (r, q) => r.RECORD.toLowerCase().includes(q) || r.VALUE.toLowerCase().includes(q),
    sorts: {
      record: (r) => r.RECORD,
      type: (r) => (typeOrder[r.TYPE] ?? 99) * 1000 + Number(r.PRIORITY || 0),
      ttl: (r) => Number(r.TTL || zone?.TTL || 0),
    },
    pageSize: 20,
  })

  // Hestia's record list keys: Enter edit, s suspend, Delete.
  const keys = useRowShortcuts(table.rows, (r) => r.ID, (r) =>
    editable
      ? {
          enter: r.SUSPENDED === 'yes' ? undefined : () => navigate(`/dns/${domain}/records/${r.ID}`),
          s: isAdmin ? () => confirm.ask(suspendConfirm(r, domain, t, m)) : undefined,
          delete: () => confirm.ask(deleteConfirm(r, domain, t, m)),
        }
      : {},
  )

  if (!zone) {
    return (
      <div className="grid gap-6">
        <PageHeader title={t('DNS records')} back={{ href: '/dns', label: 'DNS' }} />
        {query.isError ? (
          <Banner
            variant="error"
            icon={<WarningIcon weight="fill" />}
            title={
              query.error instanceof ApiError && query.error.status === 404
                ? t("DNS domain %s doesn't exist", domain)
                : t('Could not load the DNS records')
            }
            description={<span className="whitespace-pre-line">{apiErrorText(query.error, t)}</span>}
          />
        ) : (
          <DnsLoading />
        )}
      </div>
    )
  }

  const bulk = (action: keyof typeof bulkActions, destructive = false) => {
    const n = table.selectedCount
    const text = bulkActions[action]
    const subject = n === 1 ? table.selectedRows[0].RECORD : t('%s DNS records', n)
    confirm.ask({
      title: t(text.title),
      description: n === 1 ? t(text.one, subject) : t(text.many, subject),
      confirmLabel: t(text.label),
      destructive,
      done: t('%s applied to %s', t(text.label), subject),
      onDone: table.clearSelection,
      run: async () => {
        // Users never see record ids: name each failure by its record and type
        const labels = new Map(table.selectedRows.map((r) => [r.ID, `${r.RECORD} ${r.TYPE}`]))
        const reply = await m.bulkRecords(domain, { action, names: [...labels.keys()] })
        return { ...reply, failed: reply.failed.map((f) => ({ ...f, name: labels.get(f.name) ?? f.name })) }
      },
    })
  }

  return (
    <div className="grid gap-6">
      <PageHeader
        title={domain}
        description={
          <span className="inline-flex flex-wrap items-center gap-2">
            {t('DNS records · template %s · SOA %s · serial %s', zone.TPL, zone.SOA, zone.SERIAL)}{' '}
            <StatusBadge suspended={zoneSuspended} />
          </span>
        }
        back={{ href: '/dns', label: 'DNS' }}
        actions={
          !readOnly && (
            <>
              {zone.DNSSEC === 'yes' && (
                <LinkButton href={`/dns/${domain}/dnssec`} variant="secondary" icon={KeyIcon}>
                  {t('DNSSEC key')}
                </LinkButton>
              )}
              <LinkButton href={`/dns/${domain}/edit`} variant="secondary" icon={PencilSimpleIcon}>
                {t('Edit DNS domain')}
              </LinkButton>
              {!zoneSuspended && (
                <LinkButton href={`/dns/${domain}/records/new`} variant="primary" icon={PlusIcon} {...shortcutTarget('add')}>
                  {t('Add record')}
                </LinkButton>
              )}
            </>
          )
        }
      />

      {zoneSuspended && (
        <Banner
          variant="alert"
          icon={<WarningIcon weight="fill" />}
          title={t('This DNS domain is suspended')}
          description={t('Its records are kept but not served. Unsuspend the domain to edit them.')}
        />
      )}

      <div className="grid gap-3">
        <TableToolbar
          search={{
            label: t('Search records'),
            placeholder: t('Search names or values'),
            value: table.query,
            onChange: table.setQuery,
          }}
          filters={
            <Select
              aria-label={t('Filter by record type')}
              value={type}
              onValueChange={(v) => {
                setType((v as string) ?? 'all')
                table.setPage(1)
              }}
              items={typeItems}
              render={<Toolbar.Button />}
            />
          }
          selectedCount={editable ? table.selectedCount : 0}
          onClearSelection={table.clearSelection}
          bulkActions={
            <>
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

        <TableCard
          rowShortcuts={keys}
          noun={{ one: t('DNS record'), other: t('DNS records') }}
          filteredCount={table.filteredCount}
          totalCount={all.length}
          onClearSearch={() => {
            table.setQuery('')
            setType('all')
          }}
          page={table.page}
          pageSize={table.pageSize}
          onPageChange={table.setPage}
          emptyState={{
            icon: <ListBulletsIcon size={40} className="text-kumo-inactive" />,
            title: t('This zone has no records'),
            description: t('Add an A, MX or TXT record to start answering queries for this domain.'),
            action: editable && (
              <LinkButton href={`/dns/${domain}/records/new`} variant="primary" icon={PlusIcon}>
                {t('Add record')}
              </LinkButton>
            ),
          }}
        >
          <Table className="whitespace-nowrap">
            <Table.Header>
              <Table.Row>
                {editable && (
                  <Table.CheckHead
                    checked={table.allSelected}
                    indeterminate={table.someSelected && !table.allSelected}
                    onCheckedChange={table.toggleAll}
                    label={t('Select all records')}
                  />
                )}
                <SortableHead sortKey="record" sort={table.sort} onSort={table.toggleSort} sticky="left">
                  {t('Record')}
                </SortableHead>
                <SortableHead sortKey="type" sort={table.sort} onSort={table.toggleSort}>
                  {t('Type')}
                </SortableHead>
                <Table.Head className="text-right">{t('Priority')}</Table.Head>
                <SortableHead sortKey="ttl" sort={table.sort} onSort={table.toggleSort} align="right">
                  TTL
                </SortableHead>
                <Table.Head className="w-full">{t('IP or value')}</Table.Head>
                <Table.Head>{t('Status')}</Table.Head>
                {editable && (
                  <Table.Head sticky="right">
                    <span className="sr-only">{t('Actions')}</span>
                  </Table.Head>
                )}
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {table.rows.map((r) => {
                const href = `/dns/${domain}/records/${r.ID}`
                const canOpen = editable && r.SUSPENDED !== 'yes'
                return (
                  <Table.Row
                    key={r.ID}
                    variant={table.isSelected(r.ID) ? 'selected' : 'default'}
                    {...(canOpen ? rowLink(() => navigate(href)) : {})}
                    {...keys.rowProps(r.ID)}
                  >
                    {editable && (
                      <Table.CheckCell
                        checked={table.isSelected(r.ID)}
                        onCheckedChange={(_, details) => table.toggle(r.ID, details)}
                        label={t('Select record %s', `${r.RECORD} ${r.TYPE}`)}
                      />
                    )}
                    <Table.Cell sticky="left">
                      {canOpen ? (
                        <RouterLink to={href} className="font-medium text-kumo-default hover:underline">
                          {r.RECORD}
                        </RouterLink>
                      ) : (
                        <span className="font-medium">{r.RECORD}</span>
                      )}
                    </Table.Cell>
                    <Table.Cell>
                      <RecordTypeBadge type={r.TYPE} />
                    </Table.Cell>
                    <Table.Cell className="text-right tabular-nums">
                      {r.PRIORITY || <span className="text-kumo-subtle">–</span>}
                    </Table.Cell>
                    <Table.Cell className="text-right tabular-nums">
                      {r.TTL || <span className="text-kumo-subtle">{t('Default')}</span>}
                    </Table.Cell>
                    <Table.Cell>
                      <span className="block max-w-md truncate font-mono text-[13px]" title={r.VALUE}>
                        {r.VALUE}
                      </span>
                    </Table.Cell>
                    <Table.Cell>
                      <StatusBadge suspended={r.SUSPENDED === 'yes'} />
                    </Table.Cell>
                    {editable && (
                      <Table.Cell sticky="right" className="text-right">
                        <RowActions r={r} domain={domain} ask={confirm.ask} m={m} isAdmin={isAdmin} />
                      </Table.Cell>
                    )}
                  </Table.Row>
                )
              })}
            </Table.Body>
          </Table>
        </TableCard>
      </div>

      <ListConfirmDialog confirm={confirm} />
    </div>
  )
}
