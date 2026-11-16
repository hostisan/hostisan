import { Button } from '@cloudflare/kumo'
import { ArrowClockwiseIcon } from '@phosphor-icons/react'
import { useState } from 'react'
import { useT } from '../../i18n/I18nProvider'
import { useSession } from '../../session/SessionProvider'
import { AuthLayout } from './AuthLayout'

/** Shown instead of the app when /bootstrap fails, e.g. while the panel restarts. */
export function ServerUnreachable() {
  const t = useT()
  const { refresh } = useSession()
  const [busy, setBusy] = useState(false)
  const retry = async () => {
    setBusy(true)
    await refresh()
    setBusy(false)
  }
  return (
    <AuthLayout
      title={t("Can't reach the server")}
      description={t('The control panel did not respond. It may be restarting; try again in a moment.')}
    >
      <Button variant="primary" icon={ArrowClockwiseIcon} loading={busy} onClick={retry}>
        {t('Try again')}
      </Button>
    </AuthLayout>
  )
}
