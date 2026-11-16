import { useConfirm, ListConfirmDialog, useResourceDelete, ResourceDeleteDialog } from '../../components/useListConfirm'
import type { PendingConfirm } from '../../components/useListConfirm'
import { Button, DropdownMenu, LinkButton, Select, Table, Text, Toolbar } from '@cloudflare/kumo'
import {
  ArrowSquareOutIcon,
  ArrowsClockwiseIcon,
  EnvelopeSimpleIcon,
  ListBulletsIcon,
  PauseCircleIcon,
  PencilSimpleIcon,
  PlayCircleIcon,
  PlusCircleIcon,
  PlusIcon,
  SignpostIcon,
  TrashIcon,
} from '@phosphor-icons/react'
import { useMemo, useState } from 'react'
import { Link as RouterLink, useNavigate } from 'react-router'
import { useMailDomains, useMailMutation, useMailOptions } from '../../api/mail'
import { useT, type Translate } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import type { MailDomain } from '../../api/hestia.gen'
import type { MailOptions } from '../../api/types.gen'
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
import { formatMB } from '../../lib/format'
import { useSessionUser } from '../../session/SessionProvider'
import { DnsError, DnsLoading } from '../dns/shared'
import { FeatureIcon, isMailFeatureOn, mailWebmailUrl } from './shared'
import { MailAside } from './MailAside'
import { shortcutTarget } from '../../shortcuts/targets'
import { useRowShortcuts } from '../../shortcuts/useRowShortcuts'

const statusFilters = {
  all: msgid('Show all'),
  active: msgid('Active'),
  suspended: msgid('Suspended'),
}

const optionalColumns: ToggleableColumn[] = [
  { key: 'antivirus', label: msgid('Anti-virus') },
  { key: 'antispam', label: msgid('Spam filter') },
  { key: 'dkim', label: 'DKIM' },
  { key: 'ssl', label: 'SSL' },
  { key: 'created', label: msgid('Created') },
]

const hasWebmail = (d: MailDomain, options: MailOptions | undefined) =>
  Boolean(options?.webmail) && (options?.webmailClients.length ?? 0) > 0 && d.WEBMAIL !== 'disabled' && d.WEBMAIL !== ''

