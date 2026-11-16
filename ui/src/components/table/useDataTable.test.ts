import type { CheckboxChangeEventDetails } from '@cloudflare/kumo'
import { act, renderHook } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { useDataTable } from './useDataTable'

type Row = { id: string; name: string }
const rows: Row[] = ['1', '2', '3', '4', '5', '6'].map((id) => ({ id, name: `row ${id}` }))
const shift = { event: { shiftKey: true } }

function table() {
  return renderHook(() =>
    useDataTable({
      rows,
      getId: (r) => r.id,
      search: (r, q) => r.name.includes(q),
      sorts: { name: (r) => r.name },
    }),
  )
}

const selected = (r: ReturnType<typeof table>['result']) => r.current.selectedRows.map((x) => x.id)

describe('useDataTable range selection (decision 32)', () => {
  it('shift-click from row 2 to row 5 selects 2-5', () => {
    const { result } = table()
    act(() => result.current.toggle('2'))
    act(() => result.current.toggle('5', shift))
    expect(selected(result)).toEqual(['2', '3', '4', '5'])
  })

  it('works backwards, in the current sorted order', () => {
    const { result } = table()
    act(() => result.current.toggleSort('name'))
    act(() => result.current.toggleSort('name')) // descending: 6 5 4 3 2 1
    act(() => result.current.toggle('5'))
    act(() => result.current.toggle('2', shift))
    expect(selected(result).sort()).toEqual(['2', '3', '4', '5'])
  })

  it('shift-click unchecking clears the range', () => {
    const { result } = table()
    act(() => result.current.toggle('2'))
    act(() => result.current.toggle('5', shift))
    act(() => result.current.toggle('6'))
    act(() => result.current.toggle('3', shift)) // 3 is selected: the range 6..3 goes off
    expect(selected(result)).toEqual(['2'])
  })

  it('the anchor resets after setQuery and after a sort change', () => {
    const { result } = table()
    act(() => result.current.toggle('2'))
    act(() => result.current.setQuery('row'))
    act(() => result.current.toggle('5', shift))
    expect(selected(result)).toEqual(['2', '5'])

    act(() => result.current.toggleSort('name'))
    act(() => result.current.toggle('1', shift))
    expect(selected(result)).toEqual(['1', '2', '5'])
  })

  it('a shift-click with no anchor, or an anchor filtered out, toggles one row', () => {
    const { result } = table()
    act(() => result.current.toggle('3', shift))
    expect(selected(result)).toEqual(['3'])
  })

  it('plain toggle(id) is unchanged', () => {
    const { result } = table()
    act(() => result.current.toggle('2'))
    act(() => result.current.toggle('5'))
    expect(selected(result)).toEqual(['2', '5'])
    act(() => result.current.toggle('2', { event: { shiftKey: false } }))
    expect(selected(result)).toEqual(['5'])
    act(() => result.current.toggle('5', {}))
    expect(selected(result)).toEqual([])
  })

  it("accepts a Kumo Checkbox's onCheckedChange details", () => {
    const { result } = table()
    const click = new MouseEvent('click', { shiftKey: true })
    act(() => result.current.toggle('2', { event: new MouseEvent('click') }))
    act(() => result.current.toggle('4', { event: click }))
    act(() => result.current.toggle('6', { event: new Event('change') }))
    expect(selected(result)).toEqual(['2', '3', '4', '6'])
    // The list pages' wiring type-checks: onCheckedChange={(_, d) => table.toggle(id, d)}.
    const onCheckedChange: (checked: boolean, d: CheckboxChangeEventDetails) => void = (_, d) =>
      result.current.toggle('1', d)
    expect(onCheckedChange).toBeTypeOf('function')
  })
})

describe('useDataTable canSelect', () => {
  const locked = () =>
    renderHook(() =>
      useDataTable({ rows, getId: (r) => r.id, canSelect: (r) => r.id !== '3' && r.id !== '6' }),
    )

  it('keeps unselectable rows out of a shift-click range', () => {
    const { result } = locked()
    act(() => result.current.toggle('1'))
    act(() => result.current.toggle('5', shift))
    expect(selected(result)).toEqual(['1', '2', '4', '5'])
  })

  it('select-all skips them and counts as all selected without them', () => {
    const { result } = locked()
    act(() => result.current.toggleAll())
    expect(selected(result)).toEqual(['1', '2', '4', '5'])
    expect(result.current.allSelected).toBe(true)
    act(() => result.current.toggleAll())
    expect(selected(result)).toEqual([])
  })
})
