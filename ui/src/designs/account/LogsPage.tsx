import type { DateRange } from '../../components/fields'
import { ALL_DATES, DateRangeFilter, inRange } from '../../components/fields'
import { Badge, Button, LinkButton, Select, Table, Text, Toolbar, useKumoToastManager } from '@cloudflare/kumo'
import { ArrowClockwiseIcon, BinocularsIcon, ScrollIcon, TrashIcon } from '@phosphor-icons/react'
import { useMemo, useState } from 'react'
import { useSearchParams } from 'react-router'
import { useAuthLog, useLogMutation, useLogs } from '../../api/account'
import { apiErrorText } from '../../api/errors'
import type { AuthLogEntry, LogEntry } from '../../api/hestia.gen'
import { useUsers } from '../../api/users'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { PageHeader } from '../../components/PageHeader'
import { SortableHead, TableCard, TableToolbar, useDataTable } from '../../components/table'
import { useT } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { useSessionUser } from '../../session/SessionProvider'
import { DnsError, DnsLoading } from '../dns/shared'

const PAGE_SIZE = 25

const LEVEL_BADGE: Record<string, { variant: 'neutral' | 'warning' | 'error'; label: string }> = {
  Info: { variant: 'neutral', label: msgid('Information') },
  Warning: { variant: 'warning', label: msgid('Warning') },
  Error: { variant: 'error', label: msgid('Error') },
}

const SYSTEM = 'system'

/**
 * Whose log the page shows, from ?user= (admins only; Plan 4's services toolbar links to
 * /logs?user=system). The signed-in account's own log is "no user" on the wire.
 */
function useLogOwner() {
  const t = useT()
  const user = useSessionUser()
  const isAdmin = user?.role === 'admin'
  const own = user?.look || user?.name || ''
  const [params, setParams] = useSearchParams()
  const owner = isAdmin ? params.get('user') || own : own
  // The users list is refused while logged in as another user (owner ruling 12).
  const users = useUsers({ enabled: isAdmin && !user?.look }).data?.users ?? []
  const items: Record<string, string> = {
    [own]: t('User: %s', own),
    ...Object.fromEntries(users.filter((u) => u.USER !== own).map((u) => [u.USER, t('User: %s', u.USER)])),
    [SYSTEM]: t('System log'),
  }
  return {
    isAdmin,
    own,
    owner,
    /** The ?user= value to send; undefined for the own log. */
    query: owner === own ? undefined : owner,
    items,
    setOwner: (v: string) => setParams(v === own ? {} : { user: v }, { replace: true }),
    canDelete: (user?.canDeleteLogs ?? false) && !user?.readOnly,
    // The protected admin (a read-only admin) has no login history, nor has demo mode.
    canSeeHistory: (user?.features.loginHistory ?? false) && !(user?.readOnly && user.role === 'admin'),
  }
}

function UserFilter({ owner }: { owner: ReturnType<typeof useLogOwner> }) {
  const t = useT()
  if (!owner.isAdmin) return null
  return (
    <Select
      aria-label={t('Show logs for user')}
      value={owner.owner}
      onValueChange={(v) => owner.setOwner(String(v))}
      items={owner.items}
      render={<Toolbar.Button />}
    />
  )
}

/** The page description, one whole sentence per case. */
function changesText(o: ReturnType<typeof useLogOwner>, t: ReturnType<typeof useT>) {
  if (o.owner === SYSTEM) return t('Changes made to the server, newest first.')
  if (o.owner === o.own) return t('Changes made to your account, newest first.')
  return t("Changes made to %s's account, newest first.", o.owner)
}

