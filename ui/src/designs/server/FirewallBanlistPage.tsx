import {
  Badge,
  Button,
  DropdownMenu,
  Empty,
  LinkButton,
  Select,
  Table,
  Text,
  Toolbar,
  useKumoToastManager,
} from '@cloudflare/kumo'
import { PlusIcon, ProhibitIcon, TrashIcon } from '@phosphor-icons/react'
import { useMemo, useState } from 'react'
import { useBans, useServerMutation } from '../../api/server'
import type { BanItem } from '../../api/types.gen'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { PageHeader } from '../../components/PageHeader'
import { SortableHead, TableCard, TableToolbar, rowMenuTrigger, useDataTable } from '../../components/table'
import { useT } from '../../i18n/I18nProvider'
import { useSessionUser } from '../../session/SessionProvider'
import { DnsError, DnsLoading } from '../dns/shared'
import { FirewallNav } from './FirewallNav'
import { banChains, ruleComment } from './firewallShared'
import { ErrorNote } from './parts'
import { failureText } from '../../api/errors'
import { shortcutTarget } from '../../shortcuts/targets'
import { useRowShortcuts } from '../../shortcuts/useRowShortcuts'

type Ban = BanItem & { time: string; date: string; id: string }

export function FirewallBanlistPage() {
  const user = useSessionUser()
  const t = useT()
  if (user && !user.features.firewallBans) {
    return (
      <div className="grid gap-6">
        <PageHeader title={t('Firewall')} />
        <FirewallNav current="/server/firewall/banlist" />
        <Empty
          icon={<ProhibitIcon size={40} className="text-kumo-inactive" />}
          title={t('Banned IP addresses are not available')}
          description={t('Install a firewall extension such as fail2ban to ban addresses.')}
          className="py-16"
        />
      </div>
    )
  }
  return <Bans />
}

