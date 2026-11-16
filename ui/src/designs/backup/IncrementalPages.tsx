import {
  Badge,
  Banner,
  Breadcrumbs,
  Button,
  DropdownMenu,
  Empty,
  LinkButton,
  Table,
  Text,
  Tooltip,
  useKumoToastManager,
} from '@cloudflare/kumo'
import {
  ArrowCounterClockwiseIcon,
  ArrowUpIcon,
  FileIcon,
  FolderIcon,
  FolderOpenIcon,
  HouseIcon,
  PlusIcon,
  TrashIcon,
  VaultIcon,
  WarningCircleIcon,
} from '@phosphor-icons/react'
import { useMemo, useState } from 'react'
import { useNavigate, useParams, useSearchParams, Link as RouterLink } from 'react-router'
import { useBackupMutation, useSnapshot, useSnapshotFiles, useSnapshots } from '../../api/backup'
import { ApiError } from '../../api/client'
import { useRestoreFiles, useRestoreJob } from '../../api/tools'
import { apiErrorText, messageText } from '../../api/errors'
import type { SnapshotNode } from '../../api/hestia.gen'
import type { RestoreFilesResult } from '../../api/types.gen'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { PageHeader } from '../../components/PageHeader'
import { SortableHead, TableCard, TableToolbar, rowLink, rowMenuTrigger, useDataTable } from '../../components/table'
import { ListConfirmDialog, useConfirm } from '../../components/useListConfirm'
import { useT } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { useSessionUser } from '../../session/SessionProvider'
import { DnsError, DnsLoading } from '../dns/shared'
import { Summary } from './BackupDetailPage'
import { BackupTabs, formatDateTime, formatRuntime, useRefetchOnFocus } from './BackupTabs'
import { interpolate } from '../../lib/interpolate'
import { RestoreTable, restoreItems } from './RestoreTable'
import { shortcutTarget } from '../../shortcuts/targets'
import { useRowShortcuts } from '../../shortcuts/useRowShortcuts'

const listBack = { href: '/backup/incremental', label: msgid('Incremental backups') }

