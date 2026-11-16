import { Table } from '@cloudflare/kumo'
import { CaretDownIcon, CaretUpDownIcon, CaretUpIcon } from '@phosphor-icons/react'
import type { ReactNode } from 'react'
import type { SortState } from './useDataTable'

type SortableHeadProps = {
  sortKey: string
  sort: SortState | undefined
  onSort: (key: string) => void
  align?: 'left' | 'right'
  sticky?: 'left' | 'right'
  className?: string
  children: ReactNode
}

/** Column header that sorts on click; announces the order via aria-sort. */
export function SortableHead({ sortKey, sort, onSort, align = 'left', sticky, className, children }: SortableHeadProps) {
  const active = sort?.key === sortKey
  const Icon = !active ? CaretUpDownIcon : sort.dir === 'asc' ? CaretUpIcon : CaretDownIcon
  return (
    <Table.Head
      sticky={sticky}
      aria-sort={active ? (sort.dir === 'asc' ? 'ascending' : 'descending') : 'none'}
      className={className}
    >
      <button
        type="button"
        onClick={() => onSort(sortKey)}
        className={`-mx-1 flex items-center gap-1 rounded px-1 hover:text-kumo-strong ${align === 'right' ? 'ml-auto flex-row-reverse' : ''}`}
      >
        {children}
        <Icon size={12} className={active ? 'text-kumo-default' : 'text-kumo-subtle'} aria-hidden />
      </button>
    </Table.Head>
  )
}
