import { Badge } from '@cloudflare/kumo'
import { useT } from '../i18n/I18nProvider'

/** Hestia's SUSPENDED field ("yes" | "no") as a status dot badge. */
export function StatusBadge({ suspended }: { suspended: boolean }) {
  const t = useT()
  return suspended ? (
    <Badge variant="warning" appearance="dot">
      {t('Suspended')}
    </Badge>
  ) : (
    <Badge variant="success" appearance="dot">
      {t('Active')}
    </Badge>
  )
}
