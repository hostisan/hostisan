import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useNavigate } from 'react-router'
import { hasFeature, numberedSections } from '../layout/nav'
import { useSessionUser } from '../session/SessionProvider'
import { isTypingTarget, matches } from './keys'
import { SHORTCUT_ATTR, type ShortcutTarget } from './targets'

/** Handles a key the engine lets through; returns true when it acted, so the key is prevented. */
export type ShortcutHandler = (e: KeyboardEvent) => boolean

type Toggle = { open: boolean; setOpen(v: boolean): void }

type ShortcutsContextValue = {
  dialog: Toggle
  palette: Toggle
  /** Adds a page handler; returns its removal. */
  register(handler: { current: ShortcutHandler }): () => void
}

const ShortcutsContext = createContext<ShortcutsContextValue | null>(null)

const popupSelector = '[role="dialog"], [role="alertdialog"], [role="menu"], [role="listbox"]'

/** A Kumo dialog, menu or open select list is showing: its own keys win. */
export function popupOpen(): boolean {
  return [...document.querySelectorAll(popupSelector)].some((el) => shown(el) && !isHelp(el) && !isToast(el))
}

/**
 * Base UI keeps a Select's list mounted after it closes, and some lists are mounted
 * before they ever open. A closed popup carries data-closed (an open one data-open)
 * and its positioner the hidden attribute, so it has no boxes. Both must hold.
 */
export function shown(el: Element): boolean {
  if (el.closest('[data-closed], [hidden]')) return false
  if (typeof el.checkVisibility === 'function') return el.checkVisibility()
  return el.getClientRects().length > 0
}

/**
 * A Kumo toast: Base UI renders each one as a non-modal dialog inside the toast
 * viewport, a live region. It takes no keys, so shortcuts keep working under it.
 */
const isToast = (el: Element) => el.closest('[role="region"][aria-live]') !== null

/** The shortcuts help itself: h and ? still close it. */
const isHelp = (el: Element) => el.querySelector('[data-shortcuts-dialog]') !== null

/** The top-bar search field shows from this width up (Tailwind's md); below it, f opens the palette. */
export const SEARCH_FIELD_MEDIA = '(min-width: 48rem)'

function target(t: ShortcutTarget): HTMLElement | null {
  return document.querySelector<HTMLElement>(`[${SHORTCUT_ATTR}="${t}"]:not(:disabled):not([aria-disabled="true"])`)
}

/** Clicks the page's element for this target; false when the page has none. */
function follow(t: ShortcutTarget): boolean {
  const el = target(t)
  el?.click()
  return el !== null
}

function blurField(): boolean {
  const el = document.activeElement
  if (!(el instanceof HTMLElement) || !isTypingTarget(el)) return false
  el.blur()
  return true
}

/**
 * The app's one keyboard listener. Plain keys never fire while the user types in a
 * field or while a dialog or menu is open; a key is prevented only when it is handled.
 */
export function ShortcutsProvider({ children }: { children: ReactNode }) {
  const [dialogOpen, setDialogOpen] = useState(false)
  const [paletteOpen, setPaletteOpen] = useState(false)
  const handlers = useRef<Array<{ current: ShortcutHandler }>>([])

  const navigate = useNavigate()
  const features = useSessionUser()?.features
  const live = useRef({ navigate, features })
  useEffect(() => {
    live.current = { navigate, features }
  })

  useEffect(() => {
    const focusSearch = () => {
      const field = target('search')
      if (field && window.matchMedia(SEARCH_FIELD_MEDIA).matches) field.focus()
      else setPaletteOpen(true)
      return true
    }
    const openSection = (n: number) => {
      const section = numberedSections[n - 1]
      if (!section || !hasFeature(section, live.current.features)) return false
      live.current.navigate(section.path)
      return true
    }
    // Whether a popup was open when the key started. A popup may handle the key and
    // close before it bubbles to the window (an open Select picks on Enter), and the
    // key still belonged to it.
    const inPopup = new WeakSet<Event>()
    const handle = (e: KeyboardEvent): boolean => {
      if (matches(e, { key: 'k', ctrl: true }) || matches(e, { key: 'k', meta: true })) {
        setPaletteOpen((o) => !o)
        return true
      }
      const popup = inPopup.has(e) || popupOpen()
      if (matches(e, { key: 'Escape' })) return !popup && blurField()
      if (popup) return false
      const help = matches(e, { key: 'h' }) || matches(e, { key: '?', shift: true }) || matches(e, { key: '?' })
      if (help && !isTypingTarget(e.target)) {
        setDialogOpen((o) => !o)
        return true
      }
      // While the help is open, only its own keys work.
      if ([...document.querySelectorAll('[data-shortcuts-dialog]')].some(shown)) return false
      // Hestia listed Ctrl+Enter as "Save form" but never wired it; it works in fields too.
      if (matches(e, { key: 'Enter', ctrl: true })) return follow('save')
      if (isTypingTarget(e.target)) return false
      if (matches(e, { key: 'a' })) return follow('add')
      if (matches(e, { key: 'f' })) return focusSearch()
      if (matches(e, { key: 'Backspace', ctrl: true })) return follow('back')
      if (/^[1-9]$/.test(e.key) && matches(e, { key: e.key })) return openSection(Number(e.key))
      // The page's own handlers (row focus, select all), the most recently mounted first.
      for (const h of [...handlers.current].reverse()) if (h.current(e)) return true
      return false
    }
    const onKey = (e: KeyboardEvent) => {
      // Safari sends the key that ends a composition with isComposing false and keyCode 229.
      if (e.isComposing || e.keyCode === 229) return
      // Kumo radios and select triggers prevent Enter for themselves; Ctrl+Enter still saves.
      if (e.defaultPrevented && !matches(e, { key: 'Enter', ctrl: true })) return
      if (handle(e)) e.preventDefault()
    }
    const onKeyStart = (e: KeyboardEvent) => {
      if (popupOpen()) inPopup.add(e)
    }
    window.addEventListener('keydown', onKeyStart, true)
    window.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('keydown', onKeyStart, true)
      window.removeEventListener('keydown', onKey)
    }
  }, [])

  const register = useCallback((handler: { current: ShortcutHandler }) => {
    handlers.current = [...handlers.current, handler]
    return () => {
      handlers.current = handlers.current.filter((h) => h !== handler)
    }
  }, [])

  const value = useMemo<ShortcutsContextValue>(
    () => ({
      dialog: { open: dialogOpen, setOpen: setDialogOpen },
      palette: { open: paletteOpen, setOpen: setPaletteOpen },
      register,
    }),
    [dialogOpen, paletteOpen, register],
  )
  return <ShortcutsContext.Provider value={value}>{children}</ShortcutsContext.Provider>
}

const closed: Toggle = { open: false, setOpen: () => {} }

/** The shortcuts help dialog's state. */
export function useShortcutsDialog(): Toggle {
  return useContext(ShortcutsContext)?.dialog ?? closed
}

/** The ⌘K command palette's state. */
export function useCommandPalette(): Toggle {
  return useContext(ShortcutsContext)?.palette ?? closed
}

/** Lets a page take plain keys while it is mounted (outside fields and popups). */
export function useShortcutHandler(handler: ShortcutHandler) {
  const register = useContext(ShortcutsContext)?.register
  const ref = useRef(handler)
  useEffect(() => {
    ref.current = handler
  })
  useEffect(() => register?.(ref), [register])
}
