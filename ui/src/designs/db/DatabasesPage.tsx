import { Badge, Banner, Button, DropdownMenu, LinkButton, Select, Table, Text, Toolbar, Tooltip } from '@cloudflare/kumo'
import {
  ArrowSquareOutIcon,
  ArrowsClockwiseIcon,
  DatabaseIcon,
  DownloadSimpleIcon,
  InfoIcon,
  PauseCircleIcon,
  PencilSimpleIcon,
  PlayCircleIcon,
  PlusIcon,
  SignInIcon,
  TrashIcon,
} from '@phosphor-icons/react'
import { useMemo, useState } from 'react'
import { Link as RouterLink, useNavigate } from 'react-router'
import { downloadUrl, phpMyAdminSsoUrl, useDatabases, useDbMutation, useDbOptions } from '../../api/db'
import type { Database } from '../../api/hestia.gen'
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
import { ListConfirmDialog, ResourceDeleteDialog, useConfirm, useResourceDelete } from '../../components/useListConfirm'
import type { PendingConfirm } from '../../components/useListConfirm'
import { formatMB } from '../../lib/format'
import { useT, type Translate } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { useSessionUser } from '../../session/SessionProvider'
import { DnsError, DnsLoading } from '../dns/shared'
import { dbAdminName } from './dbHelpers'
import { DbAside } from './DbAside'
import { shortcutTarget } from '../../shortcuts/targets'
import { useRowShortcuts } from '../../shortcuts/useRowShortcuts'

const typeFilters = { all: msgid('All types'), mysql: 'MySQL', pgsql: 'PostgreSQL' }
const statusFilters = { all: msgid('Show all'), active: msgid('Active'), suspended: msgid('Suspended') }

const optionalColumns: ToggleableColumn[] = [
  { key: 'host', label: msgid('Hostname') },
  { key: 'charset', label: msgid('Charset') },
  { key: 'created', label: msgid('Created') },
]

export function TypeBadge({ type }: { type: Database['TYPE'] }) {
  return type === 'mysql' ? <Badge variant="orange">MySQL</Badge> : <Badge variant="blue">PostgreSQL</Badge>
}

/** The suspend or unsuspend question for a database, shared by its menu and the s key. */
function suspendConfirm(
  db: Database,
  t: Translate,
  run: (name: string, suspended: boolean) => Promise<unknown>,
): PendingConfirm {
  const name = db.DATABASE
  const suspended = db.SUSPENDED === 'yes'
  return {
    title: suspended ? t('Unsuspend database') : t('Suspend database'),
    description: suspended
      ? t('Are you sure you want to unsuspend database %s?', name)
      : t('Are you sure you want to suspend database %s?', name),
    confirmLabel: suspended ? t('Unsuspend') : t('Suspend'),
    destructive: !suspended,
    done: suspended ? t('%s unsuspended', name) : t('%s suspended', name),
    run: () => run(name, suspended),
  }
}

