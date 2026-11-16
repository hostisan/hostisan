import { Badge, Button, Loader, Text, DropdownMenu, LinkButton, Table, Tooltip, useKumoToastManager } from '@cloudflare/kumo'
import {
  ArchiveIcon,
  CheckCircleIcon,
  ClockIcon,
  CopySimpleIcon,
  DatabaseIcon,
  EnvelopeSimpleIcon,
  GlobeIcon,
  LinkSimpleIcon,
  ListBulletsIcon,
  MinusCircleIcon,
  PackageIcon,
  PencilSimpleIcon,
  PlusIcon,
  SignpostIcon,
  TrashIcon,
  TrayIcon,
} from '@phosphor-icons/react'
import { useMemo, useState } from 'react'
import { Link as RouterLink, useNavigate } from 'react-router'
import { apiErrorText } from '../../api/errors'
import type { PackageEntry } from '../../api/hestia.gen'
import type { BulkReply } from '../../api/types.gen'
import { usePackageMutation, usePackages, useUsers } from '../../api/users'
import { PageHeader } from '../../components/PageHeader'
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
import { ListConfirmDialog, useConfirm } from '../../components/useListConfirm'
import { useT } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { formatMB } from '../../lib/format'
import { useSessionUser } from '../../session/SessionProvider'
import { DnsError, DnsLoading } from '../dns/shared'
import { shortcutTarget } from '../../shortcuts/targets'
import { useRowShortcuts } from '../../shortcuts/useRowShortcuts'

// The server refuses to delete the built-in packages, and "system" can't be opened.
const PROTECTED = new Set(['default', 'system'])

const optionalColumns: ToggleableColumn[] = [
  { key: 'shell', label: msgid('SSH access') },
  { key: 'aliases', label: msgid('Web aliases') },
  { key: 'records', label: msgid('DNS records') },
  { key: 'accounts', label: msgid('Mail accounts') },
  { key: 'cron', label: msgid('Cron jobs') },
  { key: 'backups', label: msgid('Backups') },
]

const limitValue = (v: string) => (v === 'unlimited' ? Number.POSITIVE_INFINITY : Number(v))

/** Hestia shows the infinity sign for "unlimited"; the quota also for any letters (users §1.4). */
const isUnlimited = (v: string) => v === 'unlimited' || /[a-z]/i.test(v)

function Limit({ label, value }: { label: string; value: string }) {
  const t = useT()
  return (
    <Table.Cell className="text-right tabular-nums" title={t('%s: %s', label, value === 'unlimited' ? t('Unlimited') : value)}>
      {value === 'unlimited' ? (
        <span className="text-kumo-subtle" aria-label={t('Unlimited')}>
          ∞
        </span>
      ) : (
        value
      )}
    </Table.Cell>
  )
}

function SizeCell({ label, value }: { label: string; value: string }) {
  const t = useT()
  return (
    <Table.Cell className="text-right tabular-nums" title={t('%s: %s', label, isUnlimited(value) ? t('Unlimited') : formatMB(value))}>
      {isUnlimited(value) ? (
        <span className="text-kumo-subtle" aria-label={t('Unlimited')}>
          ∞
        </span>
      ) : (
        formatMB(value)
      )}
    </Table.Cell>
  )
}

function RowActions({
  name,
  readOnly,
  onDelete,
  onCopy,
}: {
  name: string
  readOnly: boolean
  onDelete: () => void
  onCopy: () => void
}) {
  const navigate = useNavigate()
  const t = useT()
  return (
    <DropdownMenu>
      <DropdownMenu.Trigger render={rowMenuTrigger(t('Actions for %s', name))} />
      <DropdownMenu.Content align="end">
        {name !== 'system' && (
          <DropdownMenu.Item icon={PencilSimpleIcon} onClick={() => navigate(`/packages/${name}`)}>
            {t('Edit')}
          </DropdownMenu.Item>
        )}
        {!readOnly && (
          <DropdownMenu.Item icon={CopySimpleIcon} onClick={onCopy}>
            {t('Duplicate')}
          </DropdownMenu.Item>
        )}
        {!readOnly && name !== 'system' && (
          <>
            <DropdownMenu.Separator />
            {PROTECTED.has(name) ? (
              <Tooltip content={t("Built-in packages can't be deleted")} side="left">
                <DropdownMenu.Item icon={TrashIcon} variant="danger" disabled>
                  {t('Delete')}
                </DropdownMenu.Item>
              </Tooltip>
            ) : (
              <DropdownMenu.Item icon={TrashIcon} variant="danger" onClick={onDelete}>
                {t('Delete')}
              </DropdownMenu.Item>
            )}
          </>
        )}
      </DropdownMenu.Content>
    </DropdownMenu>
  )
}

