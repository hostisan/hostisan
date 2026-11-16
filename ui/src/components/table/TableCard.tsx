import { Button, Empty, LayerCard, Pagination, Text } from '@cloudflare/kumo'
import { MagnifyingGlassIcon } from '@phosphor-icons/react'
import { useEffect, useRef, type ReactNode } from 'react'
import { matches } from '../../shortcuts/keys'
import { useT } from '../../i18n/I18nProvider'
import { useShortcutHandler } from '../../shortcuts/ShortcutsProvider'
import type { RowShortcuts } from '../../shortcuts/useRowShortcuts'
import { countNoun, type Noun } from './countNoun'

type TableCardProps = {
  /** The <Table>. Rendered inside a horizontal scroll area so sticky columns work. */
  children: ReactNode
  /** Rows after search/filter, and before any search. */
  filteredCount: number
  totalCount: number
  /** Noun for the footer and empty states, translated by the caller: "web domains", or { one, other } for a singular count. */
  noun: Noun
  /** Shown when the list has no rows at all (not a search miss). */
  emptyState: { icon: ReactNode; title: string; description?: string; action?: ReactNode }
  /** Lets the "no matches" state offer to clear the search. */
  onClearSearch?: () => void
  page?: number
  pageSize?: number
  onPageChange?: (page: number) => void
  /** Keyboard row focus from useRowShortcuts; the rows spread its rowProps(id). */
  rowShortcuts?: RowShortcuts
}

// The keyboard-focused row, in Kumo's focus colour and with no transition. Each cell
// draws its own part of the outline (top and bottom, plus the outer edge on the first
// and last cell), and so do the fades of the sticky cells. A ring on the <tr> is painted
// over by the opaque sticky name and actions cells.
// Written out in full: Tailwind only finds class names it can read in the source.
const focusRing = [
  '[&_[data-row-focused]>*]:shadow-[inset_0_2px_0_var(--color-kumo-brand),inset_0_-2px_0_var(--color-kumo-brand)]',
  '[&_[data-row-focused]>*:first-child]:shadow-[inset_2px_0_0_var(--color-kumo-brand),inset_0_2px_0_var(--color-kumo-brand),inset_0_-2px_0_var(--color-kumo-brand)]',
  '[&_[data-row-focused]>*:last-child]:shadow-[inset_-2px_0_0_var(--color-kumo-brand),inset_0_2px_0_var(--color-kumo-brand),inset_0_-2px_0_var(--color-kumo-brand)]',
  '[&_[data-row-focused]>*]:before:shadow-[inset_0_2px_0_var(--color-kumo-brand),inset_0_-2px_0_var(--color-kumo-brand)]',
].join(' ')

/**
 * Card around a list table: horizontal scroll, the two empty states (nothing yet vs.
 * no matches), and a footer with the count and, for long lists, pagination.
 */
export function TableCard({
  children,
  filteredCount,
  totalCount,
  noun,
  emptyState,
  onClearSearch,
  page = 1,
  pageSize,
  onPageChange,
  rowShortcuts,
}: TableCardProps) {
  const t = useT()
  const scroller = useRef<HTMLDivElement>(null)
  // Ctrl+Shift+A toggles every row through the table's own select-all checkbox (Hestia
  // listed it but it never worked). Lists without checkboxes leave the key alone.
  useShortcutHandler((e) => {
    if (!matches(e, { key: 'a', ctrl: true, shift: true })) return false
    const all = scroller.current?.querySelector<HTMLElement>('thead [role="checkbox"], thead input[type="checkbox"]')
    all?.click()
    return !!all
  })

  const focusedId = rowShortcuts?.focusedId ?? null
  useEffect(() => {
    if (focusedId === null) return
    scroller.current?.querySelector('[data-row-focused]')?.scrollIntoView({ block: 'nearest' })
  }, [focusedId])

  // Kumo's Empty draws its own card; inside ours it would double the border.
  const flatEmpty = 'rounded-none border-0 bg-transparent py-12'

  if (totalCount === 0) {
    return (
      <LayerCard className="p-0">
        <Empty
          icon={emptyState.icon}
          title={emptyState.title}
          description={emptyState.description}
          contents={emptyState.action}
          className={flatEmpty}
        />
      </LayerCard>
    )
  }

  if (filteredCount === 0) {
    return (
      <LayerCard className="p-0">
        <Empty
          icon={<MagnifyingGlassIcon size={40} className="text-kumo-inactive" />}
          title={t('No %s match your search', countNoun(2, noun))}
          description={t('Check the spelling or try a shorter search.')}
          contents={onClearSearch && <Button onClick={onClearSearch}>{t('Clear search')}</Button>}
          className={flatEmpty}
        />
      </LayerCard>
    )
  }

  const paged = pageSize !== undefined && filteredCount > pageSize
  const from = paged ? (page - 1) * pageSize + 1 : 1
  const to = paged ? Math.min(page * pageSize, filteredCount) : filteredCount

  return (
    <LayerCard className="p-0">
      <div ref={scroller} className={`overflow-x-auto ${focusRing}`}>
        {children}
      </div>
      <div className="flex flex-wrap items-center justify-between gap-3 border-t border-kumo-hairline px-4 py-2.5">
        <Text variant="secondary" as="span">
          {paged ? t('%s–%s of %s', from, to, filteredCount) : filteredCount}
          {filteredCount !== totalCount ? ` ${t('(of %s)', totalCount)}` : ''}{' '}
          {countNoun(paged ? 2 : filteredCount, noun)}
        </Text>
        {paged && onPageChange && (
          <Pagination
            page={page}
            setPage={onPageChange}
            perPage={pageSize}
            totalCount={filteredCount}
            controls="simple"
          />
        )}
      </div>
    </LayerCard>
  )
}
