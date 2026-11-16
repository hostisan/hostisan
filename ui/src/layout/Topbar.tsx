import { Breadcrumbs, Button, DropdownMenu, Input, Sidebar, Text } from '@cloudflare/kumo'
import {
  DesktopIcon,
  KeyIcon,
  KeyboardIcon,
  LockKeyIcon,
  MagnifyingGlassIcon,
  MoonIcon,
  QuestionIcon,
  SignOutIcon,
  SunIcon,
  UserCircleIcon,
  UserGearIcon,
} from '@phosphor-icons/react'
import { useState, type FormEvent } from 'react'
import { useLocation, useNavigate, useSearchParams } from 'react-router'
import { useT } from '../i18n/I18nProvider'
import { msgid } from '../i18n/msgid'
import { useTheme, type ThemePreference } from '../lib/theme'
import { api } from '../api/client'
import { useSession, useSessionUser } from '../session/SessionProvider'
import { useCommandPalette, useShortcutsDialog } from '../shortcuts/ShortcutsProvider'
import { shortcutTarget } from '../shortcuts/targets'
import { CommandMenu, commandShortcut } from './CommandMenu'
import { docsUrl, helpLink } from './help'
import { NotificationsTray } from './NotificationsTray'
import { breadcrumbsFor } from './nav'

function PageBreadcrumbs() {
  const { pathname } = useLocation()
  const t = useT()
  const crumbs = breadcrumbsFor(pathname, t)
  const user = useSessionUser()
  // While viewing as another user, the trail starts at that account.
  const account = user?.look || user?.name
  return (
    <Breadcrumbs size="sm" className="min-w-0 overflow-hidden">
      <Breadcrumbs.Link href="/">{account}</Breadcrumbs.Link>
      {/* Flat children, not fragments: Kumo shortens the trail on phones only when it sees its own parts. */}
      {crumbs.flatMap((c, i) => [
        <Breadcrumbs.Separator key={`${c.href}-sep`} />,
        i === crumbs.length - 1 ? (
          <Breadcrumbs.Current key={c.href}>{c.label}</Breadcrumbs.Current>
        ) : (
          <Breadcrumbs.Link key={c.href} href={c.href}>
            {c.label}
          </Breadcrumbs.Link>
        ),
      ])}
    </Breadcrumbs>
  )
}

const themeOptions: { value: ThemePreference; label: string; icon: typeof SunIcon }[] = [
  { value: 'light', label: msgid('Light'), icon: SunIcon },
  { value: 'dark', label: msgid('Dark'), icon: MoonIcon },
  { value: 'system', label: msgid('System'), icon: DesktopIcon },
]

function UserMenu() {
  const { preference, setPreference } = useTheme()
  const user = useSessionUser()
  const { refresh } = useSession()
  const navigate = useNavigate()
  const t = useT()
  const { bootstrap } = useSession()
  const help = helpLink(bootstrap?.hideDocs ?? false, docsUrl)
  const shortcuts = useShortcutsDialog()
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
  return (
    <DropdownMenu>
      <DropdownMenu.Trigger
        render={
          <Button variant="ghost" icon={UserCircleIcon}>
            {user?.name}
          </Button>
        }
      />
      <DropdownMenu.Content align="end">
        <DropdownMenu.Group>
          <DropdownMenu.Label>
            <div className="grid gap-0.5">
              <Text bold as="span">
                {user?.displayName}
              </Text>
              <Text variant="secondary" size="sm" as="span">
                {user?.contact}
              </Text>
            </div>
          </DropdownMenu.Label>
        </DropdownMenu.Group>
        <DropdownMenu.Separator />
        <DropdownMenu.LinkItem href="/account" icon={UserGearIcon}>
          {t('Edit account')}
        </DropdownMenu.LinkItem>
        <DropdownMenu.LinkItem href="/account/ssh-keys" icon={KeyIcon}>
          {t('SSH keys')}
        </DropdownMenu.LinkItem>
        <DropdownMenu.LinkItem href="/account/access-keys" icon={LockKeyIcon}>
          {t('Access keys')}
        </DropdownMenu.LinkItem>
        <DropdownMenu.Sub>
          <DropdownMenu.SubTrigger icon={SunIcon}>{t('Theme')}</DropdownMenu.SubTrigger>
          <DropdownMenu.SubContent>
            <DropdownMenu.RadioGroup
              value={preference}
              onValueChange={(value) => setPreference(value as ThemePreference)}
            >
              {themeOptions.map((option) => (
                <DropdownMenu.RadioItem key={option.value} value={option.value}>
                  {t(option.label)}
                  <DropdownMenu.RadioItemIndicator />
                </DropdownMenu.RadioItem>
              ))}
            </DropdownMenu.RadioGroup>
          </DropdownMenu.SubContent>
        </DropdownMenu.Sub>
        <DropdownMenu.Item icon={KeyboardIcon} onClick={() => shortcuts.setOpen(true)}>
          {t('Shortcuts')}
        </DropdownMenu.Item>
        {help && (
          <DropdownMenu.LinkItem href={help} icon={QuestionIcon} target="_blank" rel="noopener noreferrer">
            {t('Help')}
          </DropdownMenu.LinkItem>
        )}
        <DropdownMenu.Separator />
        <DropdownMenu.Item icon={SignOutIcon} variant="danger" onClick={() => void logOut()}>
          {t('Log out')}
        </DropdownMenu.Item>
      </DropdownMenu.Content>
    </DropdownMenu>
  )
}

/**
 * The global search from md up: submits to the results page (D18). The f key focuses it;
 * keep `md:block` in step with SEARCH_FIELD_MEDIA, below which f opens the palette.
 */
function SearchField({ initial }: { initial: string }) {
  const navigate = useNavigate()
  const t = useT()
  const [value, setValue] = useState(initial)
  const submit = (e: FormEvent) => {
    e.preventDefault()
    const q = value.trim()
    if (q) navigate(`/search?q=${encodeURIComponent(q)}`)
  }
  return (
    <form role="search" onSubmit={submit} className="hidden w-56 md:block">
      <Input
        type="search"
        aria-label={t('Search')}
        placeholder={t('Search all objects')}
        {...shortcutTarget('search')}
        size="sm"
        value={value}
        onChange={(e) => setValue(e.target.value)}
      />
    </form>
  )
}

/** The search field, showing the query while the results page is open. */
function TopbarSearch() {
  const { pathname } = useLocation()
  const [params] = useSearchParams()
  const q = pathname === '/search' ? (params.get('q') ?? '') : ''
  return <SearchField key={q} initial={q} />
}

export function Topbar() {
  const palette = useCommandPalette()
  const t = useT()
  return (
    <header className="z-10 flex h-(--header-height) shrink-0 items-center gap-3 border-b border-kumo-line bg-kumo-base px-4">
      <div className="md:hidden">
        <Sidebar.Trigger />
      </div>
      <PageBreadcrumbs />
      <div className="ml-auto flex items-center gap-1">
        <TopbarSearch />
        <Button
          variant="ghost"
          icon={MagnifyingGlassIcon}
          aria-label={`${t('Search')} (${commandShortcut})`}
          onClick={() => palette.setOpen(true)}
        >
          <span className="hidden sm:inline">{t('Search')}</span>
          <kbd className="ml-1 hidden rounded border border-kumo-hairline px-1.5 text-xs text-kumo-subtle sm:inline">
            {commandShortcut}
          </kbd>
        </Button>
        <CommandMenu open={palette.open} onOpenChange={palette.setOpen} />
        <NotificationsTray />
        <UserMenu />
      </div>
    </header>
  )
}
