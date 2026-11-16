/** A key with its exact modifiers. `ctrl` also accepts ⌘ on macOS. */
export type Combo = { key?: string; code?: string; ctrl?: boolean; shift?: boolean; alt?: boolean; meta?: boolean }

export const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform)

const sameKey = (a: string, b: string) =>
  a.length === 1 && b.length === 1 ? a.toLowerCase() === b.toLowerCase() : a === b

/** Whether the event is this combo, with no extra modifier held. */
export function matches(e: KeyboardEvent, c: Combo, mac = isMac): boolean {
  if (c.key !== undefined && !sameKey(e.key, c.key)) return false
  if (c.code !== undefined && e.code !== c.code) return false
  if (e.shiftKey !== !!c.shift || e.altKey !== !!c.alt) return false
  if (mac && c.ctrl) return e.ctrlKey || e.metaKey
  return e.ctrlKey === !!c.ctrl && e.metaKey === !!c.meta
}

const typingRoles = '[role="combobox"], [role="listbox"], [role="menu"], [role="menuitem"], [role="textbox"]'
const notTyping = new Set(['checkbox', 'radio', 'button', 'submit', 'reset', 'image', 'file', 'range', 'color'])

/** Whether keys typed here belong to the element: text fields, selects, editors and popup lists. */
export function isTypingTarget(t: EventTarget | null): boolean {
  if (!(t instanceof Element)) return false
  if (t instanceof HTMLInputElement) return !notTyping.has(t.type)
  if (t instanceof HTMLTextAreaElement || t instanceof HTMLSelectElement) return true
  if (t.closest('[contenteditable]:not([contenteditable="false"])')) return true
  return t.closest(typingRoles) !== null
}
