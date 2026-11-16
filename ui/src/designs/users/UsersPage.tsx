import { Badge, Button, DropdownMenu, LayerCard, LinkButton, Loader, Select, Table, Text, Toolbar, useKumoToastManager } from '@cloudflare/kumo'
import {
  ArchiveIcon,
  ArrowsClockwiseIcon,
  CaretDownIcon,
  DatabaseIcon,
  EnvelopeSimpleIcon,
  GlobeIcon,
  HardDrivesIcon,
  PackageIcon,
  PauseCircleIcon,
  PencilSimpleIcon,
  PlayCircleIcon,
  PlusIcon,
  SignInIcon,
  SignpostIcon,
  SortAscendingIcon,
  TrashIcon,
  UsersIcon,
} from '@phosphor-icons/react'
import { useMemo, useState } from 'react'
import { Link as RouterLink, useNavigate } from 'react-router'
import { apiErrorText, bulkMessage } from '../../api/errors'
import type { UserEntry } from '../../api/hestia.gen'
import type { BulkReply } from '../../api/types.gen'
import { useUserMutation, useUsers, type UserBulkAction } from '../../api/users'
import { PageHeader } from '../../components/PageHeader'
import { StatusBadge } from '../../components/StatusBadge'
import {
  ColumnsMenu,
  IconHead,
  SortableHead,
  TableCard,
  TableToolbar,
  rowLink,
  rowMenuTrigger,
  useColumnVisibility,
  useDataTable,
  type ToggleableColumn,
} from '../../components/table'
import { ListConfirmDialog, useConfirm, type PendingConfirm } from '../../components/useListConfirm'
import { useT, type Translate } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { formatMB } from '../../lib/format'
import { useSessionUser } from '../../session/SessionProvider'
import { useLoginAs } from './useLoginAs'
import { DnsError, DnsLoading } from '../dns/shared'
import { UsageMeter } from './fields'
import { UsersAside } from './UsersAside'
import { shortcutTarget } from '../../shortcuts/targets'
import { useRowShortcuts } from '../../shortcuts/useRowShortcuts'

type Row = UserEntry

// Hestia's bulk menu (users §1.1); the heavy ones run one user per request.
const BULK_REBUILDS: [UserBulkAction, string][] = [
  ['rebuild', msgid('Rebuild all')],
  ['rebuild-user', msgid('Rebuild user profile')],
  ['rebuild-web', msgid('Rebuild web domains')],
  ['rebuild-dns', msgid('Rebuild DNS zones')],
  ['rebuild-mail', msgid('Rebuild mail domains')],
  ['rebuild-db', msgid('Rebuild databases')],
  ['rebuild-cron', msgid('Rebuild cron jobs')],
  ['update-counters', msgid('Update usage counters')],
]

const statusFilters = { all: msgid('Show all'), active: msgid('Active'), suspended: msgid('Suspended') }

const sortLabels: Record<string, string> = {
  bandwidth: msgid('Bandwidth'),
  date: msgid('Date'),
  disk: msgid('Disk'),
  package: msgid('Package'),
  name: msgid('Name'),
}

const optionalColumns: ToggleableColumn[] = [
  { key: 'package', label: msgid('Package') },
  { key: 'bandwidth', label: msgid('Bandwidth') },
  { key: 'ips', label: msgid('IPs') },
  { key: 'objects', label: msgid('Web, DNS, mail and databases') },
  { key: 'backups', label: msgid('Backups') },
]

function SummaryTile({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <LayerCard className="grid gap-1 px-5 py-4">
      <Text variant="secondary" size="sm">
        {label}
      </Text>
      <Text variant="heading" size="lg" as="p" DANGEROUS_className="tabular-nums">
        {value}
      </Text>
      {hint && (
        <Text variant="secondary" size="sm">
          {hint}
        </Text>
      )}
    </LayerCard>
  )
}

