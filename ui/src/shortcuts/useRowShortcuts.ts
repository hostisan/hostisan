import { useState } from 'react'
import { matches } from './keys'
import { useShortcutHandler } from './ShortcutsProvider'

/** The per-row keys Hestia's lists use (shortcut-<key> in its templates). */
export type RowKey = 'enter' | 'delete' | 's' | 'l' | 'n' | 'w' | 'd'

const letters: RowKey[] = ['s', 'l', 'n', 'w', 'd']

export type RowShortcuts = {
  focusedId: string | null
  /** Spread onto the row: marks the focused one for TableCard's ring. */
  rowProps(id: string): { 'data-row-focused'?: '' }
}

function rowKey(e: KeyboardEvent): RowKey | null {
  if (matches(e, { key: 'Enter' })) return 'enter'
  if (matches(e, { key: 'Delete' })) return 'delete'
  return letters.find((k) => matches(e, { key: k })) ?? null
}

/** Enter on a focused link or button outside the rows keeps its own meaning. */
function controlHasFocus(): boolean {
  const el = document.activeElement
  return !!el && el !== document.body && !el.closest('[data-row-focused]') && el.matches('a, button, [role="button"]')
}

/**
 * Hestia's list keys: ↑/↓ move a focus ring across the rows, and Enter, Delete and the
 * row letters run that row's action. Delete must go through the page's own confirm.
 * Keys are prevented only when they act, so ↑ on the first row and ↓ on the last
 * still scroll the page. The focus resets whenever the rows change.
 */
export function useRowShortcuts<T>(
  rows: T[],
  id: (r: T) => string,
  actions: (r: T) => Partial<Record<RowKey, () => void>>,
): RowShortcuts {
  const ids = rows.map(id)
  const key = ids.join('\n')
  const [state, setState] = useState<{ key: string; focused: string | null }>({ key, focused: null })
  const focusedId = state.key === key ? state.focused : null
  const index = focusedId === null ? -1 : ids.indexOf(focusedId)
  const focus = (i: number) => setState({ key, focused: ids[i] })

  useShortcutHandler((e) => {
    if (matches(e, { key: 'ArrowDown' })) {
      if (index + 1 >= ids.length) return false
      focus(index + 1)
      return true
    }
    if (matches(e, { key: 'ArrowUp' })) {
      if (index <= 0) return false
      focus(index - 1)
      return true
    }
    const k = rowKey(e)
    if (index < 0 || !k || (k === 'enter' && controlHasFocus())) return false
    const run = actions(rows[index])[k]
    run?.()
    return !!run
  })

  return {
    focusedId,
    rowProps: (rowId) => (rowId === focusedId ? { 'data-row-focused': '' } : {}),
  }
}