/** The suspend or unsuspend question for a domain, shared by its menu and the s key. */
function suspendConfirm(
  d: MailDomain,
  t: Translate,
  run: (name: string, suspended: boolean) => Promise<unknown>,
): PendingConfirm {
  const name = d.NAME
  const suspended = d.SUSPENDED === 'yes'
  return {
    title: suspended ? t('Unsuspend mail domain') : t('Suspend mail domain'),
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
  options,
  run,
}: {
  d: MailDomain
  ask: (p: PendingConfirm) => void
  onDelete: (name: string) => void
  readOnly: boolean
  /** Suspend and unsuspend are admin-only on the server. */
  isAdmin: boolean
  options: MailOptions | undefined
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
        <DropdownMenu.Item icon={ListBulletsIcon} onClick={() => navigate(`/mail/${name}`)}>
          {t('Mail accounts')}
        </DropdownMenu.Item>
        <DropdownMenu.Item icon={SignpostIcon} onClick={() => navigate(`/mail/${name}/dns`)}>
          {t('DNS records')}
        </DropdownMenu.Item>
        {!suspended && hasWebmail(d, options) && (
          <DropdownMenu.LinkItem
            icon={ArrowSquareOutIcon}
            href={mailWebmailUrl(options?.webmailAlias ?? '', name)}
            target="_blank"
            rel="noopener noreferrer"
          >
            {t('Open webmail')}
          </DropdownMenu.LinkItem>
        )}
        {!readOnly && !suspended && (
          <>
            <DropdownMenu.Item icon={PlusCircleIcon} onClick={() => navigate(`/mail/${name}/accounts/new`)}>
              {t('Add mail account')}
            </DropdownMenu.Item>
            <DropdownMenu.Item icon={PencilSimpleIcon} onClick={() => navigate(`/mail/${name}/edit`)}>
              {t('Edit mail domain')}
            </DropdownMenu.Item>
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

/** Each bulk action's msgids: dialog title, button label and question. */
const bulkText = {
  rebuild: { title: msgid('Rebuild mail domains'), label: msgid('Rebuild all'), ask: msgid('Are you sure you want to rebuild %s?') },
  suspend: { title: msgid('Suspend mail domains'), label: msgid('Suspend'), ask: msgid('Are you sure you want to suspend %s?') },
  unsuspend: {
    title: msgid('Unsuspend mail domains'),
    label: msgid('Unsuspend'),
    ask: msgid('Are you sure you want to unsuspend %s?'),
  },
  delete: { title: msgid('Delete mail domains'), label: msgid('Delete'), ask: msgid('Are you sure you want to delete %s?') },
}

export function MailDomainsPage() {
  const t = useT()
  const navigate = useNavigate()
  const user = useSessionUser()
  const readOnly = user?.readOnly ?? false
  // The signed-in role, even while looking at another user
  const isAdmin = user?.role === 'admin'
  const list = useMailDomains()
  const options = useMailOptions().data
  const m = useMailMutation()
  const [status, setStatus] = useState<keyof typeof statusFilters>('all')
  const confirm = useConfirm()
  const del = useResourceDelete()
  const columns = useColumnVisibility(
    'mail-domains',
    optionalColumns.map((c) => ({ ...c, label: t(c.label) })),
  )
  const show = columns.isVisible

  const all = useMemo<MailDomain[]>(() => list.data?.domains ?? [], [list.data])
  const byStatus = useMemo(
    () => (status === 'all' ? all : all.filter((d) => (d.SUSPENDED === 'yes') === (status === 'suspended'))),
    [all, status],
  )
  const table = useDataTable({
    rows: byStatus,
    getId: (d) => d.NAME,
    search: (d, q) => d.NAME.toLowerCase().includes(q) || d.CATCHALL.toLowerCase().includes(q),
    sorts: {
      name: (d) => d.NAME,
      accounts: (d) => Number(d.ACCOUNTS),
      disk: (d) => Number(d.U_DISK),
      created: (d) => `${d.DATE} ${d.TIME}`,
    },
    // The API sends Hestia's file order; open in the user's own order (PREF_UI_SORT).
    // There is no Created column, so this is only the order the headers return to.
    initialSort: user?.sortOrder === 'date' ? { key: 'created', dir: 'desc' } : { key: 'name', dir: 'asc' },
  })

  const suspendRun = (name: string, wasSuspended: boolean) =>
    wasSuspended ? m.unsuspendDomain(name) : m.suspendDomain(name)
  // Hestia's mail list keys: n add account, l accounts, Enter open, s suspend, Delete.
  const keys = useRowShortcuts(table.rows, (d) => d.NAME, (d) => ({
    enter: () => navigate(`/mail/${d.NAME}`),
    l: () => navigate(`/mail/${d.NAME}`),
    ...(!readOnly && {
      n: d.SUSPENDED === 'yes' ? undefined : () => navigate(`/mail/${d.NAME}/accounts/new`),
      s: isAdmin ? () => confirm.ask(suspendConfirm(d, t, suspendRun)) : undefined,
      delete: () => del.ask(d.NAME),
    }),
  }))

  const bulk = (verb: keyof typeof bulkText, destructive = false) => {
    const n = table.selectedCount
    const subject = n === 1 ? table.selectedRows[0].NAME : t('%s mail domains', n)
    const text = bulkText[verb]
    confirm.ask({
      title: t(text.title),
      description: t(text.ask, subject),
      confirmLabel: t(text.label),
      destructive,
      done: t('%s applied to %s', t(text.label), subject),
      onDone: table.clearSelection,
      run: () => m.bulkDomains({ action: verb, names: table.selectedRows.map((d) => d.NAME) }),
    })
  }

  return (
    <div className="grid gap-6">
      <MailAside />
      <PageHeader
        title={t('Mail')}
        description={t('Mail domains on this server, with their spam, virus and DKIM protection.')}
        actions={
          !readOnly && (
            <LinkButton href="/mail/new" variant="primary" icon={PlusIcon} {...shortcutTarget('add')}>
              {t('Add mail domain')}
            </LinkButton>
          )
        }
      />

      <div className="grid gap-3">
        <TableToolbar
          search={{
            label: t('Search mail domains'),
            placeholder: t('Search domains or catch-all addresses'),
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
              {isAdmin && (
                <>
                  <Button size="sm" icon={ArrowsClockwiseIcon} onClick={() => bulk('rebuild')}>
                    {t('Rebuild all')}
                  </Button>
                  <Button size="sm" icon={PauseCircleIcon} onClick={() => bulk('suspend', true)}>
                    {t('Suspend')}
                  </Button>
                  <Button size="sm" icon={PlayCircleIcon} onClick={() => bulk('unsuspend')}>
                    {t('Unsuspend')}
                  </Button>
                </>
              )}
              <Button
                size="sm"
                variant="secondary-destructive"
                icon={TrashIcon}
                onClick={() => bulk('delete', true)}
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
            noun={{ one: t('mail domain'), other: t('mail domains') }}
            filteredCount={table.filteredCount}
            totalCount={all.length}
            onClearSearch={() => {
              table.setQuery('')
              setStatus('all')
            }}
            emptyState={{
              icon: <EnvelopeSimpleIcon size={40} className="text-kumo-inactive" />,
              title: t('No mail domains yet'),
              description: t('Add a mail domain to create mailboxes, aliases and forwarders for it.'),
              action: !readOnly && (
                <LinkButton href="/mail/new" variant="primary" icon={PlusIcon}>
                  {t('Add mail domain')}
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
                      label={t('Select all mail domains')}
                    />
                  )}
                  <SortableHead sortKey="name" sort={table.sort} onSort={table.toggleSort} sticky="left">
                    {t('Name')}
                  </SortableHead>
                  <SortableHead sortKey="accounts" sort={table.sort} onSort={table.toggleSort} align="right">
                    {t('Accounts')}
                  </SortableHead>
                  <SortableHead sortKey="disk" sort={table.sort} onSort={table.toggleSort} align="right">
                    {t('Disk')}
                  </SortableHead>
                  {show('antivirus') && <Table.Head className="text-center">{t('Anti-virus')}</Table.Head>}
                  {show('antispam') && <Table.Head className="text-center">{t('Spam filter')}</Table.Head>}
                  {show('dkim') && <Table.Head className="text-center">DKIM</Table.Head>}
                  {show('ssl') && <Table.Head className="text-center">SSL</Table.Head>}
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
                    {...rowLink(() => navigate(`/mail/${d.NAME}`))}
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
                        <RouterLink to={`/mail/${d.NAME}`} className="font-medium text-kumo-default hover:underline">
                          {d.NAME}
                        </RouterLink>
                        {d.CATCHALL && (
                          <Text variant="secondary" size="sm" as="span">
                            {t('Catch-all: %s', d.CATCHALL)}
                          </Text>
                        )}
                      </div>
                    </Table.Cell>
                    <Table.Cell className="text-right tabular-nums">{d.ACCOUNTS}</Table.Cell>
                    <Table.Cell className="text-right tabular-nums">{formatMB(d.U_DISK)}</Table.Cell>
                    {show('antivirus') && (
                      <Table.Cell className="text-center">
                        <FeatureIcon enabled={isMailFeatureOn(d.ANTIVIRUS)} label={t('Anti-virus')} />
                      </Table.Cell>
                    )}
                    {show('antispam') && (
                      <Table.Cell className="text-center">
                        <FeatureIcon enabled={isMailFeatureOn(d.ANTISPAM)} label={t('Spam filter')} />
                      </Table.Cell>
                    )}
                    {show('dkim') && (
                      <Table.Cell className="text-center">
                        <FeatureIcon enabled={isMailFeatureOn(d.DKIM)} label="DKIM" />
                      </Table.Cell>
                    )}
                    {show('ssl') && (
                      <Table.Cell className="text-center">
                        <FeatureIcon enabled={isMailFeatureOn(d.SSL)} label="SSL" />
                      </Table.Cell>
                    )}
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
                        options={options}
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
      <ResourceDeleteDialog state={del} resourceType={t('mail domain')} onDelete={m.deleteDomain} />
    </div>
  )
}