function CountCell({ value }: { value: string }) {
  return (
    <Table.Cell className="text-right tabular-nums">
      {value === '0' ? <span className="text-kumo-subtle">0</span> : value}
    </Table.Cell>
  )
}

type Actions = {
  self: string
  root: string
  readOnly: boolean
  loginAs: (name: string) => void
  ask: (p: PendingConfirm) => void
  single: (name: string, action: 'suspend' | 'unsuspend' | 'delete') => Promise<unknown>
}

/** Each row action's msgids: dialog title, button label, Hestia's question and the done toast. */
const rowText = {
  suspend: {
    title: msgid('Suspend user'),
    label: msgid('Suspend'),
    ask: msgid('Are you sure you want to suspend user %s?'),
    done: msgid('%s suspended'),
  },
  unsuspend: {
    title: msgid('Unsuspend user'),
    label: msgid('Unsuspend'),
    ask: msgid('Are you sure you want to unsuspend user %s?'),
    done: msgid('%s unsuspended'),
  },
  delete: {
    title: msgid('Delete user'),
    label: msgid('Delete'),
    ask: msgid('Are you sure you want to delete user %s?'),
    done: msgid('%s has been deleted'),
  },
}

/** Bulk titles and the question for several users ("… suspend 2 users?"). */
const bulkText = {
  suspend: { titles: [msgid('Suspend user'), msgid('Suspend users')], ask: msgid('Are you sure you want to suspend %s?') },
  unsuspend: {
    titles: [msgid('Unsuspend user'), msgid('Unsuspend users')],
    ask: msgid('Are you sure you want to unsuspend %s?'),
  },
  delete: { titles: [msgid('Delete user'), msgid('Delete users')], ask: msgid('Are you sure you want to delete %s?') },
}

type RowAction = 'suspend' | 'unsuspend' | 'delete'

/** One user's suspend, unsuspend or delete question, shared by its menu and the row keys. */
function rowConfirm(row: Row, action: RowAction, a: Actions, t: Translate): PendingConfirm {
  return {
    title: t(rowText[action].title),
    description: t(rowText[action].ask, row.USER),
    confirmLabel: t(rowText[action].label),
    destructive: action !== 'unsuspend',
    done: t(rowText[action].done, row.USER),
    run: () => a.single(row.USER, action),
  }
}

function RowActions({ row, a }: { row: Row; a: Actions }) {
  const navigate = useNavigate()
  const t = useT()
  const suspended = row.SUSPENDED === 'yes'
  const isSelf = row.USER === a.self
  const isRoot = row.USER === a.root
  // Only root edits the root account (Parity 5).
  const canEdit = !isRoot || a.self === a.root
  const canChange = !isSelf && !isRoot && !a.readOnly
  const confirm = (action: RowAction) => a.ask(rowConfirm(row, action, a, t))
  return (
    <DropdownMenu>
      <DropdownMenu.Trigger render={rowMenuTrigger(t('Actions for %s', row.USER))} />
      <DropdownMenu.Content align="end">
        {!isSelf && (
          <DropdownMenu.Item icon={SignInIcon} onClick={() => a.loginAs(row.USER)}>
            {t('Log in as %s', row.USER)}
          </DropdownMenu.Item>
        )}
        {canEdit && (
          <DropdownMenu.Item icon={PencilSimpleIcon} onClick={() => navigate(isSelf ? '/account' : `/users/${row.USER}`)}>
            {t('Edit')}
          </DropdownMenu.Item>
        )}
        {canChange && (
          <>
            <DropdownMenu.Separator />
            <DropdownMenu.Item
              icon={suspended ? PlayCircleIcon : PauseCircleIcon}
              onClick={() => confirm(suspended ? 'unsuspend' : 'suspend')}
            >
              {suspended ? t('Unsuspend') : t('Suspend')}
            </DropdownMenu.Item>
            <DropdownMenu.Item icon={TrashIcon} variant="danger" onClick={() => confirm('delete')}>
              {t('Delete')}
            </DropdownMenu.Item>
          </>
        )}
      </DropdownMenu.Content>
    </DropdownMenu>
  )
}

