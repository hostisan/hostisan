import { Banner, Loader } from '@cloudflare/kumo'
import { WarningIcon } from '@phosphor-icons/react'
import { apiErrorText } from '../api/errors'
import { useT } from '../i18n/I18nProvider'

/** Spinner for a page whose request is pending. */
export function LoadingState() {
  const t = useT()
  return (
    <div className="flex justify-center py-12" role="status" aria-label={t('Loading')}>
      <Loader />
    </div>
  )
}

/**
 * A failed request, with the server's own text; may span lines. With a title, the
 * text goes underneath it.
 */
export function RequestError({ error, title }: { error: unknown; title?: string }) {
  const t = useT()
  const text = apiErrorText(error, t)
  return (
    <Banner
      variant="error"
      icon={<WarningIcon weight="fill" />}
      title={title ?? text}
      description={title ? <span className="whitespace-pre-line">{text}</span> : undefined}
      className={title ? undefined : 'whitespace-pre-line'}
    />
  )
}