function Bans() {
  const toast = useKumoToastManager()
  const t = useT()
  const bans = useBans()
  const m = useServerMutation()
  const [chain, setChain] = useState('all')
  const [toDelete, setToDelete] = useState<BanItem[]>([])
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)

  // The API sends every (ip, chain) row, newest first; the pair identifies a ban.
  const all = useMemo<Ban[]>(
    () =>
      (bans.data?.rows ?? []).map((b) => ({
        ip: b.IP,
        chain: b.CHAIN,
        time: b.TIME,
        date: b.DATE,
        id: `${b.IP} ${b.CHAIN}`,
      })),
    [bans.data],
  )
  const chains = useMemo(() => [...new Set([...banChains, ...all.map((b) => b.chain)])], [all])
  const byChain = useMemo(() => (chain === 'all' ? all : all.filter((b) => b.chain === chain)), [all, chain])
  const table = useDataTable({
    rows: byChain,
    getId: (b) => b.id,
    search: (b, q) => b.ip.includes(q),
    sorts: { ip: (b) => b.ip, date: (b) => `${b.date} ${b.time}` },
    pageSize: 20,
  })
  const askDelete = (items: BanItem[]) => {
    setToDelete(items)
    setDeleteOpen(true)
  }
  const unban = async () => {
    setError(null)
    setBusy(true)
    try {
      const reply = await m.deleteBans({ items: toDelete })
      if (reply.failed.length > 0) setError(reply.failed.map((f) => failureText(f, t)).join('\n'))
      else
        toast.add({
          title: toDelete.length === 1 ? t('%s unbanned', toDelete[0].ip) : t('IP addresses unbanned'),
          type: 'success',
        })
      table.clearSelection()
    } catch (err) {
      setError(err)
    } finally {
      setBusy(false)
    }
  }
  const pair = (b: Ban): BanItem => ({ ip: b.ip, chain: b.chain })
  // A ban has no edit page: Delete (unban) is its one row key.
  const keys = useRowShortcuts(
    table.rows,
    (b) => b.id,
    (b) => ({
      delete: busy ? undefined : () => askDelete([pair(b)]),
    }),
  )

  if (bans.error) return <DnsError error={bans.error} />
  if (!bans.data) return <DnsLoading />

  return (
    <div className="grid gap-6">
      <PageHeader
        title={t('Firewall')}
        description={t('Addresses blocked by fail2ban or by hand, per service chain.')}
        actions={
          <LinkButton href="/server/firewall/banlist/new" variant="primary" icon={PlusIcon} {...shortcutTarget('add')}>
            {t('Ban IP address')}
          </LinkButton>
        }
      />
      <FirewallNav current="/server/firewall/banlist" />
      <ErrorNote error={error} />

      <div className="grid gap-3">
        <TableToolbar
          search={{
            label: t('Search banned IP addresses'),
            placeholder: t('Search IP addresses'),
            value: table.query,
            onChange: table.setQuery,
          }}
          filters={
            <Select
              aria-label={t('Filter by chain')}
              value={chain}
              onValueChange={(v) => setChain(String(v))}
              items={{ all: t('All chains'), ...Object.fromEntries(chains.map((c) => [c, ruleComment(c)])) }}
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
              disabled={busy}
              onClick={() => askDelete(table.selectedRows.map(pair))}
            >
              {t('Delete')}
            </Button>
          }
        />

        <TableCard
          rowShortcuts={keys}
          noun={{ one: t('banned IP address'), other: t('banned IP addresses') }}
          filteredCount={table.filteredCount}
          totalCount={all.length}
          onClearSearch={() => {
            table.setQuery('')
            setChain('all')
          }}
          page={table.page}
          pageSize={table.pageSize}
          onPageChange={table.setPage}
          emptyState={{
            icon: <ProhibitIcon size={40} className="text-kumo-inactive" />,
            title: t('There are currently no banned IP addresses.'),
            description: t('Addresses that fail too many logins appear here automatically.'),
            action: (
              <LinkButton href="/server/firewall/banlist/new" variant="primary" icon={PlusIcon}>
                {t('Ban IP address')}
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
                  label={t('Select all banned IP addresses')}
                />
                <SortableHead sortKey="ip" sort={table.sort} onSort={table.toggleSort} sticky="left">
                  {t('IP address')}
                </SortableHead>
                <SortableHead sortKey="date" sort={table.sort} onSort={table.toggleSort}>
                  {t('Date')}
                </SortableHead>
                <Table.Head>{t('Time')}</Table.Head>
                <Table.Head>{t('Chain')}</Table.Head>
                <Table.Head sticky="right">
                  <span className="sr-only">{t('Actions')}</span>
                </Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {table.rows.map((b) => (
                <Table.Row
                  key={b.id}
                  variant={table.isSelected(b.id) ? 'selected' : 'default'}
                  {...keys.rowProps(b.id)}
                >
                  <Table.CheckCell
                    checked={table.isSelected(b.id)}
                    onCheckedChange={(_, details) => table.toggle(b.id, details)}
                    label={t('Select %s in %s', b.ip, b.chain)}
                  />
                  <Table.Cell sticky="left">
                    <Text variant="mono" as="span">
                      {b.ip}
                    </Text>
                  </Table.Cell>
                  <Table.Cell className="tabular-nums">{b.date}</Table.Cell>
                  <Table.Cell className="tabular-nums">{b.time}</Table.Cell>
                  <Table.Cell>
                    <Badge variant="neutral">{b.chain}</Badge>
                  </Table.Cell>
                  <Table.Cell sticky="right" className="text-right">
                    <DropdownMenu>
                      <DropdownMenu.Trigger render={rowMenuTrigger(t('Actions for %s in %s', b.ip, b.chain))} />
                      <DropdownMenu.Content align="end">
                        <DropdownMenu.Item
                          icon={TrashIcon}
                          variant="danger"
                          disabled={busy}
                          onClick={() => askDelete([pair(b)])}
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
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        title={toDelete.length === 1 ? t('Unban IP address?') : t('Unban %s IP addresses?', toDelete.length)}
        description={
          toDelete.length === 1
            ? t('Are you sure you want to delete IP address %s from the %s banlist?', toDelete[0].ip, toDelete[0].chain)
            : t('Are you sure you want to delete the selected IP addresses from the banlist?')
        }
        confirmLabel={t('Delete')}
        destructive
        onConfirm={() => void unban()}
      />
    </div>
  )
}
