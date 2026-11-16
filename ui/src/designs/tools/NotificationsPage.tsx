import { Badge, Button, DropdownMenu, Table, Tabs, Text, useKumoToastManager } from '@cloudflare/kumo'
import { BellSlashIcon, CheckIcon, ChecksIcon, TrashIcon } from '@phosphor-icons/react'
import { useMemo, useState } from 'react'
import { useNotificationMutation, useNotifications } from '../../api/account'
import { apiErrorText } from '../../api/errors'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { PageHeader } from '../../components/PageHeader'
import { SafeNotice } from '../../components/SafeNotice'
import { SortableHead, TableCard, TableToolbar, rowMenuTrigger, useDataTable } from '../../components/table'
import { useT } from '../../i18n/I18nProvider'
import { useSessionUser } from '../../session/SessionProvider'
import { DnsError, DnsLoading } from '../dns/shared'

type Filter = 'all' | 'unread'
type Pending = { kind: 'all' } | { kind: 'selected'; ids: string[] } | null

export function NotificationsPage() {
  const toast = useKumoToastManager()
  const t = useT()
  const readOnly = useSessionUser()?.readOnly ?? false
  const list = useNotifications()
  const m = useNotificationMutation()
  const [filter, setFilter] = useState<Filter>('all')
  const [pending, setPending] = useState<Pending>(null)
  const [confirmOpen, setConfirmOpen] = useState(false)

  // The API's order: priority, then newest first.
  const all = useMemo(() => list.data?.items ?? [], [list.data])
  const unread = all.filter((n) => n.ACK === 'no').length
  const rows = useMemo(() => (filter === 'unread' ? all.filter((n) => n.ACK === 'no') : all), [all, filter])

  const table = useDataTable({
    rows,
    getId: (n) => n.ID,
    search: (n, q) => n.TOPIC.toLowerCase().includes(q) || n.NOTICE.toLowerCase().includes(q),
    sorts: {
      topic: (n) => n.TOPIC,
      received: (n) => `${n.DATE} ${n.TIME}`,
    },
    pageSize: 20,
  })

  const fail = (err: unknown) =>
    toast.add({ title: t('Notifications'), description: apiErrorText(err, t), type: 'error' })
  // One call at a time: each rewrites the same notifications file on the server.
  const each = async (ids: string[], call: (id: string) => Promise<unknown>) => {
    for (const id of ids) await call(id)
  }
  const acknowledge = (ids: string[] | 'all') =>
    (ids === 'all' ? m.ackAll() : each(ids, m.ack)).then(
      () => toast.add({ title: ids === 'all' ? t('All notifications marked as read') : t('Marked as read'), type: 'success' }),
      fail,
    )
  const remove = (ids: string[] | 'all') =>
    (ids === 'all' ? m.removeAll() : each(ids, m.remove)).then(
      () => toast.add({ title: ids === 'all' ? t('Notifications deleted') : t('Notification deleted'), type: 'success' }),
      fail,
    )

  const askDelete = (p: Pending) => {
    setPending(p)
    setConfirmOpen(true)
  }

  const selectedIds = table.selectedRows.map((n) => n.ID)
  const caughtUp = filter === 'unread' && all.length > 0

  return (
    <div className="grid gap-6">
      <PageHeader
        title={t('Notifications')}
        description={t('Updates, backups, certificate renewals and quota warnings from the server.')}
        actions={
          !readOnly && (
            <>
              <Button icon={ChecksIcon} disabled={unread === 0} onClick={() => acknowledge('all')}>
                {t('Mark all as read')}
              </Button>
              <Button
                variant="secondary-destructive"
                icon={TrashIcon}
                disabled={all.length === 0}
                onClick={() => askDelete({ kind: 'all' })}
              >
                {t('Delete all')}
              </Button>
            </>
          )
        }
      />

      <div className="grid gap-3">
        <div className="flex flex-wrap items-center gap-2">
          <Tabs
            variant="segmented"
            value={filter}
            onValueChange={(v) => setFilter(v as Filter)}
            tabs={[
              { value: 'all', label: t('All (%s)', all.length) },
              { value: 'unread', label: t('Unread (%s)', unread) },
            ]}
          />
          <div className="min-w-0 flex-1 basis-72">
            <TableToolbar
              search={{
                label: t('Search notifications'),
                placeholder: t('Search notifications'),
                value: table.query,
                onChange: table.setQuery,
              }}
              onRefresh={() => void list.refetch()}
              selectedCount={readOnly ? 0 : table.selectedCount}
              onClearSelection={table.clearSelection}
              bulkActions={
                <>
                  <Button
                    size="sm"
                    icon={CheckIcon}
                    onClick={() => {
                      void acknowledge(selectedIds)
                      table.clearSelection()
                    }}
                  >
                    {t('Mark as read')}
                  </Button>
                  <Button
                    size="sm"
                    variant="secondary-destructive"
                    icon={TrashIcon}
                    onClick={() => askDelete({ kind: 'selected', ids: selectedIds })}
                  >
                    {t('Delete')}
                  </Button>
                </>
              }
            />
          </div>
        </div>

        {list.isPending ? (
          <DnsLoading />
        ) : list.isError ? (
          <DnsError error={list.error} />
        ) : (
          <TableCard
            noun={{ one: t('notification'), other: t('notifications') }}
            filteredCount={table.filteredCount}
            totalCount={rows.length}
            page={table.page}
            pageSize={table.pageSize}
            onPageChange={table.setPage}
            onClearSearch={() => table.setQuery('')}
            emptyState={{
              icon: <BellSlashIcon size={40} className="text-kumo-inactive" />,
              title: caughtUp ? t('No unread notifications') : t('No notifications'),
              description: caughtUp ? t("You're all caught up.") : t('Server events and alerts will show up here.'),
              action: caughtUp ? <Button onClick={() => setFilter('all')}>{t('Show all')}</Button> : undefined,
            }}
          >
            <Table>
              <Table.Header>
                <Table.Row>
                  {!readOnly && (
                    <Table.CheckHead
                      checked={table.allSelected}
                      indeterminate={table.someSelected && !table.allSelected}
                      onCheckedChange={table.toggleAll}
                      label={t('Select all notifications')}
                    />
                  )}
                  <SortableHead sortKey="topic" sort={table.sort} onSort={table.toggleSort}>
                    {t('Notification')}
                  </SortableHead>
                  <SortableHead
                    sortKey="received"
                    sort={table.sort}
                    onSort={table.toggleSort}
                    className="whitespace-nowrap"
                  >
                    {t('Received')}
                  </SortableHead>
                  {!readOnly && (
                    <Table.Head sticky="right" className="w-px text-right">
                      <span className="sr-only">{t('Actions')}</span>
                    </Table.Head>
                  )}
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {table.rows.map((n) => (
                  <Table.Row key={n.ID} variant={table.isSelected(n.ID) ? 'selected' : 'default'}>
                    {!readOnly && (
                      <Table.CheckCell
                        checked={table.isSelected(n.ID)}
                        onCheckedChange={(_, details) => table.toggle(n.ID, details)}
                        label={t('Select %s', n.TOPIC)}
                      />
                    )}
                    <Table.Cell className="min-w-64">
                      <div className="flex flex-col gap-0.5">
                        <div className="flex flex-wrap items-center gap-2">
                          <Text bold={n.ACK === 'no'} as="span">
                            {n.TOPIC}
                          </Text>
                          {n.ACK === 'no' && <Badge variant="info">{t('New')}</Badge>}
                        </div>
                        <SafeNotice html={n.NOTICE} className="whitespace-normal text-kumo-subtle" />
                      </div>
                    </Table.Cell>
                    <Table.Cell className="whitespace-nowrap tabular-nums">
                      <div className="flex flex-col gap-0.5">
                        <span>{n.DATE}</span>
                        <Text variant="secondary" size="sm" as="span">
                          {n.TIME}
                        </Text>
                      </div>
                    </Table.Cell>
                    {!readOnly && (
                      <Table.Cell sticky="right" className="text-right">
                        <DropdownMenu>
                          <DropdownMenu.Trigger render={rowMenuTrigger(t('Actions for %s', n.TOPIC))} />
                          <DropdownMenu.Content align="end">
                            <DropdownMenu.Item
                              icon={CheckIcon}
                              disabled={n.ACK === 'yes'}
                              onClick={() => void acknowledge([n.ID])}
                            >
                              {t('Mark as read')}
                            </DropdownMenu.Item>
                            <DropdownMenu.Separator />
                            <DropdownMenu.Item icon={TrashIcon} variant="danger" onClick={() => void remove([n.ID])}>
                              {t('Delete')}
                            </DropdownMenu.Item>
                          </DropdownMenu.Content>
                        </DropdownMenu>
                      </Table.Cell>
                    )}
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
        title={
          pending?.kind === 'selected'
            ? t('Delete %s notifications?', pending.ids.length)
            : t('Delete all notifications?')
        }
        description={
          pending?.kind === 'selected'
            ? t('The selected notifications will be removed. This cannot be undone.')
            : t('All %s notifications will be removed, including unread ones. This cannot be undone.', all.length)
        }
        confirmLabel={pending?.kind === 'selected' ? t('Delete') : t('Delete all')}
        destructive
        onConfirm={() => {
          void remove(pending?.kind === 'selected' ? pending.ids : 'all')
          table.clearSelection()
        }}
      />
    </div>
  )
}