function RowActions({
  db,
  ask,
  onDelete,
  readOnly,
  isAdmin,
  run,
}: {
  db: Database
  ask: (p: PendingConfirm) => void
  onDelete: (name: string) => void
  readOnly: boolean
  /** Suspend and unsuspend are admin-only on the server. */
  isAdmin: boolean
  run: (name: string, suspended: boolean) => Promise<unknown>
}) {
  const navigate = useNavigate()
  const t = useT()
  const suspended = db.SUSPENDED === 'yes'
  const name = db.DATABASE
  return (
    <DropdownMenu>
      <DropdownMenu.Trigger render={rowMenuTrigger(t('Actions for %s', name))} />
      <DropdownMenu.Content align="end">
        {!readOnly && !suspended && (
          <DropdownMenu.Item icon={PencilSimpleIcon} onClick={() => navigate(`/db/${name}`)}>
            {t('Edit database')}
          </DropdownMenu.Item>
        )}
        {/* A plain link: the browser receives the file itself */}
        <DropdownMenu.LinkItem icon={DownloadSimpleIcon} href={downloadUrl(name)}>
          {t('Download database')}
        </DropdownMenu.LinkItem>
        {!readOnly && (
          <>
            <DropdownMenu.Separator />
            {isAdmin && (
              <DropdownMenu.Item
                icon={suspended ? PlayCircleIcon : PauseCircleIcon}
                onClick={() => ask(suspendConfirm(db, t, run))}
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
  rebuild: { title: msgid('Rebuild databases'), label: msgid('Rebuild'), ask: msgid('Are you sure you want to rebuild %s?') },
  suspend: { title: msgid('Suspend databases'), label: msgid('Suspend'), ask: msgid('Are you sure you want to suspend %s?') },
  unsuspend: {
    title: msgid('Unsuspend databases'),
    label: msgid('Unsuspend'),
    ask: msgid('Are you sure you want to unsuspend %s?'),
  },
  delete: { title: msgid('Delete databases'), label: msgid('Delete'), ask: msgid('Are you sure you want to delete %s?') },
}

export function DatabasesPage() {
  const navigate = useNavigate()
  const t = useT()
  const user = useSessionUser()
  const readOnly = user?.readOnly ?? false
  // The signed-in role, even while looking at another user
  const isAdmin = user?.role === 'admin'
  const list = useDatabases()
  const options = useDbOptions().data
  const m = useDbMutation()
  const [type, setType] = useState<keyof typeof typeFilters>('all')
  const [status, setStatus] = useState<keyof typeof statusFilters>('all')
  const confirm = useConfirm()
  const del = useResourceDelete()
  const columns = useColumnVisibility(
    'databases',
    optionalColumns.map((c) => ({ ...c, label: t(c.label) })),
  )
  const show = columns.isVisible
  const myAdminUrl = options?.phpMyAdminUrl ?? ''
  const pgAdminUrl = options?.phpPgAdminUrl ?? ''
  const ssoHint = isAdmin ? (options?.ssoHint ?? '') : ''
  const showTypeFilter = (options?.types.length ?? 0) > 1

  const all = useMemo<Database[]>(() => list.data?.databases ?? [], [list.data])
  const filtered = useMemo(
    () =>
      all.filter(
        (d) =>
          (type === 'all' || d.TYPE === type) &&
          (status === 'all' || (d.SUSPENDED === 'yes') === (status === 'suspended')),
      ),
    [all, type, status],
  )
  const table = useDataTable({
    rows: filtered,
    getId: (d) => d.DATABASE,
    search: (d, q) =>
      d.DATABASE.toLowerCase().includes(q) || d.DBUSER.toLowerCase().includes(q) || d.HOST.toLowerCase().includes(q),
    sorts: {
      name: (d) => d.DATABASE,
      user: (d) => d.DBUSER,
      host: (d) => d.HOST,
      charset: (d) => d.CHARSET.toLowerCase(),
      disk: (d) => Number(d.U_DISK),
      created: (d) => `${d.DATE} ${d.TIME}`,
    },
    // The API sends Hestia's file order; open in the user's own order (PREF_UI_SORT).
    initialSort: user?.sortOrder === 'date' ? { key: 'created', dir: 'desc' } : { key: 'name', dir: 'asc' },
  })

  const suspendRun = (name: string, wasSuspended: boolean) => (wasSuspended ? m.unsuspendDb(name) : m.suspendDb(name))
  // Hestia's database list keys: Enter edit, s suspend, Delete.
  const keys = useRowShortcuts(table.rows, (d) => d.DATABASE, (d) => ({
    enter: d.SUSPENDED === 'yes' ? undefined : () => navigate(`/db/${d.DATABASE}`),
    ...(!readOnly && {
      s: isAdmin ? () => confirm.ask(suspendConfirm(d, t, suspendRun)) : undefined,
      delete: () => del.ask(d.DATABASE),
    }),
  }))

  const bulk = (action: keyof typeof bulkText, destructive = false) => {
    const n = table.selectedCount
    const subject = n === 1 ? table.selectedRows[0].DATABASE : t('%s databases', n)
    const text = bulkText[action]
    confirm.ask({
      title: t(text.title),
      description: t(text.ask, subject),
      confirmLabel: t(text.label),
      destructive,
      done: t('%s applied to %s', t(text.label), subject),
      onDone: table.clearSelection,
      run: () => m.bulkDb({ action, names: table.selectedRows.map((d) => d.DATABASE) }),
    })
  }

  return (
    <div className="grid gap-6">
      <DbAside />
      <PageHeader
        title={t('Databases')}
        description={t('MySQL and PostgreSQL databases, their users and the servers they live on.')}
        actions={
          !readOnly && (
            <>
              {myAdminUrl !== '' && (
                <LinkButton href={myAdminUrl} external variant="secondary" icon={ArrowSquareOutIcon}>
                  phpMyAdmin
                </LinkButton>
              )}
              {pgAdminUrl !== '' && (
                <LinkButton href={pgAdminUrl} external variant="secondary" icon={ArrowSquareOutIcon}>
                  phpPgAdmin
                </LinkButton>
              )}
              <LinkButton href="/db/new" variant="primary" icon={PlusIcon} {...shortcutTarget('add')}>
                {t('Add database')}
              </LinkButton>
            </>
          )
        }
      />

      {ssoHint !== '' && <Banner variant="default" icon={<InfoIcon />} description={t(ssoHint)} />}

      <div className="grid gap-3">
        <TableToolbar
          search={{
            label: t('Search databases'),
            placeholder: t('Search databases, users or hosts'),
            value: table.query,
            onChange: table.setQuery,
          }}
          filters={
            <>
              {showTypeFilter && (
                <Select
                  aria-label={t('Filter by type')}
                  value={type}
                  onValueChange={(v) => setType(v as keyof typeof typeFilters)}
                  items={{ all: t(typeFilters.all), mysql: typeFilters.mysql, pgsql: typeFilters.pgsql }}
                  render={<Toolbar.Button />}
                />
              )}
              <Select
                aria-label={t('Filter by status')}
                value={status}
                onValueChange={(v) => setStatus(v as keyof typeof statusFilters)}
                items={{ all: t(statusFilters.all), active: t(statusFilters.active), suspended: t(statusFilters.suspended) }}
                render={<Toolbar.Button />}
              />
            </>
          }
          selectedCount={readOnly ? 0 : table.selectedCount}
          onClearSelection={table.clearSelection}
          bulkActions={
            <>
              {isAdmin && (
                <>
                  <Button size="sm" icon={ArrowsClockwiseIcon} onClick={() => bulk('rebuild')}>
                    {t('Rebuild')}
                  </Button>
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

        {list.isPending ? (
          <DnsLoading />
        ) : list.isError ? (
          <DnsError error={list.error} />
        ) : (
          <TableCard
            rowShortcuts={keys}
            noun={{ one: t('database'), other: t('databases') }}
            filteredCount={table.filteredCount}
            totalCount={all.length}
            onClearSearch={() => {
              table.setQuery('')
              setType('all')
              setStatus('all')
            }}
            emptyState={{
              icon: <DatabaseIcon size={40} className="text-kumo-inactive" />,
              title: t('No databases yet'),
              description: t('Create a MySQL or PostgreSQL database for your applications.'),
              action: !readOnly && (
                <LinkButton href="/db/new" variant="primary" icon={PlusIcon}>
                  {t('Add database')}
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
                      label={t('Select all databases')}
                    />
                  )}
                  <SortableHead sortKey="name" sort={table.sort} onSort={table.toggleSort} sticky="left">
                    {t('Name')}
                  </SortableHead>
                  <Table.Head>{t('Type')}</Table.Head>
                  <SortableHead sortKey="user" sort={table.sort} onSort={table.toggleSort}>
                    {t('Username')}
                  </SortableHead>
                  {show('host') && (
                    <SortableHead sortKey="host" sort={table.sort} onSort={table.toggleSort}>
                      {t('Hostname')}
                    </SortableHead>
                  )}
                  {show('charset') && (
                    <SortableHead sortKey="charset" sort={table.sort} onSort={table.toggleSort}>
                      {t('Charset')}
                    </SortableHead>
                  )}
                  <SortableHead sortKey="disk" sort={table.sort} onSort={table.toggleSort} align="right">
                    {t('Disk')}
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
                {table.rows.map((d) => {
                  const suspended = d.SUSPENDED === 'yes'
                  return (
                    <Table.Row
                      key={d.DATABASE}
                      variant={table.isSelected(d.DATABASE) ? 'selected' : 'default'}
                      {...(suspended ? {} : rowLink(() => navigate(`/db/${d.DATABASE}`)))}
                      {...keys.rowProps(d.DATABASE)}
                    >
                      {!readOnly && (
                        <Table.CheckCell
                          checked={table.isSelected(d.DATABASE)}
                          onCheckedChange={(_, details) => table.toggle(d.DATABASE, details)}
                          label={t('Select %s', d.DATABASE)}
                        />
                      )}
                      <Table.Cell sticky="left">
                        {suspended ? (
                          <Text bold as="span">
                            {d.DATABASE}
                          </Text>
                        ) : (
                          <RouterLink
                            to={`/db/${d.DATABASE}`}
                            className="font-medium text-kumo-default hover:underline"
                          >
                            {d.DATABASE}
                          </RouterLink>
                        )}
                      </Table.Cell>
                      <Table.Cell>
                        <TypeBadge type={d.TYPE} />
                      </Table.Cell>
                      <Table.Cell>
                        <Text variant="mono" as="span">
                          {d.DBUSER}
                        </Text>
                      </Table.Cell>
                      {show('host') && <Table.Cell>{d.HOST}</Table.Cell>}
                      {show('charset') && <Table.Cell>{d.CHARSET}</Table.Cell>}
                      <Table.Cell className="text-right tabular-nums">{formatMB(d.U_DISK)}</Table.Cell>
                      {show('created') && <Table.Cell className="tabular-nums text-kumo-subtle">{d.DATE}</Table.Cell>}
                      <Table.Cell>
                        <StatusBadge suspended={suspended} />
                      </Table.Cell>
                      <Table.Cell sticky="right" className="text-right">
                        <div className="flex items-center justify-end gap-1">
                          {/* A plain link, built by the server at click time (Parity 7) */}
                          {!readOnly && options?.ssoEnabled && d.TYPE === 'mysql' && (
                            <Tooltip
                              content={t('Open in %s', dbAdminName(d.TYPE))}
                              render={
                                <LinkButton
                                  href={phpMyAdminSsoUrl(d.DATABASE)}
                                  external
                                  target="_blank"
                                  rel="noopener"
                                  variant="ghost"
                                  size="sm"
                                  shape="square"
                                  icon={<SignInIcon size={18} />}
                                  aria-label={t('Open %s in %s', d.DATABASE, dbAdminName(d.TYPE))}
                                />
                              }
                            />
                          )}
                          <RowActions
                            db={d}
                            ask={confirm.ask}
                            onDelete={del.ask}
                            readOnly={readOnly}
                            isAdmin={isAdmin}
                            run={suspendRun}
                          />
                        </div>
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
      <ResourceDeleteDialog state={del} resourceType={t('database')} onDelete={m.deleteDb} />
    </div>
  )
}
