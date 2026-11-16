import { Button, Dialog } from '@cloudflare/kumo'
import type { Blocker } from 'react-router'
import { useT } from '../i18n/I18nProvider'

/**
 * Asks before leaving a form with unsaved changes. Always mounted; open while the
 * blocker holds a navigation. Stay cancels it, Leave page lets it through.
 *
 * Laid out like ConfirmDialog, whose cancel button is fixed to "Cancel"; this one
 * needs "Stay on page".
 */
export function UnsavedChangesDialog({ blocker }: { blocker: Blocker }) {
  const t = useT()
  const blocked = blocker.state === 'blocked'
  const stay = () => {
    if (blocker.state === 'blocked') blocker.reset()
  }
  const leave = () => {
    if (blocker.state === 'blocked') blocker.proceed()
  }

  return (
    <Dialog.Root open={blocked} onOpenChange={(open) => !open && stay()}>
      <Dialog className="grid gap-4 px-6 py-5">
        <div className="grid gap-1.5">
          <Dialog.Title className="text-lg font-semibold">{t('Unsaved changes')}</Dialog.Title>
          <Dialog.Description className="text-kumo-subtle">
            {t('Are you sure you want to leave the page?')}
          </Dialog.Description>
        </div>
        <div className="flex justify-end gap-2">
          <Button onClick={stay}>{t('Stay on page')}</Button>
          <Button variant="destructive" onClick={leave}>
            {t('Leave page')}
          </Button>
        </div>
      </Dialog>
    </Dialog.Root>
  )
}
