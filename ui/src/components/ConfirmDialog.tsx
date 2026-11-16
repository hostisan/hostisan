import { Button, Dialog } from '@cloudflare/kumo'
import type { ReactNode } from 'react'
import { useT } from '../i18n/I18nProvider'

type ConfirmDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description: ReactNode
  confirmLabel: string
  destructive?: boolean
  onConfirm: () => void
}

/** Always render this and toggle `open`; never mount it conditionally (Kumo rule). */
export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel,
  destructive,
  onConfirm,
}: ConfirmDialogProps) {
  const t = useT()
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog className="grid gap-4 px-6 py-5">
        <div className="grid gap-1.5">
          <Dialog.Title className="text-lg font-semibold">{title}</Dialog.Title>
          <Dialog.Description className="text-kumo-subtle">{description}</Dialog.Description>
        </div>
        <div className="flex justify-end gap-2">
          <Dialog.Close render={(props) => <Button {...props}>{t('Cancel')}</Button>} />
          <Button
            variant={destructive ? 'destructive' : 'primary'}
            onClick={() => {
              onConfirm()
              onOpenChange(false)
            }}
          >
            {confirmLabel}
          </Button>
        </div>
      </Dialog>
    </Dialog.Root>
  )
}
