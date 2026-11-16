import { Button, Empty, LinkButton } from '@cloudflare/kumo'
import {
  BellIcon,
  FolderOpenIcon,
  KeyIcon,
  LockKeyIcon,
  ScrollIcon,
  SignOutIcon,
  TerminalWindowIcon,
  TrayIcon,
  UserGearIcon,
  type Icon,
} from '@phosphor-icons/react'
import { useNavigate } from 'react-router'
import { api } from '../../api/client'
import type { Features } from '../../api/types.gen'
import { useT } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { useSession, useSessionUser } from '../../session/SessionProvider'

type AccountLink = { label: string; href: string; icon: Icon; feature?: keyof Features }

const accountLinks: AccountLink[] = [
  { label: msgid('Edit account'), href: '/account', icon: UserGearIcon },
  { label: msgid('SSH keys'), href: '/account/ssh-keys', icon: KeyIcon },
  { label: msgid('Access keys'), href: '/account/access-keys', icon: LockKeyIcon, feature: 'accessKeys' },
  { label: msgid('Logs'), href: '/logs', icon: ScrollIcon, feature: 'logs' },
  { label: msgid('Notifications'), href: '/notifications', icon: BellIcon },
  { label: msgid('Web terminal'), href: '/terminal', icon: TerminalWindowIcon, feature: 'terminal' },
  { label: msgid('File manager'), href: '/files', icon: FolderOpenIcon, feature: 'fileManager' },
]

/**
 * The empty-package page (/error, decision 30): the account's package allows nothing to
 * manage, so it lands here, inside the app shell, with the account pages still reachable.
 */
export function EmptyPackagePage() {
  const t = useT()
  const user = useSessionUser()
  const { refresh } = useSession()
  const navigate = useNavigate()
  const logOut = async () => {
    try {
      await api.post('/auth/logout')
    } catch {
      // Already signed out on the server: the session-lost handler shows the login page.
      return
    }
    await refresh()
    navigate('/login', { replace: true })
  }
  const links = accountLinks.filter((l) => !l.feature || user?.features[l.feature])
  return (
    <Empty
      icon={<TrayIcon size={40} className="text-kumo-inactive" />}
      title={t('Nothing to manage yet')}
      description={t(
        "Your package (%s) doesn't include web domains, DNS zones, mail domains, databases, cron jobs or backups. Ask your administrator to change it.",
        user?.package ?? '',
      )}
      contents={
        <div role="group" aria-label={t('Your account')} className="flex flex-wrap justify-center gap-2">
          {links.map((l) => (
            <LinkButton key={l.href} href={l.href} variant="secondary" icon={l.icon}>
              {t(l.label)}
            </LinkButton>
          ))}
          <Button variant="secondary" icon={SignOutIcon} onClick={() => void logOut()}>
            {t('Log out')}
          </Button>
        </div>
      }
    />
  )
}
