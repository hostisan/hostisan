import { Button, Text } from '@cloudflare/kumo'
import { ArrowClockwiseIcon, WarningCircleIcon } from '@phosphor-icons/react'
import { useT } from '../i18n/I18nProvider'

/**
 * The root route's error element: a page that crashed while rendering shows this
 * instead of React Router's built-in English page. It sits outside Root, so it needs
 * only the session's translations.
 */
export function RootError() {
  const t = useT()
  return (
    <main className="flex min-h-full items-center justify-center bg-kumo-canvas px-4 py-12">
      <div className="grid max-w-md justify-items-center gap-6 text-center">
        <WarningCircleIcon size={32} className="text-kumo-danger" aria-hidden />
        <div className="grid gap-1.5">
          <Text as="h1" variant="heading" size="lg">
            {t('An internal error occurred')}
          </Text>
          <Text variant="secondary">{t('Reload the page to try again.')}</Text>
        </div>
        <Button variant="primary" icon={ArrowClockwiseIcon} onClick={() => window.location.reload()}>
          {t('Reload')}
        </Button>
      </div>
    </main>
  )
}
