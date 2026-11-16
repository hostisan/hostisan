import { Button, LinkButton, useKumoToastManager } from '@cloudflare/kumo'
import { useQueryClient } from '@tanstack/react-query'
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type FormEvent,
  type MouseEvent,
  type ReactNode,
} from 'react'
import { useNavigate } from 'react-router'
import { useT } from '../i18n/I18nProvider'
import { shortcutTarget } from '../shortcuts/targets'
import { FormGuardContext, type FormGuard } from '../unsaved/types'
import { UnsavedChangesDialog } from '../unsaved/UnsavedChangesDialog'
import { useLeaveGuard } from '../unsaved/useLeaveGuard'
import { PageHeader } from './PageHeader'

type FormPageProps = {
  title: string
  description?: ReactNode
  back: { href: string; label: string }
  /** Primary action label, e.g. "Add web domain" or "Save". */
  submitLabel?: string
  /** Toast shown on submit in the design prototype. */
  successMessage?: string
  /** Extra buttons in the header (e.g. "Delete"). */
  actions?: ReactNode
  readOnly?: boolean
  /** Disables the submit button, e.g. until a required choice is made. */
  submitDisabled?: boolean
  /**
   * Whether the form has unsaved changes, for pages whose controls the form can't
   * see change. When set, it replaces the form's own change detection.
   */
  dirty?: boolean
  /**
   * Replaces the default "toast and go back", e.g. to show a result or open the new item.
   * Submitting disarms the unsaved-changes warning. When the save does not happen (a
   * client-side check stops it, a raw request fails, or a started job fails later), call
   * `guard.rearm()` so leaving asks again. A failed react-query mutation started by this
   * submit re-arms it on its own.
   */
  onSubmit?: (e: FormEvent<HTMLFormElement>, guard: FormGuard) => unknown
  children: ReactNode
}

/**
 * Layout for every add/edit screen: header with a back link, stacked FormSections,
 * and a sticky action bar. In the design prototype, submitting shows a toast and
 * returns to the list.
 *
 * It asks before leaving with unsaved changes. An input or change event inside the
 * form, an option picked in a select or combobox list opened from the form, or
 * `markDirty()` from a shared field counts as a change. Events from a dialog rendered
 * inside the page (React delivers them to the form through portals) do not count, and
 * a dialog's own form never submits the page.
 *
 * Submitting disarms the warning. It arms again on a later change, on `guard.rearm()`
 * (see `onSubmit`), or when a mutation this submit started fails.
 */
export function FormPage({
  title,
  description,
  back,
  submitLabel,
  successMessage,
  actions,
  readOnly,
  submitDisabled,
  dirty: dirtyProp,
  onSubmit: onSubmitProp,
  children,
}: FormPageProps) {
  const t = useT()
  const toast = useKumoToastManager()
  const navigate = useNavigate()
  const client = useQueryClient()

  // Refs, so the blocker reads the state of this very tick.
  const formRef = useRef<HTMLFormElement>(null)
  const changed = useRef(false)
  const submitting = useRef(false)
  // The mutations this form's submits started; only their failure re-arms the guard.
  const collecting = useRef(false)
  const own = useRef(new Set<unknown>())
  const override = useRef(dirtyProp)
  const [dirty, setDirty] = useState(false)

  const markDirty = useCallback(() => {
    changed.current = true
    submitting.current = false
    setDirty(true)
  }, [])
  const release = useCallback(() => {
    submitting.current = true
  }, [])
  const rearm = useCallback(() => {
    submitting.current = false
  }, [])
  // Leaves `submitting` alone, so a save with the dirty prop still set can navigate.
  const reset = useCallback(() => {
    changed.current = false
    setDirty(false)
  }, [])

  useLayoutEffect(() => {
    // A page's own dirty flag turning on is a new change, even after a save.
    if (dirtyProp && !override.current) submitting.current = false
    override.current = dirtyProp
  }, [dirtyProp])

  // A write this form's submit started that fails leaves the changes unsaved. Other
  // mutations (a notification marked read, a dialog's own action) never re-arm it.
  useEffect(
    () =>
      client.getMutationCache().subscribe((event) => {
        if (event.type === 'added' && collecting.current) own.current.add(event.mutation)
        if (event.type !== 'updated' || event.action.type !== 'error') return
        if (own.current.has(event.mutation)) submitting.current = false
      }),
    [client],
  )

  const { blocker } = useLeaveGuard(() => !submitting.current && (override.current ?? changed.current))

  const guard = useMemo<FormGuard>(
    () => ({ dirty: dirtyProp ?? dirty, markDirty, release, rearm, reset }),
    [dirtyProp, dirty, markDirty, release, rearm, reset],
  )

  /** The event comes from this form's own controls, not from a dialog portalled into it. */
  const ours = (target: EventTarget) => {
    const form = formRef.current
    if (!form || !(target instanceof Node)) return false
    return form.contains(target) || fromFormList(form, target)
  }
  const onEdit = (e: FormEvent) => {
    if (ours(e.target)) markDirty()
  }
  // Kumo's Select fires no native event when an option is picked; its options sit in
  // a portal, but their clicks still reach the form through the React tree.
  const onClick = (e: MouseEvent) => {
    if (e.target instanceof Element && e.target.closest('[role="option"]') && ours(e.target)) markDirty()
  }

  const onSubmit = (e: FormEvent<HTMLFormElement>) => {
    // A dialog's own form, portalled into this one: its handler deals with it.
    if (e.target !== e.currentTarget) return
    e.preventDefault()
    release()
    if (onSubmitProp) {
      own.current.clear()
      collecting.current = true
      let result: unknown
      try {
        result = onSubmitProp(e, guard)
      } finally {
        if (!(result instanceof Promise)) collecting.current = false
      }
      if (result instanceof Promise) {
        void result.finally(() => {
          collecting.current = false
        })
      }
      return
    }
    reset()
    toast.add({ title: successMessage ?? t('Changes have been saved.'), type: 'success' })
    navigate(back.href)
  }

  return (
    <FormGuardContext.Provider value={guard}>
      <form
        ref={formRef}
        onSubmit={onSubmit}
        onInput={onEdit}
        onChange={onEdit}
        onClick={onClick}
        className="grid gap-8 pb-4"
      >
        <PageHeader title={title} description={description} back={back} actions={actions} />
        {children}
        <div className="sticky bottom-0 z-10 -mx-4 flex justify-end gap-2 border-t border-kumo-line bg-kumo-canvas px-4 py-3 sm:-mx-6 sm:px-6">
          <LinkButton href={back.href} variant="secondary">
            {t('Cancel')}
          </LinkButton>
          <Button type="submit" variant="primary" disabled={readOnly || submitDisabled} {...shortcutTarget('save')}>
            {submitLabel ?? t('Save')}
          </Button>
        </div>
      </form>
      <UnsavedChangesDialog blocker={blocker} />
    </FormGuardContext.Provider>
  )
}

/**
 * The target sits in a select or combobox list whose trigger is inside the form. Base UI
 * points the open trigger's aria-controls at its list (or the list's positioner).
 */
function fromFormList(form: HTMLFormElement, target: Node): boolean {
  for (let el = target instanceof Element ? target : target.parentElement; el; el = el.parentElement) {
    if (!el.id) continue
    const trigger = form.querySelector(`[aria-controls~="${CSS.escape(el.id)}"]`)
    if (trigger?.getAttribute('role') === 'combobox') return true
  }
  return false
}