export function PackagesPage() {
  const navigate = useNavigate()
  const toast = useKumoToastManager()
  const t = useT()
  const me = useSessionUser()
  const readOnly = me?.readOnly ?? false
  const sortOrder = me?.sortOrder
  const list = usePackages()
  const users = useUsers()
  const m = usePackageMutation()
  const confirm = useConfirm()
  const [progress, setProgress] = useState<{ done: number; total: number } | null>(null)
  const columns = useColumnVisibility(
    'packages',
    optionalColumns.map((c) => ({ ...c, label: t(c.label) })),
  )
  const show = columns.isVisible

  const all = useMemo<PackageEntry[]>(() => list.data?.packages ?? [], [list.data])
  const userCounts = useMemo(
    () =>
      (users.data?.users ?? []).reduce<Record<string, number>>((acc, u) => {
        acc[u.PACKAGE] = (acc[u.PACKAGE] ?? 0) + 1
        return acc
      }, {}),
    [users.data],
  )

  const table = useDataTable({
    rows: all,
    getId: (p) => p.NAME,
    search: (p, q) => p.NAME.toLowerCase().includes(q),
    sorts: {
      name: (p) => p.NAME,
      date: (p) => `${p.DATE} ${p.TIME}`,
      users: (p) => userCounts[p.NAME] ?? 0,
      quota: (p) => limitValue(p.DISK_QUOTA),
      bandwidth: (p) => limitValue(p.BANDWIDTH),
    },
    // Open in the admin's saved list order (PREF_UI_SORT), as the users list does.
    initialSort: sortOrder === 'date' ? { key: 'date', dir: 'desc' } : { key: 'name', dir: 'asc' },
  })

  // One package per request, with progress: each delete moves its users first and can be slow.
  const deleteEach = async (names: string[]): Promise<BulkReply> => {
    const failed: BulkReply['failed'] = []
    setProgress({ done: 0, total: names.length })
    try {
      for (const [i, name] of names.entries()) {
        try {
          failed.push(...(await m.bulk({ action: 'delete', names: [name] })).failed)
        } catch (err) {
          failed.push({ name, message: apiErrorText(err, t) })
        }
        setProgress({ done: i + 1, total: names.length })
      }
    } finally {
      setProgress(null)
    }
    return { status: 'ok', failed }
  }

  const askDelete = (names: string[]) =>
    confirm.ask({
      title: names.length === 1 ? t('Delete package') : t('Delete packages'),
      description:
        names.length === 1
          ? t('Are you sure you want to delete package %s?', names[0])
          : t('Are you sure you want to delete %s?', t('%s packages', names.length)),
      confirmLabel: t('Delete'),
      destructive: true,
      done: names.length === 1 ? t('Package %s deleted', names[0]) : t('%s packages deleted', names.length),
      onDone: table.clearSelection,
      run: () => (names.length === 1 ? m.remove(names[0]) : deleteEach(names)),
    })

  const copy = async (name: string) => {
    try {
      await m.copy(name)
      toast.add({ title: t('Package %s created', `${name}-copy`), type: 'success' })
    } catch (err) {
      toast.add({ title: t('Duplicate %s', name), description: apiErrorText(err, t), type: 'error' })
    }
  }

  // Hestia's package list keys: Enter edit, Delete (never the built-in packages).
  const keys = useRowShortcuts(table.rows, (p) => p.NAME, (p) => ({
    enter: p.NAME === 'system' ? undefined : () => navigate(`/packages/${p.NAME}`),
    delete: readOnly || p.NAME === 'system' || PROTECTED.has(p.NAME) ? undefined : () => askDelete([p.NAME]),
  }))

  const sortHead = (key: string, label: string) => (
    <SortableHead sortKey={key} sort={table.sort} onSort={table.toggleSort} align="right">
      {label}
    </SortableHead>
  )

  return (
    <div className="grid gap-6">
      <PageHeader
        title={t('Packages')}
        description={t('Resource limits and templates applied to the users assigned to each package.')}
        back={{ href: '/users', label: t('Users') }}
        actions={
          !readOnly && (
            <LinkButton href="/packages/new" variant="primary" icon={PlusIcon} {...shortcutTarget('add')}>
              {t('Add package')}
            </LinkButton>
          )
        }
      />

      {list.isPending ? (
        <DnsLoading />
      ) : list.isError ? (
        <DnsError error={list.error} />
      ) : (
        <div className="grid gap-3">
          {progress && (
            <div role="status" className="flex items-center gap-2 rounded-lg bg-kumo-tint px-3 py-2 ring ring-kumo-line">
              <Loader size="sm" />
              <Text as="span">{t('%s: %s of %s', t('Delete'), progress.done, progress.total)}</Text>
            </div>
          )}
          <TableToolbar
            search={{ label: t('Search packages'), placeholder: t('Search packages'), value: table.query, onChange: table.setQuery }}
            selectedCount={readOnly ? 0 : table.selectedCount}
            onClearSelection={table.clearSelection}
            bulkActions={
              <Button
                size="sm"
                variant="secondary-destructive"
                icon={TrashIcon}
                onClick={() => {
                  const names = table.selectedRows.map((p) => p.NAME).filter((n) => !PROTECTED.has(n))
                  if (names.length) askDelete(names)
                  else toast.add({ title: t('Built-in packages (default, system) can’t be deleted.'), type: 'error' })
                }}
              >
                {t('Delete')}
              </Button>
            }
          />

          <TableCard
            rowShortcuts={keys}
            noun={table.filteredCount === 1 ? t('package') : t('packages')}
            filteredCount={table.filteredCount}
            totalCount={all.length}
            onClearSearch={() => table.setQuery('')}
            emptyState={{
              icon: <PackageIcon size={40} className="text-kumo-inactive" />,
              title: t('No packages yet'),
              description: t('Packages set the limits and templates for the users assigned to them.'),
              action: (
                <LinkButton href="/packages/new" variant="primary" icon={PlusIcon}>
                  {t('Add package')}
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
                      label={t('Select all packages')}
                    />
                  )}
                  <SortableHead sortKey="name" sort={table.sort} onSort={table.toggleSort} sticky="left">
                    {t('Package')}
                  </SortableHead>
                  {sortHead('users', t('Users'))}
                  {show('shell') && <Table.Head>{t('SSH access')}</Table.Head>}
                  {sortHead('quota', t('Quota'))}
                  {sortHead('bandwidth', t('Bandwidth'))}
                  <IconHead icon={GlobeIcon} label={t('Web domains')} />
                  {show('aliases') && <IconHead icon={LinkSimpleIcon} label={t('Web aliases per domain')} />}
                  <IconHead icon={SignpostIcon} label={t('DNS zones')} />
                  {show('records') && <IconHead icon={ListBulletsIcon} label={t('DNS records per domain')} />}
                  <IconHead icon={EnvelopeSimpleIcon} label={t('Mail domains')} />
                  {show('accounts') && <IconHead icon={TrayIcon} label={t('Mail accounts per domain')} />}
                  <IconHead icon={DatabaseIcon} label={t('Databases')} />
                  {show('cron') && <IconHead icon={ClockIcon} label={t('Cron jobs')} />}
                  {show('backups') && <IconHead icon={ArchiveIcon} label={t('Backups')} />}
                  <Table.Head sticky="right" className="w-px text-right">
                    <ColumnsMenu visibility={columns} />
                  </Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {table.rows.map((p) => (
                  <Table.Row
                    key={p.NAME}
                    variant={table.isSelected(p.NAME) ? 'selected' : 'default'}
                    {...(p.NAME === 'system' ? {} : rowLink(() => navigate(`/packages/${p.NAME}`)))}
                    {...keys.rowProps(p.NAME)}
                  >
                    {!readOnly && (
                      <Table.CheckCell
                        checked={table.isSelected(p.NAME)}
                        onCheckedChange={(_, details) => table.toggle(p.NAME, details)}
                        label={t('Select %s', p.NAME)}
                      />
                    )}
                    <Table.Cell sticky="left">
                      <div className="flex items-center gap-2">
                        {p.NAME === 'system' ? (
                          <span className="font-medium">{p.NAME}</span>
                        ) : (
                          <RouterLink to={`/packages/${p.NAME}`} className="font-medium text-kumo-default hover:underline">
                            {p.NAME}
                          </RouterLink>
                        )}
                        {PROTECTED.has(p.NAME) && <Badge variant="neutral">{t('Built-in')}</Badge>}
                      </div>
                    </Table.Cell>
                    <Table.Cell className="text-right tabular-nums">{userCounts[p.NAME] ?? 0}</Table.Cell>
                    {show('shell') && (
                      <Table.Cell>
                        {p.SHELL === 'nologin' ? (
                          <MinusCircleIcon size={18} className="text-kumo-subtle" aria-label={t('%s: %s', t('SSH access'), 'nologin')} role="img" />
                        ) : (
                          <CheckCircleIcon size={18} weight="fill" className="text-kumo-success" aria-label={t('%s: %s', t('SSH access'), p.SHELL)} role="img" />
                        )}
                      </Table.Cell>
                    )}
                    <SizeCell label={t('Quota')} value={p.DISK_QUOTA} />
                    <SizeCell label={t('Bandwidth')} value={p.BANDWIDTH} />
                    <Limit label={t('Web domains')} value={p.WEB_DOMAINS} />
                    {show('aliases') && <Limit label={t('Web aliases')} value={p.WEB_ALIASES} />}
                    <Limit label={t('DNS zones')} value={p.DNS_DOMAINS} />
                    {show('records') && <Limit label={t('DNS records')} value={p.DNS_RECORDS} />}
                    <Limit label={t('Mail domains')} value={p.MAIL_DOMAINS} />
                    {show('accounts') && <Limit label={t('Mail accounts')} value={p.MAIL_ACCOUNTS} />}
                    <Limit label={t('Databases')} value={p.DATABASES} />
                    {show('cron') && <Limit label={t('Cron jobs')} value={p.CRON_JOBS} />}
                    {show('backups') && <Limit label={t('Backups')} value={p.BACKUPS} />}
                    <Table.Cell sticky="right" className="text-right">
                      <RowActions name={p.NAME} readOnly={readOnly} onDelete={() => askDelete([p.NAME])} onCopy={() => copy(p.NAME)} />
                    </Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </Table>
          </TableCard>
        </div>
      )}

      <ListConfirmDialog confirm={confirm} />
    </div>
  )
}
