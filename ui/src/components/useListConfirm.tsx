import { useKumoToastManager } from '@cloudflare/kumo'
import { useEffect, useState, type ReactNode } from 'react'
import { apiErrorText, bulkMessage, failureText } from '../api/errors'
import type { BulkFailure } from '../api/types.gen'
import { useT } from '../i18n/I18nProvider'
import { ConfirmDialog } from './ConfirmDialog'
import { DeleteResource } from './DeleteResource'

function preLine(text: string) {
  return <span className="whitespace-pre-line">{text}</span>
}

// One confirm dialog per list page: rows and bulk actions call `ask` with what to
// confirm, and confirming shows the success toast.

/** State for a single ConfirmDialog shared by a whole list page. */
export type PendingConfirm = {
  title: string
  description: ReactNode
  confirmLabel: string
  destructive?: boolean
  done: string
  /** Runs after confirming, e.g. to clear the table selection after a bulk action. */
  onDone?: () => void
  /**
   * Does the work. Without it, confirming just shows the success toast (design mocks).
   * A bulk call returns its failed items so each can be named in an error toast.
   */
  run?: () => Promise<unknown>
}

export function useConfirm() {
  const [pending, setPending] = useState<PendingConfirm | null>(null)
  const [open, setOpen] = useState(false)
  const ask = (p: PendingConfirm) => {
    setPending(p)
    setOpen(true)
  }
  return { pending, open, setOpen, ask }
}

export function ListConfirmDialog({ confirm }: { confirm: ReturnType<typeof useConfirm> }) {
  const toast = useKumoToastManager()
  const t = useT()
  const p = confirm.pending
  return (
    <ConfirmDialog
      open={confirm.open}
      onOpenChange={confirm.setOpen}
      title={p?.title ?? ''}
      description={p?.description ?? ''}
      confirmLabel={p?.confirmLabel ?? t('Confirm')}
      destructive={p?.destructive}
      onConfirm={() => {
        if (!p) return
        if (!p.run) {
          toast.add({ title: p.done, type: 'success' })
          p.onDone?.()
          return
        }
        p.run().then(
          (result) => {
            const failed = (result as { failed?: BulkFailure[] } | undefined)?.failed ?? []
            if (failed.length === 0) toast.add({ title: p.done, type: 'success' })
            else if (failed.length <= 3) {
              for (const f of failed) toast.add({ title: f.name, description: preLine(bulkMessage(f, t)), type: 'error' })
            } else {
              const lines = failed.map((f) => failureText(f, t)).join('\n')
              toast.add({ title: t('Some items failed'), description: preLine(lines), type: 'error' })
            }
            p.onDone?.()
          },
          (err) => toast.add({ title: p.title, description: preLine(apiErrorText(err, t)), type: 'error' }),
        )
      }}
    />
  )
}

/** Open state for the type-the-name DeleteResource dialog on a list page. */
export function useResourceDelete() {
  const [name, setName] = useState('')
  const [open, setOpen] = useState(false)
  return {
    name,
    open,
    setOpen,
    ask: (n: string) => {
      setName(n)
      setOpen(true)
    },
  }
}

/** Single, unrecoverable deletes (zone, mail domain, mail account). Bulk deletes keep ConfirmDialog. */
export function ResourceDeleteDialog({
  state,
  resourceType,
  onDelete,
}: {
  state: ReturnType<typeof useResourceDelete>
  /** The translated noun, as it reads mid-sentence: t('web domain'). */
  resourceType: string
  /** Does the delete; the dialog stays open with the error if it rejects. */
  onDelete?: (name: string) => Promise<unknown>
}) {
  const toast = useKumoToastManager()
  const t = useT()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  // A new item or a close starts clean; an old failure never shows for another zone
  useEffect(() => setError(''), [state.open, state.name])
  return (
    <DeleteResource
      open={state.open}
      onOpenChange={state.setOpen}
      resourceType={resourceType}
      resourceName={state.name}
      isDeleting={busy}
      errorMessage={error ? <span className="whitespace-pre-line">{error}</span> : undefined}
      onDelete={async () => {
        if (onDelete) {
          setBusy(true)
          try {
            await onDelete(state.name)
          } catch (err) {
            setError(apiErrorText(err, t))
            return
          } finally {
            setBusy(false)
          }
        }
        toast.add({ title: t('%s deleted', state.name), type: 'success' })
        state.setOpen(false)
      }}
    />
  )
}
