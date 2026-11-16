// Kumo "delete-resource" block (copied from @cloudflare/kumo/dist/blocks-source, as
// Kumo blocks are meant to be), with imports pointed at the package. Used for deleting
// things that can't be recovered: the user types the name to confirm.
import { Banner, Button, Dialog, DialogClose, DialogRoot, DialogTitle, Input, cn } from '@cloudflare/kumo'
import { CheckIcon, CopyIcon, WarningCircleIcon, XIcon } from '@phosphor-icons/react'
import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { useT } from '../i18n/I18nProvider'
import { interpolate } from '../lib/interpolate'

export const KUMO_DELETE_RESOURCE_VARIANTS = {
  size: {
    sm: { classes: '' },
    base: { classes: '' },
  },
} as const

export const KUMO_DELETE_RESOURCE_DEFAULT_VARIANTS = {
  size: 'base',
} as const

export type KumoDeleteResourceSize = keyof typeof KUMO_DELETE_RESOURCE_VARIANTS.size

export interface KumoDeleteResourceVariantsProps {
  size?: KumoDeleteResourceSize
}

export interface DeleteResourceProps extends KumoDeleteResourceVariantsProps {
  /** Whether the dialog is open */
  open: boolean
  /** Callback when open state changes */
  onOpenChange: (open: boolean) => void
  /**
   * The kind of thing being deleted, already translated and as it reads mid-sentence
   * (t('web domain')): it fills "Delete %s" and the warning sentence.
   */
  resourceType: string
  /** The name of the specific resource being deleted */
  resourceName: string
  /** Callback when delete is confirmed */
  onDelete: () => void | Promise<void>
  /** Whether the delete action is in progress */
  isDeleting?: boolean
  /** Whether the confirmation input should be case-sensitive (default: true) */
  caseSensitive?: boolean
  /** Custom delete button text, already translated (defaults to "Delete {resourceType}") */
  deleteButtonText?: string
  /** Additional className for the dialog */
  className?: string
  /** Error message to display if the delete action fails */
  errorMessage?: ReactNode
}

export function DeleteResource({
  open,
  onOpenChange,
  resourceType,
  resourceName,
  onDelete,
  isDeleting = false,
  caseSensitive = true,
  deleteButtonText,
  size = KUMO_DELETE_RESOURCE_DEFAULT_VARIANTS.size,
  errorMessage,
  className,
}: DeleteResourceProps) {
  const t = useT()
  const [confirmationInput, setConfirmationInput] = useState('')
  const [copied, setCopied] = useState(false)

  useEffect(() => {
    if (!open) {
      setConfirmationInput('')
      setCopied(false)
    }
  }, [open])

  const normalizeForComparison = useCallback(
    (str: string) => (caseSensitive ? str : str.toLowerCase()),
    [caseSensitive],
  )

  const isConfirmed = normalizeForComparison(confirmationInput) === normalizeForComparison(resourceName)

  const handleDelete = useCallback(async () => {
    if (!isConfirmed || isDeleting) return
    await onDelete()
  }, [isConfirmed, isDeleting, onDelete])

  const handleCopy = useCallback(async () => {
    await navigator.clipboard.writeText(resourceName)
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }, [resourceName])

  return (
    <DialogRoot open={open} onOpenChange={onOpenChange}>
      <Dialog size={size} className={cn('p-0', className)}>
        <div className="flex items-center justify-between border-b border-kumo-line px-6 py-4">
          <DialogTitle className="text-lg font-semibold">{t('Delete %s', resourceName)}</DialogTitle>
          <DialogClose
            render={(props) => (
              <Button {...props} variant="ghost" shape="square" size="sm" aria-label={t('Close')} disabled={isDeleting}>
                <XIcon size={18} />
              </Button>
            )}
          />
        </div>

        <div className="flex flex-col gap-4 p-6">
          <div className="flex flex-col gap-2">
            {errorMessage && (
              <Banner icon={<WarningCircleIcon />} variant="error">
                {errorMessage}
              </Banner>
            )}
            <p className="max-w-prose text-base text-pretty text-kumo-subtle">
              {interpolate(
                t('This action cannot be undone. This will permanently delete the %s %s.'),
                <span className="font-medium text-kumo-default">{resourceName}</span>,
                resourceType,
              )}
            </p>
          </div>

          <div className="flex flex-col gap-2">
            <div className="flex items-center gap-1.5 text-base">
              <span>
                {interpolate(
                  t('Type %s to confirm:'),
                  <button
                  className="group inline rounded-md bg-kumo-tint px-2 py-1 font-mono text-sm font-semibold hover:cursor-pointer hover:bg-kumo-fill"
                  onClick={handleCopy}
                  aria-label={t('Copy %s to clipboard', resourceName)}
                >
                  {resourceName}

                  {copied ? (
                    <CheckIcon size={12} weight="bold" className="ml-1.5 inline" />
                  ) : (
                    <CopyIcon
                      size={12}
                      weight="bold"
                      className="ml-1.5 inline text-kumo-subtle group-hover:text-kumo-default"
                    />
                  )}
                  </button>,
                )}
              </span>
            </div>
            <Input
              placeholder={resourceName}
              value={confirmationInput}
              onChange={(e) => setConfirmationInput(e.target.value)}
              disabled={isDeleting}
              autoComplete="off"
              autoCorrect="off"
              autoCapitalize="off"
              spellCheck={false}
              aria-label={t('Type %s to confirm deletion', resourceName)}
              className="w-full"
            />
          </div>
        </div>

        <div className="flex justify-end gap-3 border-t border-kumo-line px-6 py-4">
          <DialogClose
            render={(props) => (
              <Button {...props} variant="secondary" disabled={isDeleting}>
                {t('Cancel')}
              </Button>
            )}
          />
          <Button
            variant="destructive"
            onClick={handleDelete}
            disabled={!isConfirmed || isDeleting}
            loading={isDeleting}
          >
            {deleteButtonText || t('Delete %s', resourceType)}
          </Button>
        </div>
      </Dialog>
    </DialogRoot>
  )
}

DeleteResource.displayName = 'DeleteResource'