export function LogsPage() {
  const toast = useKumoToastManager()
  const t = useT()
  const o = useLogOwner()
  const logs = useLogs(o.query)
  const m = useLogMutation()
  const [level, setLevel] = useState('all')
  const [category, setCategory] = useState('all')
  const [range, setRange] = useState<DateRange>(ALL_DATES)
  const [confirmOpen, setConfirmOpen] = useState(false)

  // The API sends the newest first.
  const entries = useMemo<LogEntry[]>(() => logs.data?.entries ?? [], [logs.data])
  const categories = useMemo(() => [...new Set(entries.map((e) => e.CATEGORY))].sort(), [entries])
  const filtered = useMemo(
    () =>
      entries.filter(
        (e) =>
          (level === 'all' || e.LEVEL === level) &&
          (category === 'all' || e.CATEGORY === category) &&
          inRange(e.DATE, range),
      ),
    [entries, level, category, range],
  )

  const table = useDataTable({
    rows: filtered,
    getId: (e) => e.ID,
    search: (e, q) => e.MESSAGE.toLowerCase().includes(q),
    sorts: {
      date: (e) => `${e.DATE} ${e.TIME} ${e.ID.padStart(8, '0')}`,
      category: (e) => e.CATEGORY,
    },
    initialSort: { key: 'date', dir: 'desc' },
    pageSize: PAGE_SIZE,
  })

  const authHref = o.query ? `/logs/auth?user=${encodeURIComponent(o.query)}` : '/logs/auth'

  return (
    <div className="grid gap-6">
      <PageHeader
        title={t('Logs')}
        description={changesText(o, t)}
        actions={
          <>
            {o.owner !== SYSTEM && o.canSeeHistory && (
              <LinkButton href={authHref} variant="secondary" icon={BinocularsIcon}>
                {t('Login history')}
              </LinkButton>
            )}
            <Button variant="secondary" icon={ArrowClockwiseIcon} onClick={() => void logs.refetch()}>
              {t('Refresh')}
            </Button>
            {o.canDelete && (
              <Button variant="secondary" icon={TrashIcon} disabled={entries.length === 0} onClick={() => setConfirmOpen(true)}>
                {t('Delete logs')}
              </Button>
            )}
          </>
        }
      />

      <div className="grid gap-3">
        <TableToolbar
          search={{ label: t('Search messages'), placeholder: t('Search messages'), value: table.query, onChange: table.setQuery }}
          filters={
            <>
              <Select
                aria-label={t('Filter by level')}
                value={level}
                onValueChange={(v) => setLevel(String(v))}
                items={{ all: t('All levels'), Info: t('Information'), Warning: t('Warning'), Error: t('Error') }}
                render={<Toolbar.Button />}
              />
              <Select
                aria-label={t('Filter by category')}
                value={category}
                onValueChange={(v) => setCategory(String(v))}
                items={{ all: t('All categories'), ...Object.fromEntries(categories.map((c) => [c, c])) }}
                render={<Toolbar.Button className="max-md:hidden" />}
              />
              <DateRangeFilter value={range} onChange={setRange} />
              <UserFilter owner={o} />
            </>
          }
        />

        {logs.isPending ? (
          <DnsLoading />
        ) : logs.isError ? (
          <DnsError error={logs.error} />
        ) : (
          <TableCard
            noun={{ one: t('log entry'), other: t('log entries') }}
            filteredCount={table.filteredCount}
            totalCount={entries.length}
            page={table.page}
            pageSize={table.pageSize}
            onPageChange={table.setPage}
            onClearSearch={() => {
              table.setQuery('')
              setLevel('all')
              setCategory('all')
              setRange(ALL_DATES)
            }}
            emptyState={{
              icon: <ScrollIcon size={40} className="text-kumo-inactive" />,
              title: t('No log entries'),
              description: t('Actions on this account, like adding domains or changing passwords, will show up here.'),
            }}
          >
            <Table className="whitespace-nowrap">
              <Table.Header>
                <Table.Row>
                  <Table.Head>{t('Level')}</Table.Head>
                  <SortableHead sortKey="date" sort={table.sort} onSort={table.toggleSort}>
                    {t('Date')}
                  </SortableHead>
                  <Table.Head>{t('Time')}</Table.Head>
                  <SortableHead sortKey="category" sort={table.sort} onSort={table.toggleSort}>
                    {t('Category')}
                  </SortableHead>
                  <Table.Head>{t('Message')}</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {table.rows.map((e) => {
                  const badge = LEVEL_BADGE[e.LEVEL] ?? LEVEL_BADGE.Info
                  return (
                    <Table.Row key={e.ID}>
                      <Table.Cell>
                        <Badge variant={badge.variant} appearance="dot">
                          {t(badge.label)}
                        </Badge>
                      </Table.Cell>
                      <Table.Cell className="tabular-nums">{e.DATE}</Table.Cell>
                      <Table.Cell className="tabular-nums text-kumo-subtle">{e.TIME}</Table.Cell>
                      <Table.Cell>{e.CATEGORY}</Table.Cell>
                      <Table.Cell className="min-w-96 whitespace-normal">{e.MESSAGE}</Table.Cell>
                    </Table.Row>
                  )
                })}
              </Table.Body>
            </Table>
          </TableCard>
        )}
      </div>

      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={t('Delete logs')}
        description={`${t('Are you sure you want to delete the logs for %s?', o.owner)} ${t("This can't be undone.")}`}
        confirmLabel={t('Delete logs')}
        destructive
        onConfirm={() =>
          m.clear(o.query).then(
            () => toast.add({ title: t('Logs deleted'), type: 'success' }),
            (err) => toast.add({ title: t('Delete logs'), description: apiErrorText(err, t), type: 'error' }),
          )
        }
      />
    </div>
  )
}

function AuthStatusBadge({ entry }: { entry: AuthLogEntry }) {
  const t = useT()
  return entry.STATUS === 'success' ? (
    <Badge variant="success" appearance="dot">
      {t('Success')}
    </Badge>
  ) : (
    <Badge variant="error" appearance="dot">
      {t('Failed')}
    </Badge>
  )
}

