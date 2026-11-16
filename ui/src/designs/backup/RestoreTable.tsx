import { Button, Table, Text, useKumoToastManager } from '@cloudflare/kumo'
import {
  ArrowCounterClockwiseIcon,
  BookOpenIcon,
  ClockIcon,
  DatabaseIcon,
  EnvelopeSimpleIcon,
  FolderOpenIcon,
  GlobeIcon,
  PackageIcon,
  type Icon,
} from '@phosphor-icons/react'
import { useState } from 'react'
import { apiErrorText } from '../../api/errors'
import type { QueuedReply, RestoreRequest } from '../../api/types.gen'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { TableCard, TableToolbar, useDataTable } from '../../components/table'
import { useT, type Translate } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'

export type RestoreType = 'web' | 'dns' | 'mail' | 'db' | 'cron' | 'udir'

export type RestoreItem = { type: RestoreType; object: string }

export const restoreGroups: Record<RestoreType, { label: string; icon: Icon }> = {
  web: { label: msgid('Web domains'), icon: GlobeIcon },
  dns: { label: msgid('DNS zones'), icon: BookOpenIcon },
  mail: { label: msgid('Mail domains'), icon: EnvelopeSimpleIcon },
  db: { label: msgid('Databases'), icon: DatabaseIcon },
  cron: { label: msgid('Cron jobs'), icon: ClockIcon },
  udir: { label: msgid('User directories'), icon: FolderOpenIcon },
}

const split = (v: string) => v.split(',').filter(Boolean)

/**
 * Expands a Hestia backup record (comma-separated WEB, DNS, MAIL, DB, CRON, UDIR) into restorable
 * items, in the order of the detail table: web, mail, dns, db, cron, udir.
 */
export function restoreItems(
  b: { WEB: string; DNS: string; MAIL: string; DB: string; CRON: string; UDIR: string },
  types: RestoreType[] = ['web', 'mail', 'dns', 'db', 'cron', 'udir'],
): RestoreItem[] {
  const map: Record<RestoreType, string[]> = {
    web: split(b.WEB),
    dns: split(b.DNS),
    mail: split(b.MAIL),
    db: split(b.DB),
    // CRON is a count; Hestia omits the row for 0
    cron: b.CRON && b.CRON !== '0' ? ['records'] : [],
    udir: split(b.UDIR),
  }
  return types.flatMap((type) => map[type].map((object) => ({ type, object })))
}

const keyOf = (i: RestoreItem) => `${i.type}:${i.object}`
const nameOf = (i: RestoreItem, t: Translate) => (i.type === 'cron' ? t('Jobs') : i.object)

/** The restore body for a set of picked rows. */
export function restoreRequest(items: RestoreItem[]): RestoreRequest {
  const of = (type: RestoreType) => items.filter((i) => i.type === type).map((i) => i.object)
  return {
    all: false,
    web: of('web'),
    dns: of('dns'),
    mail: of('mail'),
    db: of('db'),
    udir: of('udir'),
    cron: items.some((i) => i.type === 'cron'),
  }
}

/**
 * Below sm the Restore column scrolls with the row: pinned, it and its fade covered the end of the
 * names at 390 px. The icon-only button and the wrapping type label let the table fit instead.
 */
const unstickNarrow = 'max-sm:static max-sm:before:hidden'

type Pending = { title: string; description: string; items: RestoreItem[] } | null

/**
 * Grouped restore list used by both the tar backup detail and the restic snapshot detail.
 * Rows stay in Hestia's group order (no sorting); search and bulk restore are kept from Hestia.
 */
