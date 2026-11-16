import { Text } from '@cloudflare/kumo'
import type { ReactNode } from 'react'
import { useT } from '../../i18n/I18nProvider'

/** Small "Optional" marker after a label, as Hestia does. */
export function Optional({ children }: { children?: ReactNode }) {
  const t = useT()
  return (
    <Text variant="secondary" as="span">
      ({children ?? t('optional')})
    </Text>
  )
}
