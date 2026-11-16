import { useMemo, useState } from 'react'

/**
 * What a row checkbox's onCheckedChange passes as its second argument (Kumo's
 * CheckboxChangeEventDetails fits): only the native event's shiftKey is read.
 */
export type ToggleDetails = { event?: { shiftKey?: boolean } | Event }

export type SortDir = 'asc' | 'desc'
export type SortState = { key: string; dir: SortDir }
type SortValue = string | number

type Options<T> = {
  rows: T[]
  getId: (row: T) => string
  /** Return true when the row matches the (lower-cased, trimmed) query. */
  search?: (row: T, query: string) => boolean
  /** Sortable columns: key -> value to compare. */
  sorts?: Record<string, (row: T) => SortValue>
  initialSort?: SortState
  /** Rows per page; omit for lists that never get long. */
  pageSize?: number
  /** Rows that may be selected (default all); others stay out of ranges and select-all. */
  canSelect?: (row: T) => boolean
}

/**
 * Client-side search, sort, selection and paging for a list table. Selection is
 * kept by row id and only counts rows that match the current search, so bulk
 * actions never touch rows the user can't see.
 */
export function useDataTable<T>({ rows, getId, search, sorts = {}, initialSort, pageSize, canSelect }: Options<T>) {
  const [query, setQueryState] = useState('')
  const [sort, setSort] = useState<SortState | undefined>(initialSort)
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())
  const [page, setPage] = useState(1)
  // The row last clicked, for shift-click ranges (decision 32); reset with the order.
  const [anchor, setAnchor] = useState<string | undefined>()

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    const matched = q && search ? rows.filter((r) => search(r, q)) : rows
    const value = sort && sorts[sort.key]
    if (!sort || !value) return matched
    const dir = sort.dir === 'asc' ? 1 : -1
    return [...matched].sort((a, b) => {
      const va = value(a)
      const vb = value(b)
      const cmp =
        typeof va === 'number' && typeof vb === 'number'
          ? va - vb
          : String(va).localeCompare(String(vb), undefined, { numeric: true })
      return cmp * dir
    })
    // sorts is a fresh object each render; the sort key/dir is what matters.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rows, query, sort, search])

  const pageCount = pageSize ? Math.max(1, Math.ceil(filtered.length / pageSize)) : 1
  const currentPage = Math.min(page, pageCount)
  const visibleRows = pageSize
    ? filtered.slice((currentPage - 1) * pageSize, currentPage * pageSize)
    : filtered

  const filteredIds = filtered.map(getId)
  const selectableIds = canSelect ? filtered.filter(canSelect).map(getId) : filteredIds
  const selectedRows = filtered.filter((r) => selectedIds.has(getId(r)))
  const allSelected = selectableIds.length > 0 && selectableIds.every((id) => selectedIds.has(id))
  const someSelected = selectedRows.length > 0

  return {
    query,
    setQuery: (q: string) => {
      setQueryState(q)
      setPage(1)
      setAnchor(undefined)
    },
    sort,
    /** Sets the sort outright (a menu item that names a direction). */
    setSort: (next: SortState) => {
      setAnchor(undefined)
      setSort(next)
    },
    /** Header click: a new column sorts ascending; clicking the sorted column flips it. */
    toggleSort: (key: string) => {
      setAnchor(undefined)
      setSort((s) => (s?.key !== key ? { key, dir: 'asc' } : { key, dir: s.dir === 'asc' ? 'desc' : 'asc' }))
    },
    rows: visibleRows,
    filteredCount: filtered.length,
    totalCount: rows.length,
    isFiltered: query.trim() !== '',
    selectedRows,
    selectedCount: selectedRows.length,
    allSelected,
    someSelected,
    isSelected: (id: string) => selectedIds.has(id),
    /**
     * Row checkbox click. With shift held (details from the checkbox's onCheckedChange),
     * every row from the last clicked one to this one, in the current filtered and
     * sorted order, takes this row's new state; otherwise only this row flips.
     */
    toggle: (id: string, details?: ToggleDetails) => {
      const ev = details?.event
      const shift = !!ev && 'shiftKey' in ev && ev.shiftKey === true
      const from = anchor === undefined ? -1 : filteredIds.indexOf(anchor)
      const to = filteredIds.indexOf(id)
      const span = shift && from >= 0 && to >= 0 ? filteredIds.slice(Math.min(from, to), Math.max(from, to) + 1) : [id]
      const range = span.length > 1 && canSelect ? span.filter((r) => selectableIds.includes(r)) : span
      setAnchor(id)
      setSelectedIds((prev) => {
        const next = new Set(prev)
        const on = !prev.has(id)
        for (const r of range) {
          if (on) next.add(r)
          else next.delete(r)
        }
        return next
      })
    },
    toggleAll: () => setSelectedIds(allSelected ? new Set() : new Set(selectableIds)),
    clearSelection: () => setSelectedIds(new Set()),
    page: currentPage,
    setPage,
    pageSize,
    pageCount,
  }
}

export type DataTableState = ReturnType<typeof useDataTable<unknown>>
