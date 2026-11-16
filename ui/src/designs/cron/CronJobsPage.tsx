import { Button, DropdownMenu, LinkButton, Select, Table, Text, Toolbar, Tooltip, useKumoToastManager } from '@cloudflare/kumo'
import {
  BellIcon,
  BellSlashIcon,
  ClockIcon,
  PauseCircleIcon,
  PencilSimpleIcon,
  PlayCircleIcon,
  PlusIcon,
  TrashIcon,
} from '@phosphor-icons/react'
import { useMemo, useState } from 'react'
import { Link as RouterLink, useNavigate } from 'react-router'
import { useCronJobs, useCronMutation } from '../../api/cron'
import { apiErrorText } from '../../api/errors'
import type { CronJob } from '../../api/hestia.gen'
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
import { ListConfirmDialog, useConfirm } from '../../components/useListConfirm'
import { useT } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { useSessionUser } from '../../session/SessionProvider'
import { DnsError, DnsLoading } from '../dns/shared'
import { CronAside } from './CronAside'
import { describeCron } from './describeCron'
import { shortcutTarget } from '../../shortcuts/targets'
import { useRowShortcuts } from '../../shortcuts/useRowShortcuts'

type Kind = 'delete' | 'suspend' | 'unsuspend'

const statusFilters = { all: msgid('Show all'), active: msgid('Active'), suspended: msgid('Suspended') }

const optionalColumns: ToggleableColumn[] = [
  { key: 'expression', label: msgid('Expression') },
  { key: 'created', label: msgid('Created') },
]

const confirmCopy: Record<Kind, { title: string; one: string; many: string; done: string }> = {
  delete: {
    title: msgid('Delete'),
    one: msgid('Are you sure you want to delete this cron job?'),
    many: msgid('Are you sure you want to delete %d cron jobs?'),
    done: msgid('Cron job deleted'),
  },
  suspend: {
    title: msgid('Suspend'),
    one: msgid('Are you sure you want to suspend this cron job?'),
    many: msgid('Are you sure you want to suspend %d cron jobs?'),
    done: msgid('Cron job suspended'),
  },
  unsuspend: {
    title: msgid('Unsuspend'),
    one: msgid('Are you sure you want to unsuspend this cron job?'),
    many: msgid('Are you sure you want to unsuspend %d cron jobs?'),
    done: msgid('Cron job unsuspended'),
  },
}

function RowActions({
  job,
  onAction,
  isAdmin,
}: {
  job: CronJob
  onAction: (kind: Kind) => void
  /** Suspend and unsuspend are admin-only on the server. */
  isAdmin: boolean
}) {
  const navigate = useNavigate()
  const t = useT()
  const suspended = job.SUSPENDED === 'yes'
  return (
    <DropdownMenu>
      <DropdownMenu.Trigger render={rowMenuTrigger(t('Actions for job %s', job.JOB))} />
      <DropdownMenu.Content align="end">
        {!suspended && (
          <DropdownMenu.Item icon={PencilSimpleIcon} onClick={() => navigate(`/cron/${job.JOB}`)}>
            {t('Edit')}
          </DropdownMenu.Item>
        )}
        {isAdmin && (
          <DropdownMenu.Item
            icon={suspended ? PlayCircleIcon : PauseCircleIcon}
            onClick={() => onAction(suspended ? 'unsuspend' : 'suspend')}
          >
            {suspended ? t('Unsuspend') : t('Suspend')}
          </DropdownMenu.Item>
        )}
        {(isAdmin || !suspended) && <DropdownMenu.Separator />}
        <DropdownMenu.Item icon={TrashIcon} variant="danger" onClick={() => onAction('delete')}>
          {t('Delete')}
        </DropdownMenu.Item>
      </DropdownMenu.Content>
    </DropdownMenu>
  )
}

function Field({ value }: { value: string }) {
  return (
    <Text variant="mono" as="span">
      {value}
    </Text>
  )
}

