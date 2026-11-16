import { Button, InputGroup, Text, Toolbar } from '@cloudflare/kumo'
import { ArrowsClockwiseIcon, MagnifyingGlassIcon, XIcon } from '@phosphor-icons/react'
import { useState, type ReactNode } from 'react'
import { useT } from '../../i18n/I18nProvider'

type TableToolbarProps = {
  search?: { label: string; placeholder?: string; value: string; onChange: (value: string) => void }
  /** Filter / view controls joined to the search, e.g. <Select render={<Toolbar.Button />} />. */
  filters?: ReactNode
  /** Number of selected rows; when > 0 the toolbar becomes the bulk-action bar. */
  selectedCount?: number
  onClearSelection?: () => void
  /** Bulk action buttons shown while rows are selected. */
  bulkActions?: ReactNode
  /** Adds a refresh button, for lists that change on their own (services, logs, queues). */
  onRefresh?: () => void | Promise<void>
}

/**
 * Cloudflare-style list toolbar: one joined bar with search and filters. While rows
 * are selected it swaps to a same-height bar of bulk actions, so nothing shifts.
 */
export function TableToolbar({
  search,
  filters,
  selectedCount = 0,
  onClearSelection,
  bulkActions,
  onRefresh,
}: TableToolbarProps) {
  const t = useT()
  const [refreshing, setRefreshing] = useState(false)
  const refresh = async () => {
    setRefreshing(true)
    // Keep the spinner up long enough to read as feedback even when data is local.
    await Promise.all([onRefresh?.(), new Promise((r) => setTimeout(r, 600))])
    setRefreshing(false)
  }
  if (selectedCount > 0) {
    return (
      <div
        role="region"
        aria-label={t('Bulk actions')}
        className="flex min-h-9 flex-wrap items-center gap-2 rounded-lg bg-kumo-tint px-3 py-0.5 ring ring-kumo-line"
      >
        <Text bold as="span">
          {t('%s selected', selectedCount)}
        </Text>
        <Button variant="ghost" size="sm" icon={XIcon} onClick={onClearSelection}>
          {t('Clear')}
        </Button>
        <div className="ml-auto flex flex-wrap items-center gap-2">{bulkActions}</div>
      </div>
    )
  }
  return (
    <Toolbar className="w-full">
      {search && (
        <Toolbar.InputGroup aria-label={search.label} className="min-w-0 flex-1">
          <InputGroup.Addon>
            <MagnifyingGlassIcon />
          </InputGroup.Addon>
          <InputGroup.Input
            placeholder={search.placeholder ?? t('Search')}
            value={search.value}
            onChange={(e) => search.onChange(e.target.value)}
          />
        </Toolbar.InputGroup>
      )}
      {filters}
      {onRefresh && (
        <Toolbar.Button
          aria-label={t('Refresh')}
          disabled={refreshing}
          onClick={refresh}
          icon={<ArrowsClockwiseIcon size={16} className={refreshing ? 'animate-spin' : undefined} />}
        />
      )}
    </Toolbar>
  )
}
