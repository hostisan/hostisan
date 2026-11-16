import { PlusIcon } from '@phosphor-icons/react'
import { useMemo } from 'react'
import { useT } from '../../i18n/I18nProvider'
import { useSessionUser } from '../../session/SessionProvider'
import { noCommands, type CommandContribution, type CommandItem } from './types'

/**
 * The server section's palette entries: the two add actions. The pages themselves are
 * already listed from nav.ts, so none are repeated here. Only an admin with the server
 * feature (the flag already folds in the hide-services policy) sees anything.
 */
export function useServerCommands(_open: boolean): CommandContribution {
  const t = useT()
  const user = useSessionUser()
  const server = user?.role === 'admin' && (user.features.server ?? false)
  const firewall = user?.features.firewall ?? false
  return useMemo(() => {
    if (!server) return noCommands
    const actions: CommandItem[] = [
      { id: 'new:ip', title: t('Add IP address'), icon: PlusIcon, href: '/server/ip/new' },
      ...(firewall ? [{ id: 'new:firewall', title: t('Add firewall rule'), icon: PlusIcon, href: '/server/firewall/new' }] : []),
    ]
    return { actions, pages: [], groups: [] }
  }, [t, server, firewall])
}