export function CronJobsPage() {
  const t = useT()
  const toast = useKumoToastManager()
  const navigate = useNavigate()
  const user = useSessionUser()
  const readOnly = user?.readOnly ?? false
  // The signed-in role, even while looking at another user
  const isAdmin = user?.role === 'admin'
  const list = useCronJobs()
  const m = useCronMutation()
  const confirm = useConfirm()
  const [status, setStatus] = useState<keyof typeof statusFilters>('all')
  const [savingReports, setSavingReports] = useState(false)
  const columns = useColumnVisibility(
    'cron-jobs',
    optionalColumns.map((c) => ({ ...c, label: t(c.label) })),
  )
  const show = columns.isVisible
  const reports = list.data?.reports ?? false

  const all = useMemo<CronJob[]>(() => list.data?.jobs ?? [], [list.data])
  const byStatus = useMemo(
    () => (status === 'all' ? all : all.filter((j) => (j.SUSPENDED === 'yes') === (status === 'suspended'))),
    [all, status],
  )
  const table = useDataTable({
    rows: byStatus,
    getId: (j) => j.JOB,
    search: (j, q) => j.CMD.toLowerCase().includes(q),
    sorts: {
      command: (j) => j.CMD,
      // Hestia's date order; the job ID breaks ties between jobs created the same day.
      created: (j) => `${j.DATE} ${j.TIME} ${j.JOB.padStart(6, '0')}`,
    },
    // Open in the saved list order (PREF_UI_SORT): Command A-Z for "name", newest first for "date".
    initialSort: user?.sortOrder === 'name' ? { key: 'command', dir: 'asc' } : { key: 'created', dir: 'desc' },
  })

  const ask = (kind: Kind, ids: string[], one?: () => Promise<unknown>) => {
    const copy = confirmCopy[kind]
    confirm.ask({
      title: t(copy.title),
      description: ids.length > 1 ? t(copy.many, ids.length) : t(copy.one),
      confirmLabel: t(copy.title),
      destructive: kind === 'delete',
      done: t(copy.done),
      onDone: table.clearSelection,
      run: one ?? (() => m.bulk({ action: kind, names: ids })),
    })
  }
  const selectedIds = () => table.selectedRows.map((j) => j.JOB)

  /** One job's suspend, unsuspend or delete, asked first; shared by its menu and the row keys. */
  const rowAction = (j: CronJob, kind: Kind) =>
    ask(kind, [j.JOB], () =>
      kind === 'delete' ? m.remove(j.JOB) : kind === 'suspend' ? m.suspend(j.JOB) : m.unsuspend(j.JOB),
    )
  // Hestia's cron list keys: Enter edit, s suspend, Delete.
  const keys = useRowShortcuts(table.rows, (j) => j.JOB, (j) => {
    const suspended = j.SUSPENDED === 'yes'
    if (readOnly) return {}
    return {
      enter: suspended ? undefined : () => navigate(`/cron/${j.JOB}`),
      s: isAdmin ? () => rowAction(j, suspended ? 'unsuspend' : 'suspend') : undefined,
      delete: () => rowAction(j, 'delete'),
    }
  })

  const setNotifications = async (on: boolean) => {
    setSavingReports(true)
    try {
      await m.setReports(on)
      toast.add({
        title: t(
          on
            ? 'Cron job email reporting has been successfully enabled.'
            : 'Cron job email reporting has been successfully disabled.',
        ),
        type: 'success',
      })
    } catch (err) {
      toast.add({ title: t('Cron notifications'), description: apiErrorText(err, t), type: 'error' })
    } finally {
      setSavingReports(false)
    }
  }

  return (
    <div className="grid gap-6">
      <CronAside data={list.data} onReportsChange={setNotifications} busy={savingReports} />
      <PageHeader
        title={t('Cron jobs')}
        description={t('Commands that run on a schedule under this account.')}
        actions={
          !readOnly && (
            <LinkButton href="/cron/new" variant="primary" icon={PlusIcon} {...shortcutTarget('add')}>
              {t('Add cron job')}
            </LinkButton>
          )
        }
      />

      <div className="grid gap-3">
        <TableToolbar
          search={{
            label: t('Search cron jobs'),
            placeholder: t('Search commands'),
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
            </>
          }
          selectedCount={readOnly ? 0 : table.selectedCount}
          onClearSelection={table.clearSelection}
          bulkActions={
            <>
              <Button
                size="sm"
                icon={reports ? BellSlashIcon : BellIcon}
                disabled={savingReports}
                onClick={() => {
                  void setNotifications(!reports)
                  table.clearSelection()
                }}
              >
                {reports ? t('Disable notifications') : t('Enable notifications')}
              </Button>
              {isAdmin && (
                <>
                  <Button size="sm" icon={PauseCircleIcon} onClick={() => ask('suspend', selectedIds())}>
                    {t('Suspend')}
                  </Button>
                  <Button size="sm" icon={PlayCircleIcon} onClick={() => ask('unsuspend', selectedIds())}>
                    {t('Unsuspend')}
                  </Button>
                </>
              )}
              <Button size="sm" variant="secondary-destructive" icon={TrashIcon} onClick={() => ask('delete', selectedIds())}>
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
            noun={{ one: t('cron job'), other: t('cron jobs') }}
            filteredCount={table.filteredCount}
            totalCount={all.length}
            onClearSearch={() => {
              table.setQuery('')
              setStatus('all')
            }}
            emptyState={{
              icon: <ClockIcon size={40} className="text-kumo-inactive" />,
              title: t('No cron jobs yet'),
              description: t('Schedule a command to run every few minutes, daily, weekly or monthly.'),
              action: !readOnly && (
                <LinkButton href="/cron/new" variant="primary" icon={PlusIcon}>
                  {t('Add cron job')}
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
                      label={t('Select all cron jobs')}
                    />
                  )}
                  <SortableHead sortKey="command" sort={table.sort} onSort={table.toggleSort} sticky="left">
                    {t('Command')}
                  </SortableHead>
                  <Table.Head>{t('Schedule')}</Table.Head>
                  {show('expression') && (
                    <Table.Head>
                      <Tooltip content={t('Minute, hour, day, month, day of week')}>
                        <span>{t('Expression')}</span>
                      </Tooltip>
                    </Table.Head>
                  )}
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
                {table.rows.map((j) => {
                  const suspended = j.SUSPENDED === 'yes'
                  const editable = !suspended && !readOnly
                  return (
                    <Table.Row
                      key={j.JOB}
                      variant={table.isSelected(j.JOB) ? 'selected' : 'default'}
                      {...(editable ? rowLink(() => navigate(`/cron/${j.JOB}`)) : {})}
                      {...keys.rowProps(j.JOB)}
                    >
                      {!readOnly && (
                        <Table.CheckCell
                          checked={table.isSelected(j.JOB)}
                          onCheckedChange={(_, details) => table.toggle(j.JOB, details)}
                          label={t('Select job %s', j.JOB)}
                        />
                      )}
                      <Table.Cell sticky="left">
                        <div className="max-w-[12rem] truncate font-mono text-[0.9em] sm:max-w-[20rem]" title={j.CMD}>
                          {editable ? (
                            <RouterLink to={`/cron/${j.JOB}`} className="text-kumo-default hover:underline">
                              {j.CMD}
                            </RouterLink>
                          ) : (
                            j.CMD
                          )}
                        </div>
                      </Table.Cell>
                      <Table.Cell>{describeCron(j.MIN, j.HOUR, j.DAY, j.MONTH, j.WDAY, t)}</Table.Cell>
                      {show('expression') && (
                        <Table.Cell>
                          <Field value={[j.MIN, j.HOUR, j.DAY, j.MONTH, j.WDAY].join(' ')} />
                        </Table.Cell>
                      )}
                      {show('created') && <Table.Cell className="tabular-nums text-kumo-subtle">{j.DATE}</Table.Cell>}
                      <Table.Cell>
                        <StatusBadge suspended={suspended} />
                      </Table.Cell>
                      <Table.Cell sticky="right" className="text-right">
                        {!readOnly && (
                          <RowActions
                            job={j}
                            isAdmin={isAdmin}
                            onAction={(kind) => rowAction(j, kind)}
                          />
                        )}
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
    </div>
  )
}
