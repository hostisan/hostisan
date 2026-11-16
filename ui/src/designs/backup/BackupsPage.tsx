import { Badge, Button, DropdownMenu, Select, Table, Toolbar, Tooltip, useKumoToastManager } from '@cloudflare/kumo'
import { ArchiveIcon, ArrowCounterClockwiseIcon, DownloadSimpleIcon, PlusIcon, TrashIcon } from '@phosphor-icons/react'
import { useMemo, useState } from 'react'
import { useNavigate, Link as RouterLink } from 'react-router'
import { useBackupMutation, useBackups } from '../../api/backup'
import { apiErrorText } from '../../api/errors'
import type { Backup } from '../../api/hestia.gen'
import { PageHeader } from '../../components/PageHeader'
import { SortableHead, TableCard, TableToolbar, rowLink, rowMenuTrigger, useDataTable } from '../../components/table'
import { ListConfirmDialog, useConfirm } from '../../components/useListConfirm'
import { useT } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { translateDate } from '../../lib/date'
import { useSessionUser } from '../../session/SessionProvider'
import { DnsError, DnsLoading } from '../dns/shared'
import {
  BackupTabs,
  formatBackupSize,
  formatRuntime,
  useBackupDownload,
  useRefetchOnFocus,
} from './BackupTabs'
import { shortcutTarget } from '../../shortcuts/targets'
import { useRowShortcuts } from '../../shortcuts/useRowShortcuts'

const contentKeys: { key: 'WEB' | 'DNS' | 'MAIL' | 'DB' | 'CRON' | 'UDIR'; label: string }[] = [
  { key: 'WEB', label: msgid('Web') },
  { key: 'DNS', label: 'DNS' },
  { key: 'MAIL', label: msgid('Mail') },
  { key: 'DB', label: msgid('Databases') },
  { key: 'CRON', label: msgid('Cron') },
  { key: 'UDIR', label: msgid('User directories') },
]

function Contents({ backup }: { backup: Backup }) {
  const t = useT()
  const included = contentKeys.filter(({ key }) => backup[key] && (key !== 'CRON' || backup.CRON !== '0'))
  return (
    <div className="flex gap-1">
      {included.map(({ key, label: id }) => {
        const label = t(id)
        const count = backup[key].split(',').filter(Boolean).length
        return (
          <Tooltip key={key} content={count ? `${label}: ${count}` : label}>
            <span>
              <Badge variant="neutral">
                {key === 'UDIR' ? t('Dirs') : key === 'DB' ? t('DB') : label}
                {count > 0 && <span className="ml-1 tabular-nums opacity-70">{count}</span>}
              </Badge>
            </span>
          </Tooltip>
        )
      })}
    </div>
  )
}

