import { Tooltip } from '@cloudflare/kumo'
import { CheckCircleIcon, MinusIcon } from '@phosphor-icons/react'
import { useT } from '../../i18n/I18nProvider'

/** Enabled/disabled feature flag for dense mail tables. `label` comes translated. */
export function FeatureIcon({ enabled, label }: { enabled: boolean; label: string }) {
  const t = useT()
  const text = `${label}: ${enabled ? t('Enabled') : t('Disabled')}`
  return (
    <Tooltip
      content={text}
      render={
        <span aria-label={text} role="img" className="inline-flex items-center">
          {enabled ? (
            <CheckCircleIcon weight="fill" size={16} className="text-kumo-success" />
          ) : (
            <MinusIcon size={16} className="text-kumo-inactive" />
          )}
        </span>
      }
    />
  )
}

// Shared by the domain and account screens (Ruling E: Slice E owns this file, Slice D
// only reads it).

/**
 * The webmail address for a domain (Parity 7): https://<alias>.<domain>/, with
 * ?_user=<account>@<domain> to prefill the login. alias is MailOptions.webmailAlias;
 * "" falls back to "webmail" as Hestia did.
 */
export function mailWebmailUrl(alias: string, domain: string, account?: string) {
  const base = `https://${alias || 'webmail'}.${domain}/`
  return account ? `${base}?_user=${account}@${domain}` : base
}

/** List icons: Hestia shows a feature as off only when its value is exactly "no". */
export function isMailFeatureOn(value: string) {
  return value !== 'no'
}

/** The mail server's host name for a domain (DNS rows, settings panel). */
export function mailHostname(domain: string) {
  return `mail.${domain}`
}
