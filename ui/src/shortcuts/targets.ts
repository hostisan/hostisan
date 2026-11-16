/**
 * Elements a global shortcut acts on carry `data-shortcut="<target>"`: `a` follows
 * the "add" link, `f` focuses "search", Ctrl+Backspace follows "back", Ctrl+Enter clicks
 * "save". Spread `shortcutTarget('add')` onto the element.
 */
export const SHORTCUT_ATTR = 'data-shortcut'

export type ShortcutTarget = 'add' | 'search' | 'back' | 'save'

export function shortcutTarget(t: ShortcutTarget): { 'data-shortcut': ShortcutTarget } {
  return { [SHORTCUT_ATTR]: t } as { 'data-shortcut': ShortcutTarget }
}
