import { Badge, Button, DropdownMenu, LinkButton, Select, Table, Text, Toolbar } from '@cloudflare/kumo'
import {
  ArrowRightIcon,
  ArrowsClockwiseIcon,
  NetworkIcon,
  PencilSimpleIcon,
  PlusIcon,
  TrashIcon,
} from '@phosphor-icons/react'
import { useMemo, useState } from 'react'
import { Link as RouterLink, useNavigate } from 'react-router'
import { useIps, useServerMutation } from '../../api/server'
import type { BulkFailure, IPRow, JobRef } from '../../api/types.gen'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { PageHeader } from '../../components/PageHeader'
import { SortableHead, TableCard, TableToolbar, rowLink, rowMenuTrigger, useDataTable } from '../../components/table'
import { useT } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { DnsError, DnsLoading } from '../dns/shared'
import { ErrorNote, JobPanel } from './parts'
import { failureText } from '../../api/errors'
import { shortcutTarget } from '../../shortcuts/targets'
import { useRowShortcuts } from '../../shortcuts/useRowShortcuts'

const ipPath = (ip: string) => `/server/ip/${encodeURIComponent(ip)}`

const statusFilters = { all: msgid('Show all'), shared: msgid('Shared'), dedicated: msgid('Dedicated') }

type Ask = { title: string; description: string; run: () => Promise<JobRef> }

