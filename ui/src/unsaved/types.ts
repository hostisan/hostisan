import { createContext, useContext } from 'react'

/**
 * The unsaved-changes state of the form page around a component. Shared field
 * components call `markDirty()` for changes that don't bubble a native input event.
 */
export type FormGuard = {
  /** The form has changes that are not saved. */
  dirty: boolean
  /** Records a change. */
  markDirty(): void
  /** Disarms the guard, for a submit or a deliberate leave. */
  release(): void
  /** Arms the guard again after a submit that did not save; keeps the changes. */
  rearm(): void
  /** Forgets every change, as after a successful save. */
  reset(): void
}

export const FormGuardContext = createContext<FormGuard | null>(null)

/** The surrounding form page's guard; null outside a form page. */
export function useFormGuard(): FormGuard | null {
  return useContext(FormGuardContext)
}