export function UsersPage() {
  const navigate = useNavigate()
  const toast = useKumoToastManager()
  const t = useT()
  const me = useSessionUser()
  const readOnly = me?.readOnly ?? false
  const list = useUsers()
  const m = useUserMutation()
  const confirm = useConfirm()
  const [status, setStatus] = useState<keyof typeof statusFilters>('all')
  const [progress, setProgress] = useState<{ label: string; done: number; total: number } | null>(null)
  const columns = useColumnVisibility(
    'users',
    optionalColumns.map((c) => ({ ...c, label: t(c.label) })),
  )
  const show = columns.isVisible

  const all = useMemo<Row[]>(() => list.data?.users ?? [], [list.data])
  const self = list.data?.self ?? ''
  const root = list.data?.rootUser ?? ''
  const byStatus = useMemo(
    () => (status === 'all' ? all : all.filter((u) => (u.SUSPENDED === 'yes') === (status === 'suspended'))),
    [all, status],
  )
  const table = useDataTable({
    rows: byStatus,
    getId: (u) => u.USER,
    search: (u, q) => u.USER.toLowerCase().includes(q) || u.NAME.toLowerCase().includes(q) || u.CONTACT.toLowerCase().includes(q),
    sorts: {
      name: (u) => u.USER.toLowerCase(),
      date: (u) => `${u.DATE} ${u.TIME}`,
      package: (u) => u.PACKAGE.toLowerCase(),
      disk: (u) => Number(u.U_DISK),
      bandwidth: (u) => Number(u.U_BANDWIDTH),
    },
    // The API sends Hestia's order; open in the admin's own (PREF_UI_SORT).
    initialSort: me?.sortOrder === 'date' ? { key: 'date', dir: 'desc' } : { key: 'name', dir: 'asc' },
  })

  const totals = useMemo(() => {
    const sum = (k: keyof Row) => all.reduce((acc, u) => acc + (Number(u[k]) || 0), 0)
    return {
      users: all.length,
      suspended: all.filter((u) => u.SUSPENDED === 'yes').length,
      admins: all.filter((u) => u.ROLE === 'admin').length,
      disk: sum('U_DISK'),
      bandwidth: sum('U_BANDWIDTH'),
      web: sum('U_WEB_DOMAINS'),
      dns: sum('U_DNS_DOMAINS'),
      mail: sum('U_MAIL_DOMAINS'),
      db: sum('U_DATABASES'),
    }
  }, [all])

  const signInAs = useLoginAs()
  const loginAs = async (name: string) => {
    try {
      await signInAs(name)
    } catch (err) {
      toast.add({ title: t('Log in as %s', name), description: apiErrorText(err, t), type: 'error' })
    }
  }

  // One user per request for the heavy actions, with progress; the counters go in one.
  const run = async (action: UserBulkAction, names: string[], label: string): Promise<BulkReply> => {
    const failed: BulkReply['failed'] = []
    const batches = action === 'update-counters' ? [names] : names.map((n) => [n])
    setProgress({ label, done: 0, total: names.length })
    try {
      let done = 0
      for (const batch of batches) {
        try {
          failed.push(...(await m.bulk({ action, names: batch })).failed)
        } catch (err) {
          for (const name of batch) failed.push({ name, message: apiErrorText(err, t) })
        }
        done += batch.length
        setProgress({ label, done, total: names.length })
      }
    } finally {
      setProgress(null)
    }
    return { status: 'ok', failed }
  }

  const protectedName = (n: string) => n === self || n === root
  const selectedNames = () => table.selectedRows.map((u) => u.USER)
  const bulkConfirm = (action: 'suspend' | 'unsuspend' | 'delete') => {
    const label = t(rowText[action].label)
    // The server refuses ROOT_USER and the signed-in admin, so they're left out.
    const names = selectedNames().filter((n) => !protectedName(n))
    if (names.length === 0) {
      toast.add({ title: t('The signed-in admin and the root account can’t be suspended or deleted.'), type: 'error' })
      return
    }
    const one = names.length === 1
    const subject = one ? names[0] : t('%s users', names.length)
    confirm.ask({
      title: t(bulkText[action].titles[one ? 0 : 1]),
      description: one ? t(rowText[action].ask, names[0]) : t(bulkText[action].ask, subject),
      confirmLabel: label,
      destructive: action !== 'unsuspend',
      done: t('%s applied to %s', label, subject),
      onDone: table.clearSelection,
      run: () => run(action, names, label),
    })
  }
  const rebuild = async (action: UserBulkAction, label: string) => {
    const names = selectedNames()
    const r = await run(action, names, label)
    const subject = names.length === 1 ? names[0] : t('%s users', names.length)
    if (r.failed.length === 0) toast.add({ title: t('%s done for %s', label, subject), type: 'success' })
    else for (const f of r.failed) toast.add({ title: f.name, description: bulkMessage(f, t), type: 'error' })
    table.clearSelection()
  }

  const actions: Actions = {
    self,
    root,
    readOnly,
    loginAs,
    ask: confirm.ask,
    single: (name, action) => m[action === 'delete' ? 'remove' : action](name),
  }

  // Hestia's user list keys: Enter edit, s suspend, Delete; never on yourself or root.
  const keys = useRowShortcuts(table.rows, (u) => u.USER, (u) => {
    const isSelf = u.USER === self
    const isRoot = u.USER === root
    const canChange = !isSelf && !isRoot && !readOnly
    const ask = (action: RowAction) => () => confirm.ask(rowConfirm(u, action, actions, t))
    return {
      enter: !isRoot || self === root ? () => navigate(isSelf ? '/account' : `/users/${u.USER}`) : undefined,
      s: canChange ? ask(u.SUSPENDED === 'yes' ? 'unsuspend' : 'suspend') : undefined,
      delete: canChange ? ask('delete') : undefined,
    }
  })

  const sortKey = table.sort?.key ?? 'name'
  const sortHead = (key: string, label: string, align: 'left' | 'right' = 'left') => (
    <SortableHead sortKey={key} sort={table.sort} onSort={table.toggleSort} align={align}>
      {label}
    </SortableHead>
  )

  return (
    <div className="grid gap-6">
      <UsersAside />
      <PageHeader
        title={t('Users')}
        description={t('Accounts on this server, their packages and how much of it they use.')}
        actions={
          <>
            <LinkButton href="/packages" variant="secondary" icon={PackageIcon}>
              {t('Packages')}
            </LinkButton>
            {!readOnly && (
              <LinkButton href="/users/new" variant="primary" icon={PlusIcon} {...shortcutTarget('add')}>
                {t('Add user')}
              </LinkButton>
            )}
          </>
        }
      />

      {list.isPending ? (
        <DnsLoading />
      ) : list.isError ? (
        <DnsError error={list.error} />
      ) : (
        <>
          <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
            <SummaryTile
              label={t('Users')}
              value={String(totals.users)}
              hint={
                totals.admins === 1
                  ? t('1 administrator, %s suspended', totals.suspended)
                  : t('%s administrators, %s suspended', totals.admins, totals.suspended)
              }
            />
            <SummaryTile label={t('Disk used')} value={formatMB(String(totals.disk), 1)} hint={t('Across all users')} />
            <SummaryTile
              label={t('Bandwidth this month')}
              value={formatMB(String(totals.bandwidth), 1)}
              hint={t('Across all users')}
            />
            <SummaryTile
              label={t('Hosted objects')}
              value={String(totals.web + totals.dns + totals.mail + totals.db)}
              hint={t('%s web · %s DNS · %s mail · %s DB', totals.web, totals.dns, totals.mail, totals.db)}
            />
          </div>

          <div className="grid gap-3">
            {progress && (
              <div role="status" className="flex items-center gap-2 rounded-lg bg-kumo-tint px-3 py-2 ring ring-kumo-line">
                <Loader size="sm" />
                <Text as="span">{t('%s: %s of %s', progress.label, progress.done, progress.total)}</Text>
              </div>
            )}
            <TableToolbar
              search={{
                label: t('Search users'),
                placeholder: t('Search usernames, contact names or emails'),
                value: table.query,
                onChange: table.setQuery,
              }}
              filters={
                <>
                  <Select
                    aria-label={t('Filter by status')}
                    value={status}
                    onValueChange={(v) => setStatus(v as keyof typeof statusFilters)}
                    items={{ all: t(statusFilters.all), active: t(statusFilters.active), suspended: t(statusFilters.suspended) }}
                    render={<Toolbar.Button />}
                  />
                  <DropdownMenu>
                    <DropdownMenu.Trigger
                      render={
                        <Toolbar.Button icon={<SortAscendingIcon size={16} />}>
                          {t('Sort by: %s', t(sortLabels[sortKey] ?? sortLabels.name))}
                        </Toolbar.Button>
                      }
                    />
                    <DropdownMenu.Content align="end">
                      {Object.entries(sortLabels).map(([key, label]) => (
                        <DropdownMenu.Item key={key} onClick={() => table.toggleSort(key)}>
                          {t(label)}
                        </DropdownMenu.Item>
                      ))}
                    </DropdownMenu.Content>
                  </DropdownMenu>
                </>
              }
              selectedCount={readOnly ? 0 : table.selectedCount}
              onClearSelection={table.clearSelection}
              bulkActions={
                <>
                  <DropdownMenu>
                    <DropdownMenu.Trigger
                      render={
                        <Button size="sm" icon={ArrowsClockwiseIcon}>
                          {t('Rebuild')}
                          <CaretDownIcon size={12} />
                        </Button>
                      }
                    />
                    <DropdownMenu.Content align="end">
                      {BULK_REBUILDS.map(([key, label]) => (
                        <DropdownMenu.Item key={key} onClick={() => rebuild(key, t(label))}>
                          {t(label)}
                        </DropdownMenu.Item>
                      ))}
                    </DropdownMenu.Content>
                  </DropdownMenu>
                  <Button size="sm" icon={PauseCircleIcon} onClick={() => bulkConfirm('suspend')}>
                    {t('Suspend')}
                  </Button>
                  <Button size="sm" icon={PlayCircleIcon} onClick={() => bulkConfirm('unsuspend')}>
                    {t('Unsuspend')}
                  </Button>
                  <Button size="sm" variant="secondary-destructive" icon={TrashIcon} onClick={() => bulkConfirm('delete')}>
                    {t('Delete')}
                  </Button>
                </>
              }
            />

            <TableCard
              rowShortcuts={keys}
              noun={table.filteredCount === 1 ? t('user account') : t('user accounts')}
              filteredCount={table.filteredCount}
              totalCount={all.length}
              onClearSearch={() => {
                table.setQuery('')
                setStatus('all')
              }}
              emptyState={{
                icon: <UsersIcon size={40} className="text-kumo-inactive" />,
                title: t('No users yet'),
                description: t('Add a user to give someone their own hosting account.'),
                action: (
                  <LinkButton href="/users/new" variant="primary" icon={PlusIcon}>
                    {t('Add user')}
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
                        label={t('Select all users')}
                      />
                    )}
                    <SortableHead sortKey="name" sort={table.sort} onSort={table.toggleSort} sticky="left">
                      {t('User')}
                    </SortableHead>
                    {show('package') && sortHead('package', t('Package'))}
                    {sortHead('disk', t('Disk'))}
                    {show('bandwidth') && sortHead('bandwidth', t('Bandwidth'))}
                    {show('ips') && <IconHead icon={HardDrivesIcon} label={t('IP addresses')} />}
                    {show('objects') && (
                      <>
                        <IconHead icon={GlobeIcon} label={t('Web domains')} />
                        <IconHead icon={SignpostIcon} label={t('DNS zones')} />
                        <IconHead icon={EnvelopeSimpleIcon} label={t('Mail domains')} />
                        <IconHead icon={DatabaseIcon} label={t('Databases')} />
                      </>
                    )}
                    {show('backups') && <IconHead icon={ArchiveIcon} label={t('Backups')} />}
                    <Table.Head>{t('Status')}</Table.Head>
                    <Table.Head sticky="right" className="w-px text-right">
                      <ColumnsMenu visibility={columns} />
                    </Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {table.rows.map((u) => {
                    const isSelf = u.USER === self
                    // Only root opens the root account (Parity 5); a click does nothing for others.
                    const canEdit = u.USER !== root || self === root
                    return (
                      <Table.Row
                        key={u.USER}
                        variant={table.isSelected(u.USER) ? 'selected' : 'default'}
                        {...(canEdit ? rowLink(() => navigate(isSelf ? '/account' : `/users/${u.USER}`)) : {})}
                        {...keys.rowProps(u.USER)}
                      >
                        {!readOnly && (
                          <Table.CheckCell
                            checked={table.isSelected(u.USER)}
                            onCheckedChange={(_, details) => table.toggle(u.USER, details)}
                            label={t('Select %s', u.USER)}
                          />
                        )}
                        <Table.Cell sticky="left">
                          <div className="flex flex-col gap-0.5">
                            <div className="flex items-center gap-2">
                              {isSelf ? (
                                <RouterLink to="/account" className="font-medium text-kumo-default hover:underline">
                                  {u.USER}
                                </RouterLink>
                              ) : (
                                <button
                                  type="button"
                                  title={t('Log in as %s', u.USER)}
                                  onClick={() => loginAs(u.USER)}
                                  className="font-medium text-kumo-default hover:underline"
                                >
                                  {u.USER}
                                </button>
                              )}
                              {u.ROLE === 'admin' && <Badge variant="purple">{t('Admin')}</Badge>}
                              {u.ROLE === 'dns-cluster' && <Badge variant="teal">{t('DNS sync')}</Badge>}
                            </div>
                            <Text variant="secondary" size="sm" as="span" title={u.CONTACT}>
                              {u.NAME} · {u.CONTACT}
                            </Text>
                          </div>
                        </Table.Cell>
                        {show('package') && (
                          <Table.Cell>
                            {u.PACKAGE === 'system' ? (
                              u.PACKAGE
                            ) : (
                              <RouterLink to={`/packages/${u.PACKAGE}`} className="text-kumo-link hover:underline">
                                {u.PACKAGE}
                              </RouterLink>
                            )}
                          </Table.Cell>
                        )}
                        <Table.Cell>
                          <UsageMeter label={t('Disk')} used={u.U_DISK} quota={u.DISK_QUOTA} />
                        </Table.Cell>
                        {show('bandwidth') && (
                          <Table.Cell>
                            <UsageMeter label={t('Bandwidth')} used={u.U_BANDWIDTH} quota={u.BANDWIDTH} />
                          </Table.Cell>
                        )}
                        {show('ips') && <CountCell value={u.IP_OWNED} />}
                        {show('objects') && (
                          <>
                            <CountCell value={u.U_WEB_DOMAINS} />
                            <CountCell value={u.U_DNS_DOMAINS} />
                            <CountCell value={u.U_MAIL_DOMAINS} />
                            <CountCell value={u.U_DATABASES} />
                          </>
                        )}
                        {show('backups') && <CountCell value={u.U_BACKUPS} />}
                        <Table.Cell>
                          <StatusBadge suspended={u.SUSPENDED === 'yes'} />
                        </Table.Cell>
                        <Table.Cell sticky="right" className="text-right">
                          <RowActions row={u} a={actions} />
                        </Table.Cell>
                      </Table.Row>
                    )
                  })}
                </Table.Body>
              </Table>
            </TableCard>
          </div>
        </>
      )}

      <ListConfirmDialog confirm={confirm} />
    </div>
  )
}
