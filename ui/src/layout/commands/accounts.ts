import { ArchiveIcon, BellIcon, KeyIcon, PlusIcon, UserCircleIcon, UsersIcon } from '@phosphor-icons/react'
import { useMemo } from 'react'
import { useUsers } from '../../api/users'
import { useT } from '../../i18n/I18nProvider'
import { useSessionUser } from '../../session/SessionProvider'
import type { CommandContribution, CommandItem } from './types'

/** The accounts section's palette entries: cron, user and backup actions, account pages, users. */
export function useAccountCommands(open: boolean): CommandContribution {
  const t = useT()
  const user = useSessionUser()
  const isAdmin = user?.role === 'admin' && user.look === ''
  const cron = user?.features.cron ?? false
  const backup = user?.features.backup ?? false
  const accessKeys = user?.features.accessKeys ?? false
  // Fetched only while the palette is open, and only for an admin who can list users.
  const list = useUsers({ enabled: open && isAdmin }).data
  const users = list?.users
  const self = list?.self
  const root = list?.rootUser
  return useMemo(() => {
    const actions: CommandItem[] = [
      ...(cron ? [{ id: 'new:cron', title: t('Add cron job'), icon: PlusIcon, href: '/cron/new' }] : []),
      ...(isAdmin ? [{ id: 'new:user', title: t('Add user'), icon: PlusIcon, href: '/users/new' }] : []),
      ...(backup ? [{ id: 'backups', title: t('Create backup'), icon: ArchiveIcon, href: '/backup' }] : []),
    ]
    const pages: CommandItem[] = [
      { id: 'page:/account', title: t('Edit account'), icon: UserCircleIcon, href: '/account' },
      { id: 'page:/account/ssh-keys', title: t('SSH keys'), icon: KeyIcon, href: '/account/ssh-keys' },
      ...(accessKeys
        ? [{ id: 'page:/account/access-keys', title: t('Access keys'), icon: KeyIcon, href: '/account/access-keys' }]
        : []),
      { id: 'page:/notifications', title: t('Notifications'), icon: BellIcon, href: '/notifications' },
    ]
    const groups = isAdmin && users
      ? [
          {
            id: 'users',
            label: t('Users'),
            // Your own entry opens the account page, and only root may open root's.
            items: users
              .filter((u) => u.USER !== root || self === root)
              .map((u) => ({
                id: `user:${u.USER}`,
                title: u.USER,
                hint: u.NAME,
                icon: UsersIcon,
                href: u.USER === self ? '/account' : `/users/${u.USER}`,
                keywords: u.CONTACT,
              })),
          },
        ]
      : []
    return { actions, pages, groups }
  }, [t, isAdmin, cron, backup, accessKeys, users, self, root])
}
