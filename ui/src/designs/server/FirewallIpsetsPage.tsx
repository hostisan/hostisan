import { Badge, Button, DropdownMenu, Empty, LinkButton, Table, Text, useKumoToastManager } from '@cloudflare/kumo'
import { ListBulletsIcon, PlusIcon, ShieldSlashIcon, TrashIcon } from '@phosphor-icons/react'
import { useMemo, useState } from 'react'
import { useFirewall, useIpsets, useServerMutation } from '../../api/server'
import type { FirewallIpset } from '../../api/hestia.gen'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { PageHeader } from '../../components/PageHeader'
import { SortableHead, TableCard, TableToolbar, rowMenuTrigger, useDataTable } from '../../components/table'
import { useT } from '../../i18n/I18nProvider'
import { DnsError, DnsLoading } from '../dns/shared'
import { FirewallNav } from './FirewallNav'
import { ErrorNote } from './parts'
import { IpsetCountryMap } from './IpsetCountryMap'
import { failureText } from '../../api/errors'
import { shortcutTarget } from '../../shortcuts/targets'
import { useRowShortcuts } from '../../shortcuts/useRowShortcuts'

export function FirewallIpsetsPage() {
  const toast = useKumoToastManager()
  const t = useT()
  const ipsets = useIpsets()
  const firewall = useFirewall()
  const m = useServerMutation()
  const [toDelete, setToDelete] = useState<string[]>([])
  const [bulk, setBulk] = useState(false)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)

  const all = useMemo<FirewallIpset[]>(() => ipsets.data?.rows ?? [], [ipsets.data])
  const table = useDataTable({
    rows: all,
    getId: (l) => l.LISTNAME,
    search: (l, q) => l.LISTNAME.toLowerCase().includes(q) || l.SOURCE.toLowerCase().includes(q),
    sorts: { name: (l) => l.LISTNAME, date: (l) => `${l.DATE} ${l.TIME}` },
    initialSort: { key: 'name', dir: 'asc' },
  })
  const askDelete = (names: string[], isBulk: boolean) => {
    setToDelete(names)
    setBulk(isBulk)
    setDeleteOpen(true)
  }
  // The ipset list has no edit page: Delete is its one row key.
  const keys = useRowShortcuts(
    table.rows,
    (l) => l.LISTNAME,
    (l) => ({
      delete: () => askDelete([l.LISTNAME], false),
    }),
  )
  const remove = async () => {
    setError(null)
    setBusy(true)
    try {
      if (bulk) {
        const reply = await m.bulkIpsets({ action: 'delete', names: toDelete })
        if (reply.failed.length > 0) {
          setError(reply.failed.map((f) => failureText(f, t)).join('\n'))
          return
        }
      } else {
        await m.deleteIpset(toDelete[0])
      }
      toast.add({ title: t('IP list deleted'), type: 'success' })
      table.clearSelection()
    } catch (err) {
      setError(err)
    } finally {
      setBusy(false)
    }
  }

  if (ipsets.error) return <DnsError error={ipsets.error} />
  if (!ipsets.data) return <DnsLoading />
  const enabled = ipsets.data.enabled

  return (
    <div className="grid gap-6">
      <PageHeader
        title={t('Firewall')}
        description={t('Named lists of addresses that firewall rules can match with ipset:<name>.')}
        actions={
          enabled && (
            <LinkButton href="/server/firewall/ipsets/new" variant="primary" icon={PlusIcon} {...shortcutTarget('add')}>
              {t('Add IP list')}
            </LinkButton>
          )
        }
      />
      <FirewallNav current="/server/firewall/ipsets" />
      <ErrorNote error={error} />

      {!enabled ? (
        <Empty
          icon={<ShieldSlashIcon size={40} className="text-kumo-inactive" />}
          title={t('The firewall is disabled')}
          description={t('Turn the firewall on to manage IP lists.')}
          contents={<LinkButton href="/server/firewall">{t('Go to the firewall')}</LinkButton>}
          className="py-16"
        />
      ) : (
        <>
          <div className="grid gap-3">
            {all.length > 0 && (
              <TableToolbar
                search={{
                  label: t('Search IP lists'),
                  placeholder: t('Search names or sources'),
                  value: table.query,
                  onChange: table.setQuery,
                }}
                selectedCount={table.selectedCount}
                onClearSelection={table.clearSelection}
                bulkActions={
                  <Button
                    size="sm"
                    variant="secondary-destructive"
                    icon={TrashIcon}
                    disabled={busy}
                    onClick={() =>
                      askDelete(
                        table.selectedRows.map((l) => l.LISTNAME),
                        true,
                      )
                    }
                  >
                    {t('Delete')}
                  </Button>
                }
              />
            )}

            <TableCard
              rowShortcuts={keys}
              noun={{ one: t('IP list'), other: t('IP lists') }}
              filteredCount={table.filteredCount}
              totalCount={all.length}
              onClearSearch={() => table.setQuery('')}
              emptyState={{
                icon: <ListBulletsIcon size={40} className="text-kumo-inactive" />,
                title: t('There are currently no IP lists defined.'),
                description: t('Add a country list or a blocklist, then reference it from a firewall rule.'),
                action: (
                  <LinkButton href="/server/firewall/ipsets/new" variant="primary" icon={PlusIcon}>
                    {t('Add IP list')}
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
                      label={t('Select all IP lists')}
                    />
                    <SortableHead sortKey="name" sort={table.sort} onSort={table.toggleSort} sticky="left">
                      {t('IP list name')}
                    </SortableHead>
                    <Table.Head>{t('Auto update')}</Table.Head>
                    <Table.Head>{t('IP version')}</Table.Head>
                    <SortableHead sortKey="date" sort={table.sort} onSort={table.toggleSort}>
                      {t('Date')}
                    </SortableHead>
                    <Table.Head>{t('Time')}</Table.Head>
                    <Table.Head sticky="right">
                      <span className="sr-only">{t('Actions')}</span>
                    </Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {table.rows.map((l) => (
                    <Table.Row
                      key={l.LISTNAME}
                      variant={table.isSelected(l.LISTNAME) ? 'selected' : 'default'}
                      {...keys.rowProps(l.LISTNAME)}
                    >
                      <Table.CheckCell
                        checked={table.isSelected(l.LISTNAME)}
                        onCheckedChange={(_, details) => table.toggle(l.LISTNAME, details)}
                        label={t('Select %s', l.LISTNAME)}
                      />
                      <Table.Cell sticky="left">
                        <div className="flex flex-col gap-0.5">
                          <Text bold as="span">
                            {l.LISTNAME}
                          </Text>
                          <Text
                            variant="secondary"
                            size="sm"
                            as="span"
                            DANGEROUS_className="max-w-md truncate font-mono"
                          >
                            {l.SOURCE}
                          </Text>
                        </div>
                      </Table.Cell>
                      <Table.Cell>
                        {l.AUTOUPDATE === 'yes' ? (
                          <Badge variant="success" appearance="dot">
                            {t('Enabled')}
                          </Badge>
                        ) : (
                          <Badge variant="neutral" appearance="dot">
                            {t('Disabled')}
                          </Badge>
                        )}
                      </Table.Cell>
                      <Table.Cell>{l.IP_VERSION === 'v4' ? 'IPv4' : 'IPv6'}</Table.Cell>
                      <Table.Cell className="tabular-nums">{l.DATE}</Table.Cell>
                      <Table.Cell className="tabular-nums">{l.TIME}</Table.Cell>
                      <Table.Cell sticky="right" className="text-right">
                        <DropdownMenu>
                          <DropdownMenu.Trigger render={rowMenuTrigger(t('Actions for %s', l.LISTNAME))} />
                          <DropdownMenu.Content align="end">
                            <DropdownMenu.Item
                              icon={TrashIcon}
                              variant="danger"
                              disabled={busy}
                              onClick={() => askDelete([l.LISTNAME], false)}
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

          <IpsetCountryMap lists={all} rules={firewall.data?.rules ?? []} />
        </>
      )}

      <ConfirmDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        title={toDelete.length === 1 ? t('Delete IP list?') : t('Delete %s IP lists?', toDelete.length)}
        description={
          toDelete.length === 1
            ? `${t('Are you sure you want to delete IP list %s?', toDelete[0])} ${t('Rules that use it stop matching.')}`
            : t('Are you sure you want to delete the selected IP lists? Rules that use them stop matching.')
        }
        confirmLabel={t('Delete')}
        destructive
        onConfirm={() => void remove()}
      />
    </div>
  )
}