export function RestoreTable({
  items,
  restoreLabel,
  restore,
  readOnly = false,
}: {
  items: RestoreItem[]
  /** Translated by the caller; defaults to Restore. */
  restoreLabel?: string
  /** Queues the restore of the picked rows. */
  restore: (body: RestoreRequest) => Promise<QueuedReply>
  readOnly?: boolean
}) {
  const toast = useKumoToastManager()
  const t = useT()
  const bulkLabel = restoreLabel ?? t('Restore')
  const [pending, setPending] = useState<Pending>(null)
  const [open, setOpen] = useState(false)
  const table = useDataTable({
    rows: items,
    getId: keyOf,
    search: (i, q) => i.object.toLowerCase().includes(q) || t(restoreGroups[i.type].label).toLowerCase().includes(q),
  })

  const confirm = (p: Pending) => {
    setPending(p)
    setOpen(true)
  }

  return (
    <div className="grid gap-3">
      <TableToolbar
        search={{
          label: t('Search backup contents'),
          placeholder: t('Search domains, databases or directories'),
          value: table.query,
          onChange: table.setQuery,
        }}
        selectedCount={readOnly ? 0 : table.selectedCount}
        onClearSelection={table.clearSelection}
        bulkActions={
          <Button
            size="sm"
            icon={ArrowCounterClockwiseIcon}
            onClick={() =>
              confirm({
                items: table.selectedRows,
                title: bulkLabel,
                description: t(
                  'Are you sure you want to restore %s?',
                  table.selectedCount === 1
                    ? nameOf(table.selectedRows[0], t)
                    : t('%s selected items', table.selectedCount),
                ),
              })
            }
          >
            {bulkLabel}
          </Button>
        }
      />

      <TableCard
        noun={t('items')}
        filteredCount={table.filteredCount}
        totalCount={items.length}
        onClearSearch={() => table.setQuery('')}
        emptyState={{
          icon: <PackageIcon size={40} className="text-kumo-inactive" />,
          title: t('This backup is empty'),
          description: t('No web domains, mail, databases or cron jobs were included.'),
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
                  label={t('Select all items')}
                />
              )}
              <Table.Head>{t('Type')}</Table.Head>
              <Table.Head className="w-full">{t('Details')}</Table.Head>
              <Table.Head sticky="right" className={unstickNarrow}>
                <span className="sr-only">{t('Restore')}</span>
              </Table.Head>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {table.rows.map((r, idx) => {
              const k = keyOf(r)
              const { icon: GroupIcon } = restoreGroups[r.type]
              const label = t(restoreGroups[r.type].label)
              const firstOfGroup = idx === 0 || table.rows[idx - 1].type !== r.type
              return (
                <Table.Row key={k} variant={table.isSelected(k) ? 'selected' : 'default'}>
                  {!readOnly && (
                    <Table.CheckCell
                      checked={table.isSelected(k)}
                      onCheckedChange={(_, details) => table.toggle(k, details)}
                      label={t('Select %s', nameOf(r, t))}
                    />
                  )}
                  <Table.Cell className="max-sm:whitespace-normal">
                    {firstOfGroup ? (
                      <span className="flex items-start gap-2">
                        <span className="flex h-lh items-center">
                          <GroupIcon className="text-kumo-subtle" />
                        </span>
                        {label}
                      </span>
                    ) : (
                      <span className="sr-only">{label}</span>
                    )}
                  </Table.Cell>
                  {/* The names are the point of the page: wrap them instead of clipping at 390 px. */}
                  <Table.Cell data-testid="restore-details" className="whitespace-normal [overflow-wrap:anywhere]">
                    <Text bold as="span">
                      {nameOf(r, t)}
                    </Text>
                  </Table.Cell>
                  <Table.Cell sticky="right" className={`text-right ${unstickNarrow}`}>
                    {!readOnly && (
                      <Button
                        size="sm"
                        icon={ArrowCounterClockwiseIcon}
                        onClick={() =>
                          confirm({
                            items: [r],
                            title: t('Restore'),
                            description:
                              r.type === 'cron'
                                ? t('Are you sure you want to restore cron jobs?')
                                : t('Are you sure you want to restore %s?', r.object),
                          })
                        }
                      >
                        <span className="max-sm:sr-only">{t('Restore')}</span>
                      </Button>
                    )}
                  </Table.Cell>
                </Table.Row>
              )
            })}
          </Table.Body>
        </Table>
      </TableCard>

      <ConfirmDialog
        open={open}
        onOpenChange={setOpen}
        title={pending?.title ?? t('Restore')}
        description={pending?.description ?? ''}
        confirmLabel={t('Restore')}
        onConfirm={() => {
          if (!pending) return
          restore(restoreRequest(pending.items)).then(
            (r) => toast.add({ title: t('Restore scheduled'), description: t(r.message), type: 'success' }),
            (err) =>
              toast.add({
                title: t('Restore failed'),
                description: <span className="whitespace-pre-line">{apiErrorText(err, t)}</span>,
                type: 'error',
              }),
          )
          table.clearSelection()
        }}
      />
    </div>
  )
}