export function IpListPage() {
  const navigate = useNavigate()
  const t = useT()
  const ips = useIps()
  const m = useServerMutation()
  const [status, setStatus] = useState<keyof typeof statusFilters>('all')
  const [ask, setAsk] = useState<Ask | null>(null)
  const [askOpen, setAskOpen] = useState(false)
  const [jobId, setJobId] = useState('')
  const [running, setRunning] = useState(false)
  const [error, setError] = useState<unknown>(null)

  const all = useMemo<IPRow[]>(() => ips.data?.ips ?? [], [ips.data])
  const byStatus = useMemo(() => (status === 'all' ? all : all.filter((d) => d.status === status)), [all, status])
  // The API sends the rows in the user's sort order; header clicks re-sort in the browser.
  const table = useDataTable({
    rows: byStatus,
    getId: (d) => d.ip,
    search: (d, q) =>
      d.ip.includes(q) ||
      d.owner.toLowerCase().includes(q) ||
      d.interface.includes(q) ||
      d.nat.includes(q) ||
      d.name.toLowerCase().includes(q),
    sorts: {
      date: (d) => `${d.date} ${d.time}`,
      ip: (d) => d.ip,
      netmask: (d) => d.netmask,
      interface: (d) => d.interface,
      domains: (d) => Number(d.webDomains),
      owner: (d) => d.owner,
    },
  })

  /** Starts a job route; a refusal shows in the banner, success hands the job to the panel. */
  const start = async (call: () => Promise<JobRef>) => {
    setError(null)
    setRunning(true)
    try {
      const ref = await call()
      setJobId(ref.jobId)
      table.clearSelection()
    } catch (err) {
      setError(err)
      setRunning(false)
    }
  }
  const askDelete = (names: string[], bulk: boolean) =>
    setAsk(
      !bulk
        ? {
            title: t('Delete IP address?'),
            description: `${t('Are you sure you want to delete IP address %s?', names[0])} ${t('Domains using it must be moved to another address first.')}`,
            run: () => m.deleteIp(names[0]),
          }
        : {
            title: names.length === 1 ? t('Delete IP address?') : t('Delete %s IP addresses?', names.length),
            description: t(
              'Are you sure you want to delete the selected IP addresses? Domains using them must be moved first.',
            ),
            run: () => m.bulkIps({ action: 'delete', names }),
          },
    )
  const confirmDelete = (names: string[], bulk = false) => {
    askDelete(names, bulk)
    setAskOpen(true)
  }

  // Hestia's IP list keys: Enter edit, Delete.
  const keys = useRowShortcuts(
    table.rows,
    (d) => d.ip,
    (d) => ({
      enter: () => navigate(ipPath(d.ip)),
      delete: running ? undefined : () => confirmDelete([d.ip]),
    }),
  )
  const sortHead = (key: string, label: string, align?: 'right', sticky?: 'left') => (
    <SortableHead sortKey={key} sort={table.sort} onSort={table.toggleSort} align={align} sticky={sticky}>
      {label}
    </SortableHead>
  )

  if (ips.error) return <DnsError error={ips.error} />
  if (!ips.data) return <DnsLoading />

  return (
    <div className="grid gap-6">
      <PageHeader
        title={t('IP addresses')}
        description={t("Addresses configured on this server's network interfaces, and who can use them.")}
        actions={
          <>
            <Button icon={ArrowsClockwiseIcon} disabled={running} onClick={() => start(() => m.refreshIps())}>
              {t('Refresh IP addresses')}
            </Button>
            <LinkButton href="/server/ip/new" variant="primary" icon={PlusIcon} {...shortcutTarget('add')}>
              {t('Add IP address')}
            </LinkButton>
          </>
        }
      />

      <ErrorNote error={error} />
      <JobPanel
        jobId={jobId}
        onDone={(status) => {
          setRunning(false)
          // A bulk delete ends done, with the items the core refused in its result
          const failed = (status.result as { failed?: BulkFailure[] } | null)?.failed ?? []
          if (failed.length > 0) setError(failed.map((f) => failureText(f, t)).join('\n'))
        }}
      />

      <div className="grid gap-3">
        <TableToolbar
          search={{
            label: t('Search IP addresses'),
            placeholder: t('Search addresses, interfaces or owners'),
            value: table.query,
            onChange: table.setQuery,
          }}
          filters={
            <Select
              aria-label={t('Filter by status')}
              value={status}
              onValueChange={(v) => setStatus(v as keyof typeof statusFilters)}
              items={Object.fromEntries(Object.entries(statusFilters).map(([k, v]) => [k, t(v)]))}
              render={<Toolbar.Button />}
            />
          }
          selectedCount={table.selectedCount}
          onClearSelection={table.clearSelection}
          bulkActions={
            <Button
              size="sm"
              variant="secondary-destructive"
              icon={TrashIcon}
              disabled={running}
              onClick={() =>
                confirmDelete(
                  table.selectedRows.map((d) => d.ip),
                  true,
                )
              }
            >
              {t('Delete')}
            </Button>
          }
        />

        <TableCard
          rowShortcuts={keys}
          noun={{ one: t('IP address'), other: t('IP addresses') }}
          filteredCount={table.filteredCount}
          totalCount={all.length}
          onClearSearch={() => {
            table.setQuery('')
            setStatus('all')
          }}
          emptyState={{
            icon: <NetworkIcon size={40} className="text-kumo-inactive" />,
            title: t('No IP addresses yet'),
            description: t('Add the address configured on your network interface to start hosting.'),
            action: (
              <LinkButton href="/server/ip/new" variant="primary" icon={PlusIcon}>
                {t('Add IP address')}
              </LinkButton>
            ),
          }}
        >
          <Table className="whitespace-nowrap">
            <Table.Header>
              <Table.Row>
                <Table.CheckHead
                  checked={table.allSelected}
                  indeterminate={table.someSelected && !table.allSelected}
                  onCheckedChange={table.toggleAll}
                  label={t('Select all IP addresses')}
                />
                {sortHead('ip', t('IP address'), undefined, 'left')}
                {sortHead('netmask', t('Netmask'))}
                {sortHead('interface', t('Interface'))}
                <Table.Head>{t('Status')}</Table.Head>
                {sortHead('domains', t('Domains'), 'right')}
                {sortHead('owner', t('Owner'))}
                <Table.Head sticky="right">
                  <span className="sr-only">{t('Actions')}</span>
                </Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {table.rows.map((d) => (
                <Table.Row
                  key={d.ip}
                  variant={table.isSelected(d.ip) ? 'selected' : 'default'}
                  {...rowLink(() => navigate(ipPath(d.ip)))}
                  {...keys.rowProps(d.ip)}
                >
                  <Table.CheckCell
                    checked={table.isSelected(d.ip)}
                    onCheckedChange={(_, details) => table.toggle(d.ip, details)}
                    label={t('Select %s', d.ip)}
                  />
                  <Table.Cell sticky="left">
                    <div className="flex flex-col gap-0.5">
                      <span className="flex items-center gap-1.5">
                        <RouterLink
                          to={ipPath(d.ip)}
                          className="font-mono font-medium text-kumo-default hover:underline"
                        >
                          {d.ip}
                        </RouterLink>
                        {d.nat && (
                          <>
                            <ArrowRightIcon className="text-kumo-subtle" aria-label={t('NAT to')} />
                            <Text variant="mono" as="span">
                              {d.nat}
                            </Text>
                          </>
                        )}
                      </span>
                      {d.name && (
                        <Text variant="secondary" size="sm" as="span">
                          {d.name}
                        </Text>
                      )}
                    </div>
                  </Table.Cell>
                  <Table.Cell>
                    <Text variant="mono" as="span">
                      {d.netmask}
                    </Text>
                  </Table.Cell>
                  <Table.Cell>
                    <Text variant="mono" as="span">
                      {d.interface}
                    </Text>
                  </Table.Cell>
                  <Table.Cell>
                    {d.status === 'shared' ? (
                      <Badge variant="blue">{t('Shared')}</Badge>
                    ) : (
                      <Badge variant="purple">{t('Dedicated')}</Badge>
                    )}
                  </Table.Cell>
                  <Table.Cell className="text-right tabular-nums">{d.webDomains}</Table.Cell>
                  <Table.Cell>{d.status === 'shared' ? '' : d.owner}</Table.Cell>
                  <Table.Cell sticky="right" className="text-right">
                    <DropdownMenu>
                      <DropdownMenu.Trigger render={rowMenuTrigger(t('Actions for %s', d.ip))} />
                      <DropdownMenu.Content align="end">
                        <DropdownMenu.Item icon={PencilSimpleIcon} onClick={() => navigate(ipPath(d.ip))}>
                          {t('Edit IP address')}
                        </DropdownMenu.Item>
                        <DropdownMenu.Separator />
                        <DropdownMenu.Item
                          icon={TrashIcon}
                          variant="danger"
                          disabled={running}
                          onClick={() => confirmDelete([d.ip])}
                        >
                          {t('Delete')}
                        </DropdownMenu.Item>
                      </DropdownMenu.Content>
                    </DropdownMenu>
                  </Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table>
        </TableCard>
      </div>

      <ConfirmDialog
        open={askOpen}
        onOpenChange={setAskOpen}
        title={ask?.title ?? ''}
        description={ask?.description ?? ''}
        confirmLabel={t('Delete')}
        destructive
        onConfirm={() => ask && void start(ask.run)}
      />
    </div>
  )
}