export function IncrementalBackupsPage() {
  const toast = useKumoToastManager()
  const t = useT()
  const navigate = useNavigate()
  const readOnly = useSessionUser()?.readOnly ?? false
  const list = useSnapshots()
  const m = useBackupMutation()
  const confirm = useConfirm()
  useRefetchOnFocus(list.refetch)
  const rows = useMemo(() => list.data?.snapshots ?? [], [list.data])
  // The API sends newest first.
  const latest = rows[0]?.short_id

  const table = useDataTable({
    rows,
    getId: (s) => s.short_id,
    search: (s, q) => s.short_id.includes(q) || s.time.includes(q) || s.hostname.includes(q),
    sorts: { snapshot: (s) => s.short_id, date: (s) => s.time },
    initialSort: { key: 'date', dir: 'desc' },
  })

  const askDelete = (ids: string[]) =>
    confirm.ask({
      title: t('Delete'),
      description:
        ids.length === 1
          ? t('Are you sure you want to delete snapshot %s?', ids[0])
          : t('Are you sure you want to delete %s?', t('%s snapshots', ids.length)),
      confirmLabel: t('Delete'),
      destructive: true,
      done: ids.length === 1 ? t('Snapshot deleted') : t('Snapshots deleted'),
      onDone: table.clearSelection,
      run: () => m.bulkSnapshots({ action: 'delete', names: ids }),
    })

  // Hestia's snapshot list keys: Enter restore, plus Delete.
  const keys = useRowShortcuts(table.rows, (s) => s.short_id, (s) => ({
    enter: () => navigate(`/backup/incremental/${s.short_id}`),
    delete: readOnly ? undefined : () => askDelete([s.short_id]),
  }))

  const createSnapshot = () =>
    m.scheduleSnapshot().then(
      (r) => toast.add({ title: t('Snapshot scheduled'), description: t(r.message), type: 'success' }),
      (err) => toast.add({ title: t('Snapshot not scheduled'), description: apiErrorText(err, t), type: 'error' }),
    )

  return (
    <div className="grid gap-6">
      <PageHeader
        title={t('Backups')}
        description={t(
          'Restic snapshots of your home directory. Each snapshot only stores what changed since the last one.',
        )}
        actions={
          !readOnly && (
            <Button variant="primary" icon={PlusIcon} {...shortcutTarget('add')} onClick={createSnapshot}>
              {t('Create snapshot')}
            </Button>
          )
        }
      />
      <BackupTabs value="/backup/incremental" />

      <div className="grid gap-3">
        <TableToolbar
          search={{
            label: t('Search snapshots'),
            placeholder: t('Search by snapshot ID or date'),
            value: table.query,
            onChange: table.setQuery,
          }}
          selectedCount={readOnly ? 0 : table.selectedCount}
          onClearSelection={table.clearSelection}
          bulkActions={
            <Button
              size="sm"
              variant="secondary-destructive"
              icon={TrashIcon}
              onClick={() => askDelete(table.selectedRows.map((s) => s.short_id))}
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
            noun={{ one: t('snapshot'), other: t('snapshots') }}
            filteredCount={table.filteredCount}
            totalCount={rows.length}
            onClearSearch={() => table.setQuery('')}
            emptyState={{
              icon: <VaultIcon size={40} className="text-kumo-inactive" />,
              title: t('No snapshots yet'),
              description: t('Create a snapshot to keep an incremental copy of your home directory.'),
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
                      label={t('Select all snapshots')}
                    />
                  )}
                  <SortableHead sortKey="snapshot" sort={table.sort} onSort={table.toggleSort} sticky="left">
                    {t('Snapshot')}
                  </SortableHead>
                  <SortableHead sortKey="date" sort={table.sort} onSort={table.toggleSort}>
                    {t('Date')}
                  </SortableHead>
                  <Table.Head>{t('Type')}</Table.Head>
                  <Table.Head>{t('Hostname')}</Table.Head>
                  <Table.Head sticky="right">
                    <span className="sr-only">{t('Actions')}</span>
                  </Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {table.rows.map((s) => (
                  <Table.Row
                    key={s.short_id}
                    variant={table.isSelected(s.short_id) ? 'selected' : 'default'}
                    {...rowLink(() => navigate(`/backup/incremental/${s.short_id}`))}
                    {...keys.rowProps(s.short_id)}
                  >
                    {!readOnly && (
                      <Table.CheckCell
                        checked={table.isSelected(s.short_id)}
                        onCheckedChange={(_, details) => table.toggle(s.short_id, details)}
                        label={t('Select snapshot %s', s.short_id)}
                      />
                    )}
                    <Table.Cell sticky="left">
                      <span className="flex items-center gap-2">
                        <RouterLink
                          to={`/backup/incremental/${s.short_id}`}
                          className="font-mono text-[0.9em] font-medium text-kumo-default hover:underline"
                        >
                          {s.short_id}
                        </RouterLink>
                        {s.short_id === latest && <Badge variant="green">{t('Latest')}</Badge>}
                      </span>
                    </Table.Cell>
                    <Table.Cell className="tabular-nums">{formatDateTime(s.time)}</Table.Cell>
                    <Table.Cell>Restic</Table.Cell>
                    <Table.Cell>{s.hostname}</Table.Cell>
                    <Table.Cell sticky="right" className="text-right">
                      <div className="flex items-center justify-end gap-1">
                        <LinkButton
                          href={`/backup/incremental/${s.short_id}/files`}
                          variant="ghost"
                          size="sm"
                          icon={FolderOpenIcon}
                        >
                          {t('Browse')}
                        </LinkButton>
                        <DropdownMenu>
                          <DropdownMenu.Trigger render={rowMenuTrigger(t('Actions for snapshot %s', s.short_id))} />
                          <DropdownMenu.Content align="end">
                            {!readOnly && (
                              <DropdownMenu.Item
                                icon={ArrowCounterClockwiseIcon}
                                onClick={() => navigate(`/backup/incremental/${s.short_id}`)}
                              >
                                {t('Restore')}
                              </DropdownMenu.Item>
                            )}
                            <DropdownMenu.Item
                              icon={FolderOpenIcon}
                              onClick={() => navigate(`/backup/incremental/${s.short_id}/files`)}
                            >
                              {t('Browse files')}
                            </DropdownMenu.Item>
                            {!readOnly && (
                              <>
                                <DropdownMenu.Separator />
                                <DropdownMenu.Item
                                  icon={TrashIcon}
                                  variant="danger"
                                  onClick={() => askDelete([s.short_id])}
                                >
                                  {t('Delete')}
                                </DropdownMenu.Item>
                              </>
                            )}
                          </DropdownMenu.Content>
                        </DropdownMenu>
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

function SnapshotNotFound({ id }: { id: string }) {
  const t = useT()
  return (
    <div className="grid gap-6">
      <PageHeader title={t('Snapshot not found')} back={{ ...listBack, label: t(listBack.label) }} />
      <Empty
        icon={<VaultIcon size={40} className="text-kumo-inactive" />}
        title={t('Snapshot not found')}
        description={t("There is no snapshot %s in this account's restic repository.", id)}
        contents={<LinkButton href="/backup/incremental">{t('Back to snapshots')}</LinkButton>}
        className="py-16"
      />
    </div>
  )
}

export function SnapshotDetailPage() {
  const { snapshot = '' } = useParams()
  const toast = useKumoToastManager()
  const t = useT()
  const back = { ...listBack, label: t(listBack.label) }
  const readOnly = useSessionUser()?.readOnly ?? false
  const [confirmOpen, setConfirmOpen] = useState(false)
  const q = useSnapshot(snapshot)
  const list = useSnapshots().data?.snapshots
  const m = useBackupMutation()

  if (q.isPending) return <DnsLoading />
  if (q.isError && !(q.error instanceof ApiError && q.error.status === 404)) {
    return (
      <div className="grid gap-6">
        <PageHeader title={t('Snapshot')} back={back} />
        <DnsError error={q.error} />
      </div>
    )
  }
  if (!q.data) return <SnapshotNotFound id={snapshot} />
  const d = q.data.detail
  const s = list?.find((x) => x.short_id === snapshot)

  const restoreAll = () =>
    m.restoreSnapshot(snapshot, { all: true, web: [], dns: [], mail: [], db: [], udir: [], cron: false }).then(
      (r) => toast.add({ title: t('Restore scheduled'), description: t(r.message), type: 'success' }),
      (err) =>
        toast.add({
          title: t('Restore failed'),
          description: <span className="whitespace-pre-line">{apiErrorText(err, t)}</span>,
          type: 'error',
        }),
    )

  return (
    <div className="grid gap-6">
      <PageHeader
        title={t('Restore from snapshot')}
        description={<span className="font-mono text-[0.9em]">{snapshot}</span>}
        back={back}
        actions={
          <>
            <LinkButton href={`/backup/incremental/${snapshot}/files`} icon={FolderOpenIcon}>
              {t('Browse files')}
            </LinkButton>
            {!readOnly && (
              <Button variant="primary" icon={ArrowCounterClockwiseIcon} onClick={() => setConfirmOpen(true)}>
                {t('Restore all')}
              </Button>
            )}
          </>
        }
      />

      <Summary
        items={[
          { label: t('Date'), value: `${d.DATE} ${d.TIME}` },
          { label: t('Type'), value: 'Restic' },
          ...(s ? [{ label: t('Hostname'), value: s.hostname }] : []),
          { label: t('Runtime'), value: formatRuntime(d.RUNTIME, t) },
          ...(s
            ? [{ label: t('Paths'), value: <span className="font-mono text-[0.9em]">{s.paths.join(', ')}</span> }]
            : []),
        ]}
      />

      <div className="grid gap-1">
        <Text variant="heading" as="h2">
          {t('Snapshot contents')}
        </Text>
        <Text variant="secondary">
          {t(
            'Restore whole web domains, mail domains, DNS zones, databases or cron jobs. Individual files can be browsed but not restored.',
          )}
        </Text>
      </div>
      <RestoreTable
        items={restoreItems(d, ['web', 'mail', 'dns', 'db', 'cron'])}
        restoreLabel={t('Restore snapshot')}
        restore={(body) => m.restoreSnapshot(snapshot, body)}
        readOnly={readOnly}
      />

      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={t('Restore all')}
        description={t(
          'Are you sure you want to restore everything in snapshot %s? Existing data will be overwritten.',
          snapshot,
        )}
        confirmLabel={t('Restore all')}
        onConfirm={restoreAll}
      />
    </div>
  )
}

const typeLabel: Record<string, string> = { dir: msgid('Directory'), file: msgid('File'), symlink: msgid('Symlink') }

// v-restore-file-restic expands the path unquoted in a root shell: the server refuses
// whitespace, wildcards, backslashes and a leading dash, so Restore is off for them.
const unsafeChars = /[\s\p{Cc}\uFFFD*?[\]\\]/u
const unsafeHint = msgid("Files with spaces or special characters in their names can't be restored from here.")
const restoreMaxPaths = 50
const restoreMaxBytes = 4096

/** Mirrors the API's ValidRestorePath for a path under home: the whole path, every segment. */
function restorePathOk(home: string, p: string): boolean {
  const rest = p.startsWith(`${home}/`) ? p.slice(home.length + 1) : ''
  if (rest === '' || rest === 'conf' || rest.startsWith('conf/')) return false
  if (new TextEncoder().encode(p).length > restoreMaxBytes || unsafeChars.test(p)) return false
  return rest.split('/').every((seg) => seg !== '' && !seg.startsWith('-'))
}

// Folders always list before files, whichever column is sorted.
const dirFirst = (f: SnapshotNode) => (f.type === 'dir' ? '0' : '1')

/** A file size in bytes as "412 B", "2.0 KB", "86.2 KB", "1.4 MB". */
function formatBytes(n: number): string {
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = n
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return i === 0 ? `${v} B` : `${v.toFixed(1)} ${units[i]}`
}

function FilesTable({
  snapshot,
  home,
  path,
  parent,
  nodes,
  readOnly,
  busy,
  onRestore,
}: {
  snapshot: string
  home: string
  path: string
  parent: string
  nodes: SnapshotNode[]
  readOnly: boolean
  busy: boolean
  onRestore: (paths: string[], done: () => void) => void
}) {
  const t = useT()
  const [, setParams] = useSearchParams()
  const table = useDataTable({
    rows: nodes,
    getId: (f) => f.path,
    search: (f, q) => f.name.toLowerCase().includes(q),
    sorts: {
      name: (f) => `${dirFirst(f)}${f.name.toLowerCase()}`,
      size: (f) => f.size,
      date: (f) => f.ctime,
    },
    initialSort: { key: 'name', dir: 'asc' },
    pageSize: 20,
  })

  const go = (p: string) => setParams(p === home ? {} : { path: p })
  // The server won't list ~/conf (it holds server-generated config).
  const browsable = (p: string) => p !== `${home}/conf` && !p.startsWith(`${home}/conf/`)
  // Restorable: anywhere but ~/conf, and with a name the server's path rules accept.
  const restorable = (f: SnapshotNode) => browsable(f.path)
  const restoreOk = (f: SnapshotNode) => restorable(f) && restorePathOk(home, f.path)
  // Only restorable rows can be selected: select-all and the counts cover just those.
  const eligible = table.rows.filter(restoreOk)
  const allEligibleSelected = eligible.length > 0 && eligible.every((f) => table.isSelected(f.path))
  const toggleAllEligible = () => {
    if (allEligibleSelected) for (const f of eligible) table.toggle(f.path)
    else for (const f of eligible) if (!table.isSelected(f.path)) table.toggle(f.path)
  }
  const picked = table.selectedRows.filter(restoreOk).map((f) => f.path)
  const overCap = picked.length > restoreMaxPaths
  const bulkButton = (
    <Button
      size="sm"
      variant="primary"
      icon={ArrowCounterClockwiseIcon}
      disabled={picked.length === 0 || overCap || busy}
      onClick={() => onRestore(picked, table.clearSelection)}
    >
      {t('Restore selected')}
    </Button>
  )

  return (
    <div className="grid gap-3">
      <TableToolbar
        search={{
          label: t('Search this folder'),
          placeholder: t('Search this folder'),
          value: table.query,
          onChange: table.setQuery,
        }}
        selectedCount={readOnly ? 0 : picked.length}
        onClearSelection={table.clearSelection}
        bulkActions={
          overCap ? (
            <Tooltip content={t('You can restore up to %s files at once.', restoreMaxPaths)}>
              <span tabIndex={0} data-testid="restore-cap" className="inline-flex">
                {bulkButton}
              </span>
            </Tooltip>
          ) : (
            bulkButton
          )
        }
      />

      <TableCard
        noun={t('items')}
        filteredCount={table.filteredCount}
        totalCount={nodes.length}
        onClearSearch={() => table.setQuery('')}
        page={table.page}
        pageSize={table.pageSize}
        onPageChange={table.setPage}
        emptyState={{
          icon: <FolderIcon size={40} className="text-kumo-inactive" />,
          title: t('This folder is empty'),
          description: t('Nothing was stored under %s in this snapshot.', path),
          action: parent && <Button onClick={() => go(parent)}>{t('Go up one level')}</Button>,
        }}
      >
        <Table className="whitespace-nowrap">
          <Table.Header>
            <Table.Row>
              {!readOnly && (
                <Table.CheckHead
                  checked={allEligibleSelected}
                  indeterminate={picked.length > 0 && !allEligibleSelected}
                  onCheckedChange={toggleAllEligible}
                  label={t('Select all items')}
                />
              )}
              <SortableHead sortKey="name" sort={table.sort} onSort={table.toggleSort} sticky="left" className="w-full">
                {t('Name')}
              </SortableHead>
              <Table.Head>{t('Type')}</Table.Head>
              <SortableHead sortKey="size" sort={table.sort} onSort={table.toggleSort} className="text-right">
                {t('Size')}
              </SortableHead>
              <SortableHead sortKey="date" sort={table.sort} onSort={table.toggleSort}>
                {t('Date')}
              </SortableHead>
              {!readOnly && (
                <Table.Head sticky="right">
                  <span className="sr-only">{t('Actions')}</span>
                </Table.Head>
              )}
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {parent && !table.isFiltered && table.page === 1 && (
              <Table.Row {...rowLink(() => go(parent))}>
                {!readOnly && <Table.Cell />}
                <Table.Cell sticky="left" colSpan={readOnly ? 4 : 5}>
                  <button
                    type="button"
                    onClick={() => go(parent)}
                    className="flex items-center gap-2 font-medium"
                    aria-label={t('Up one level')}
                  >
                    <ArrowUpIcon className="text-kumo-subtle" />
                    ..
                  </button>
                </Table.Cell>
              </Table.Row>
            )}
            {table.rows.map((f) => {
              const canOpen = f.type === 'dir' && browsable(f.path)
              return (
                <Table.Row
                  key={f.path}
                  variant={table.isSelected(f.path) ? 'selected' : 'default'}
                  {...(canOpen ? rowLink(() => go(f.path)) : {})}
                >
                  {!readOnly &&
                    (restoreOk(f) ? (
                      <Table.CheckCell
                        checked={table.isSelected(f.path)}
                        onCheckedChange={(_, d) => table.toggle(f.path, d)}
                        label={t('Select %s', f.name)}
                      />
                    ) : (
                      <Table.Cell />
                    ))}
                  <Table.Cell sticky="left">
                    {canOpen ? (
                      <RouterLink
                        to={`/backup/incremental/${snapshot}/files?path=${encodeURIComponent(f.path)}`}
                        className="flex items-center gap-2 font-medium text-kumo-default hover:underline"
                      >
                        <FolderIcon weight="fill" className="text-kumo-subtle" />
                        {f.name}
                      </RouterLink>
                    ) : (
                      <span className="flex items-center gap-2 font-medium">
                        {f.type === 'dir' ? (
                          <FolderIcon weight="fill" className="text-kumo-inactive" />
                        ) : (
                          <FileIcon className="text-kumo-subtle" />
                        )}
                        {f.name}
                      </span>
                    )}
                  </Table.Cell>
                  <Table.Cell>{typeLabel[f.type] ? t(typeLabel[f.type]) : t('Unknown')}</Table.Cell>
                  <Table.Cell className="text-right tabular-nums">
                    {f.type === 'file' ? formatBytes(f.size) : <span className="text-kumo-subtle">-</span>}
                  </Table.Cell>
                  <Table.Cell className="tabular-nums">{formatDateTime(f.ctime)}</Table.Cell>
                  {!readOnly && (
                    <Table.Cell sticky="right" className="text-right">
                      {restorable(f) &&
                        (restoreOk(f) ? (
                          <Button
                            variant="ghost"
                            size="sm"
                            icon={ArrowCounterClockwiseIcon}
                            aria-label={t('Restore %s', f.name)}
                            disabled={busy}
                            onClick={(e) => {
                              e.stopPropagation()
                              onRestore([f.path], () => {})
                            }}
                          />
                        ) : (
                          <Tooltip content={t(unsafeHint)}>
                            <span
                              tabIndex={0}
                              data-testid={`restore-hint-${f.name}`}
                              className="inline-flex"
                              onClick={(e) => e.stopPropagation()}
                            >
                              <Button
                                variant="ghost"
                                size="sm"
                                icon={ArrowCounterClockwiseIcon}
                                aria-label={t('Restore %s', f.name)}
                                disabled
                              />
                            </span>
                          </Tooltip>
                        ))}
                    </Table.Cell>
                  )}
                </Table.Row>
              )
            })}
          </Table.Body>
        </Table>
      </TableCard>
    </div>
  )
}

/** The per-file restore job: progress while it runs, then what went back. */
function RestoreJobPanel({ id }: { id: string }) {
  const t = useT()
  const job = useRestoreJob(id)
  const j = job.data
  if (job.isError) return <Banner variant="error" icon={<WarningCircleIcon />} title={apiErrorText(job.error, t)} />
  if (!j || j.state === 'running') {
    return (
      <Banner
        title={t('Restoring files')}
        description={j?.step ? <span className="font-mono text-[0.9em]">{j.step}</span> : undefined}
      />
    )
  }
  const list = (paths: string[]) => (
    <ul className="mt-1 grid gap-0.5 font-mono text-[0.9em]">
      {paths.map((p) => (
        <li key={p}>{p}</li>
      ))}
    </ul>
  )
  if (j.state === 'done') {
    const done = (j.result as RestoreFilesResult | null)?.restored ?? j.steps
    return (
      <Banner
        variant="default"
        title={done.length === 1 ? t('Restored 1 file') : t('Restored %s files', done.length)}
        description={list(done)}
      />
    )
  }
  const failing = j.error?.args?.[0] ?? j.step
  const restored = j.steps.slice(0, -1)
  return (
    <Banner
      variant="error"
      icon={<WarningCircleIcon />}
      title={t('Could not restore %s', failing)}
      description={
        <>
          <span className="whitespace-pre-line">{j.error ? messageText(j.error.message, j.error.args ?? [], t) : ''}</span>
          {restored.length > 0 && (
            <>
              <br />
              {t('These files were restored before it stopped:')}
              {list(restored)}
            </>
          )}
        </>
      }
    />
  )
}

export function SnapshotFilesPage() {
  const { snapshot = '' } = useParams()
  const [params] = useSearchParams()
  const user = useSessionUser()
  const t = useT()
  const readOnly = user?.readOnly ?? false
  const home = `/home/${user?.look || user?.name || ''}`
  const path = params.get('path') ?? home
  const files = useSnapshotFiles(snapshot, path)
  const restore = useRestoreFiles(snapshot)
  const [asked, setAsked] = useState<string[]>([])
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [jobId, setJobId] = useState('')
  const [startError, setStartError] = useState<unknown>(null)
  const [clearAfter, setClearAfter] = useState<() => void>(() => () => {})
  const ask = (paths: string[], done: () => void) => {
    setAsked(paths)
    setClearAfter(() => done)
    setConfirmOpen(true)
  }
  const start = () => {
    if (restore.isPending) return
    setStartError(null)
    setJobId('')
    restore.mutate(
      { paths: asked },
      {
        onSuccess: (r) => {
          setJobId(r.jobId)
          clearAfter()
        },
        onError: (e) => setStartError(e),
      },
    )
  }

  const restoreOff =
    startError instanceof ApiError && startError.status === 409 && startError.code === 'disabled'
      ? startError.message
      : ''

  const segments = path.slice(home.length).split('/').filter(Boolean)
  const base = `/backup/incremental/${snapshot}/files`
  const back = { href: `/backup/incremental/${snapshot}`, label: t('Snapshot %s', snapshot) }

  return (
    <div className="grid gap-6">
      <PageHeader
        title={t('Browse snapshot')}
        description={interpolate(t('Snapshot %s'), <span className="font-mono text-[0.9em]">{snapshot}</span>)}
        back={back}
      />

      {restoreOff ? (
        // Per-file restore is off on this server (409 disabled) until the server check
        // is done: say so plainly rather than as a failure, and stop offering it.
        <Banner
          title={t(restoreOff)}
          description={t('Whole items can still be restored from the snapshot page.')}
        />
      ) : (
        startError != null && <Banner variant="error" icon={<WarningCircleIcon />} title={apiErrorText(startError, t)} />
      )}
      {jobId && <RestoreJobPanel id={jobId} />}

      <div className="grid gap-3">
        <div className="overflow-x-auto">
          <Breadcrumbs>
            {segments.length === 0 ? (
              <Breadcrumbs.Current icon={<HouseIcon size={16} />}>{home}</Breadcrumbs.Current>
            ) : (
              <Breadcrumbs.Link href={base} icon={<HouseIcon size={16} />}>
                {home}
              </Breadcrumbs.Link>
            )}
            {segments.map((seg, i) => {
              const p = `${home}/${segments.slice(0, i + 1).join('/')}`
              const last = i === segments.length - 1
              return [
                <Breadcrumbs.Separator key={`sep-${p}`} />,
                last ? (
                  <Breadcrumbs.Current key={p}>{seg}</Breadcrumbs.Current>
                ) : (
                  <Breadcrumbs.Link key={p} href={`${base}?path=${encodeURIComponent(p)}`}>
                    {seg}
                  </Breadcrumbs.Link>
                ),
              ]
            })}
          </Breadcrumbs>
        </div>

        {files.isPending ? (
          <DnsLoading />
        ) : files.isError ? (
          <DnsError error={files.error} />
        ) : (
          // Keyed by folder so search and page reset when navigating.
          <FilesTable
            key={path}
            snapshot={snapshot}
            home={home}
            path={files.data.path}
            parent={files.data.parent}
            nodes={files.data.nodes}
            readOnly={readOnly}
            busy={restore.isPending || restoreOff !== ''}
            onRestore={ask}
          />
        )}
      </div>

      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={t('Restore files')}
        description={
          asked.length === 1
            ? t('The live file will be overwritten by the version in this snapshot.')
            : t('The live files will be overwritten by the versions in this snapshot.')
        }
        confirmLabel={t('Restore')}
        onConfirm={start}
      />
    </div>
  )
}