export function BackupsPage() {
  const toast = useKumoToastManager()
  const t = useT()
  const navigate = useNavigate()
  const user = useSessionUser()
  const readOnly = user?.readOnly ?? false
  const list = useBackups()
  const m = useBackupMutation()
  const download = useBackupDownload()
  const confirm = useConfirm()
  const [type, setType] = useState('all')
  useRefetchOnFocus(list.refetch)

  const all = useMemo<Backup[]>(() => list.data?.backups ?? [], [list.data])
  const types = useMemo(() => [...new Set(all.map((b) => b.TYPE))], [all])
  const typeFilters = { all: t('All locations'), ...Object.fromEntries(types.map((x) => [x, x])) }
  const byType = useMemo(() => (type === 'all' ? all : all.filter((b) => b.TYPE === type)), [all, type])
  const table = useDataTable({
    rows: byType,
    getId: (b) => b.NAME,
    search: (b, q) => b.NAME.includes(q) || b.TYPE.includes(q) || b.DATE.includes(q),
    sorts: {
      name: (b) => b.NAME,
      date: (b) => `${b.DATE} ${b.TIME}`,
      size: (b) => Number(b.SIZE),
      runtime: (b) => Number(b.RUNTIME),
    },
    // The API sends newest first; open in the user's own order (PREF_UI_SORT).
    initialSort: user?.sortOrder === 'name' ? { key: 'name', dir: 'asc' } : { key: 'date', dir: 'desc' },
  })

  const askDelete = (names: string[]) =>
    confirm.ask({
      title: t('Delete'),
      description:
        names.length === 1
          ? t('Are you sure you want to delete backup %s?', names[0])
          : t('Are you sure you want to delete %s?', t('%s backups', names.length)),
      confirmLabel: t('Delete'),
      destructive: true,
      done: names.length === 1 ? t('Backup deleted') : t('Backups deleted'),
      onDone: table.clearSelection,
      run: () => (names.length === 1 ? m.remove(names[0]) : m.bulk({ action: 'delete', names })),
    })

  // The job runs later and finishes by email and in the notification tray (no live status).
  const createBackup = () =>
    m.schedule().then(
      (r) => toast.add({ title: t('Backup scheduled'), description: t(r.message), type: 'success' }),
      (err) => toast.add({ title: t('Backup not scheduled'), description: apiErrorText(err, t), type: 'error' }),
    )
  const detail = (name: string) => `/backup/${encodeURIComponent(name)}`

  // Hestia's backup list keys: d download, Enter restore, Delete.
  const keys = useRowShortcuts(table.rows, (b) => b.NAME, (b) => ({
    d: () => download(b.NAME),
    ...(!readOnly && {
      enter: () => navigate(detail(b.NAME)),
      delete: () => askDelete([b.NAME]),
    }),
  }))

  return (
    <div className="grid gap-6">
      <PageHeader
        title={t('Backups')}
        description={
          list.data && Number(list.data.usage.limit) > 0
            ? t('Full account backups. Keeps the latest %s backups; older ones are rotated out.', list.data.usage.limit)
            : t('Full account backups.')
        }
        actions={
          !readOnly && (
            <Button variant="primary" icon={PlusIcon} {...shortcutTarget('add')} onClick={createBackup}>
              {t('Create backup')}
            </Button>
          )
        }
      />
      <BackupTabs value="/backup" />

      <div className="grid gap-3">
        <TableToolbar
          // Backups created with "Create backup" are queued and appear here once they finish.
          onRefresh={() => list.refetch().then(() => undefined)}
          search={{
            label: t('Search backups'),
            placeholder: t('Search by file name or date'),
            value: table.query,
            onChange: table.setQuery,
          }}
          filters={
            types.length > 1 && (
              <Select
                aria-label={t('Filter by location')}
                value={type}
                onValueChange={(v) => setType(v as string)}
                items={typeFilters}
                render={<Toolbar.Button />}
              />
            )
          }
          selectedCount={readOnly ? 0 : table.selectedCount}
          onClearSelection={table.clearSelection}
          bulkActions={
            <Button
              size="sm"
              variant="secondary-destructive"
              icon={TrashIcon}
              onClick={() => askDelete(table.selectedRows.map((b) => b.NAME))}
            >
              {t('Delete')}
            </Button>
          }
        />

        {list.isPending ? (
          <DnsLoading />
        ) : list.isError ? (
          <DnsError error={list.error} />
        ) : (
          <TableCard
            rowShortcuts={keys}
            noun={{ one: t('backup'), other: t('backups') }}
            filteredCount={table.filteredCount}
            totalCount={all.length}
            onClearSearch={() => {
              table.setQuery('')
              setType('all')
            }}
            emptyState={{
              icon: <ArchiveIcon size={40} className="text-kumo-inactive" />,
              title: t('No backups yet'),
              description: t('Create a backup to get a downloadable archive of this account.'),
              action: !readOnly && (
                <Button variant="primary" icon={PlusIcon} onClick={createBackup}>
                  {t('Create backup')}
                </Button>
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
                      label={t('Select all backups')}
                    />
                  )}
                  <SortableHead sortKey="name" sort={table.sort} onSort={table.toggleSort} sticky="left">
                    {t('File name')}
                  </SortableHead>
                  <SortableHead sortKey="date" sort={table.sort} onSort={table.toggleSort}>
                    {t('Date')}
                  </SortableHead>
                  <SortableHead sortKey="size" sort={table.sort} onSort={table.toggleSort} align="right">
                    {t('Size')}
                  </SortableHead>
                  <Table.Head>{t('Type')}</Table.Head>
                  <SortableHead sortKey="runtime" sort={table.sort} onSort={table.toggleSort} align="right">
                    {t('Runtime')}
                  </SortableHead>
                  <Table.Head>{t('Contents')}</Table.Head>
                  <Table.Head sticky="right">
                    <span className="sr-only">{t('Actions')}</span>
                  </Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {table.rows.map((b) => (
                  <Table.Row
                    key={b.NAME}
                    variant={table.isSelected(b.NAME) ? 'selected' : 'default'}
                    {...(readOnly ? {} : rowLink(() => navigate(detail(b.NAME))))}
                    {...keys.rowProps(b.NAME)}
                  >
                    {!readOnly && (
                      <Table.CheckCell
                        checked={table.isSelected(b.NAME)}
                        onCheckedChange={(_, details) => table.toggle(b.NAME, details)}
                        label={t('Select %s', b.NAME)}
                      />
                    )}
                    <Table.Cell sticky="left">
                      {readOnly ? (
                        <span className="font-mono text-[0.9em] font-medium">{b.NAME}</span>
                      ) : (
                        <RouterLink
                          to={detail(b.NAME)}
                          className="font-mono text-[0.9em] font-medium text-kumo-default hover:underline"
                        >
                          {b.NAME}
                        </RouterLink>
                      )}
                    </Table.Cell>
                    <Table.Cell className="tabular-nums">{translateDate(b.DATE, t)}</Table.Cell>
                    <Table.Cell className="text-right tabular-nums">{formatBackupSize(b.SIZE)}</Table.Cell>
                    <Table.Cell>
                      <Badge variant={b.TYPE === 'local' ? 'neutral' : 'blue'}>{b.TYPE}</Badge>
                    </Table.Cell>
                    <Table.Cell className="text-right tabular-nums">{formatRuntime(b.RUNTIME, t)}</Table.Cell>
                    <Table.Cell>
                      <Contents backup={b} />
                    </Table.Cell>
                    <Table.Cell sticky="right" className="text-right">
                      <div className="flex items-center justify-end gap-1">
                        <Button
                          variant="ghost"
                          size="sm"
                          shape="square"
                          icon={<DownloadSimpleIcon size={18} />}
                          aria-label={t('Download %s', b.NAME)}
                          onClick={() => download(b.NAME)}
                        />
                        {!readOnly && (
                          <DropdownMenu>
                            <DropdownMenu.Trigger render={rowMenuTrigger(t('Actions for %s', b.NAME))} />
                            <DropdownMenu.Content align="end">
                              <DropdownMenu.Item
                                icon={ArrowCounterClockwiseIcon}
                                onClick={() => navigate(detail(b.NAME))}
                              >
                                {t('Restore')}
                              </DropdownMenu.Item>
                              <DropdownMenu.Item icon={DownloadSimpleIcon} onClick={() => download(b.NAME)}>
                                {t('Download')}
                              </DropdownMenu.Item>
                              <DropdownMenu.Separator />
                              <DropdownMenu.Item icon={TrashIcon} variant="danger" onClick={() => askDelete([b.NAME])}>
                                {t('Delete')}
                              </DropdownMenu.Item>
                            </DropdownMenu.Content>
                          </DropdownMenu>
                        )}
                      </div>
                    </Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </Table>
          </TableCard>
        )}
      </div>

      <ListConfirmDialog confirm={confirm} />
    </div>
  )
}
