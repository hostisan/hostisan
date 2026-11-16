import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { TableCard } from './TableCard'
import { countNoun } from './countNoun'

describe('countNoun', () => {
  it('uses the singular for exactly one', () => {
    expect(countNoun(1, { one: 'DNS zone', other: 'DNS zones' })).toBe('DNS zone')
    expect(countNoun(0, { one: 'DNS zone', other: 'DNS zones' })).toBe('DNS zones')
    expect(countNoun(2, { one: 'DNS zone', other: 'DNS zones' })).toBe('DNS zones')
    expect(countNoun(1, 'rules')).toBe('rules')
  })
})

describe('TableCard footer', () => {
  const empty = { icon: null, title: 'None' }
  it('says "1 DNS zone"', () => {
    render(
      <TableCard filteredCount={1} totalCount={1} noun={{ one: 'DNS zone', other: 'DNS zones' }} emptyState={empty}>
        <div />
      </TableCard>,
    )
    expect(screen.getByText('1 DNS zone')).toBeTruthy()
  })
  it('says "2 DNS zones"', () => {
    render(
      <TableCard filteredCount={2} totalCount={2} noun={{ one: 'DNS zone', other: 'DNS zones' }} emptyState={empty}>
        <div />
      </TableCard>,
    )
    expect(screen.getByText('2 DNS zones')).toBeTruthy()
  })
})

describe('TableCard row focus', () => {
  it('the focused row is marked and scrolled into view', () => {
    const scrolled = vi.spyOn(Element.prototype, 'scrollIntoView')
    const rows = (focusedId: string | null) => (
      <TableCard
        filteredCount={2}
        totalCount={2}
        noun="rows"
        emptyState={{ icon: null, title: 'None' }}
        rowShortcuts={{ focusedId, rowProps: () => ({}) }}
      >
        <table>
          <tbody>
            <tr {...(focusedId === 'a' ? { 'data-row-focused': '' } : {})}>
              <td>row a</td>
            </tr>
            <tr {...(focusedId === 'b' ? { 'data-row-focused': '' } : {})}>
              <td>row b</td>
            </tr>
          </tbody>
        </table>
      </TableCard>
    )
    const { container, rerender } = render(rows(null))
    expect(scrolled).not.toHaveBeenCalled()
    rerender(rows('b'))
    const row = screen.getByText('row b').closest('tr') as HTMLElement
    expect(scrolled).toHaveBeenCalledTimes(1)
    expect(scrolled.mock.contexts[0]).toBe(row)
    expect(scrolled).toHaveBeenCalledWith({ block: 'nearest' })
    // The mark comes from the card's scroll area, in Kumo's focus colour, with no transition.
    // Each cell draws its own part, so the opaque sticky name and actions cells (and
    // their fades) show it too: a ring on the <tr> was painted over by them.
    const area = container.querySelector('.overflow-x-auto') as HTMLElement
    // A regex, so Tailwind's scan of this file doesn't emit the old class.
    expect(area.className).not.toMatch(/focused\]\]:ring/)
    for (const part of ['>*]:shadow-', '>*:first-child]:shadow-', '>*:last-child]:shadow-', '>*]:before:shadow-']) {
      expect(area.className).toContain(`[&_[data-row-focused]${part}`)
    }
    expect(area.className).toContain('var(--color-kumo-brand)')
    expect(area.className).not.toMatch(/transition|font-bold/)
    scrolled.mockRestore()
  })
})