export function AuthLogPage() {
  const toast = useKumoToastManager()
  const t = useT()
  const o = useLogOwner()
  const auth = useAuthLog(o.query)
  const m = useLogMutation()
  const [status, setStatus] = useState('all')
  const [range, setRange] = useState<DateRange>(ALL_DATES)
  const [confirmOpen, setConfirmOpen] = useState(false)

  const entries = useMemo<AuthLogEntry[]>(() => auth.data?.entries ?? [], [auth.data])
  const byStatus = useMemo(
    () => entries.filter((e) => (status === 'all' || e.STATUS === status) && inRange(e.DATE, range)),
    [entries, status, range],
  )
  const failed = entries.filter((e) => e.STATUS === 'failed').length

  const table = useDataTable({
    rows: byStatus,
    getId: (e) => e.ID,
    search: (e, q) => e.IP.includes(q) || e.USER_AGENT.toLowerCase().includes(q),
    sorts: {
      date: (e) => `${e.DATE} ${e.TIME}`,
      ip: (e) => e.IP,
    },
    initialSort: { key: 'date', dir: 'desc' },
    pageSize: PAGE_SIZE,
  })

  return (
    <div className="grid gap-6">
      <PageHeader
        title={t('Login history')}
        description={
          failed > 0
            ? t('Sign-in attempts to the control panel, newest first. %s failed.', failed)
            : t('Sign-in attempts to the control panel, newest first.')
        }
        back={{ href: o.query ? `/logs?user=${encodeURIComponent(o.query)}` : '/logs', label: t('Logs') }}
        actions={
          <>
            <Button variant="secondary" icon={ArrowClockwiseIcon} onClick={() => void auth.refetch()}>
              {t('Refresh')}
            </Button>
            {o.canDelete && (
              <Button variant="secondary" icon={TrashIcon} disabled={entries.length === 0} onClick={() => setConfirmOpen(true)}>
                {t('Delete logs')}
              </Button>
            )}
          </>
        }
      />

      <div className="grid gap-3">
        <TableToolbar
          search={{
            label: t('Search IPs or browsers'),
            placeholder: t('Search IPs or browsers'),
            value: table.query, onChange: table.setQuery }}
          filters={
            <>
              <Select
                aria-label={t('Filter by status')}
                value={status}
                onValueChange={(v) => setStatus(String(v))}
                items={{ all: t('All attempts'), success: t('Successful'), failed: t('Failed') }}
                renderValue={(v) =>
                  v === 'all' ? (
                    <>
                      <span className="max-md:hidden">{t('All attempts')}</span>
                      <span className="md:hidden">{t('All')}</span>
                    </>
                  ) : v === 'success' ? (
                    t('Successful')
                  ) : (
                    t('Failed')
                  )
                }
                render={<Toolbar.Button />}
              />
              <DateRangeFilter value={range} onChange={setRange} />
              <UserFilter owner={o} />
            </>
          }
        />

        {auth.isPending ? (
          <DnsLoading />
        ) : auth.isError ? (
          <DnsError error={auth.error} />
        ) : (
          <TableCard
            noun={{ one: t('sign-in attempt'), other: t('sign-in attempts') }}
            filteredCount={table.filteredCount}
            totalCount={entries.length}
            page={table.page}
            pageSize={table.pageSize}
            onPageChange={table.setPage}
            onClearSearch={() => {
              table.setQuery('')
              setStatus('all')
              setRange(ALL_DATES)
            }}
            emptyState={{
              icon: <BinocularsIcon size={40} className="text-kumo-inactive" />,
              title: t('No sign-in attempts recorded'),
              description: t('Logins to the control panel will appear here.'),
            }}
          >
            <Table className="whitespace-nowrap">
              <Table.Header>
                <Table.Row>
                  <Table.Head>{t('Status')}</Table.Head>
                  <SortableHead sortKey="date" sort={table.sort} onSort={table.toggleSort}>
                    {t('Date')}
                  </SortableHead>
                  <Table.Head>{t('Time')}</Table.Head>
                  <SortableHead sortKey="ip" sort={table.sort} onSort={table.toggleSort}>
                    {t('IP address')}
                  </SortableHead>
                  <Table.Head>{t('Browser')}</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {table.rows.map((e) => (
                  <Table.Row key={e.ID}>
                    <Table.Cell>
                      <AuthStatusBadge entry={e} />
                    </Table.Cell>
                    <Table.Cell className="tabular-nums">{e.DATE}</Table.Cell>
                    <Table.Cell className="tabular-nums text-kumo-subtle">{e.TIME}</Table.Cell>
                    <Table.Cell>
                      <Text variant="mono" as="span">
                        {e.IP}
                      </Text>
                    </Table.Cell>
                    <Table.Cell>{e.USER_AGENT}</Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </Table>
          </TableCard>
        )}
      </div>

      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={t('Delete logs')}
        description={t('Are you sure you want to delete the login history for %s?', o.owner)}
        confirmLabel={t('Delete logs')}
        destructive
        onConfirm={() =>
          m.clearAuth(o.query).then(
            () => toast.add({ title: t('Login history deleted'), type: 'success' }),
            (err) => toast.add({ title: t('Delete logs'), description: apiErrorText(err, t), type: 'error' }),
          )
        }
      />
    </div>
  )
}
