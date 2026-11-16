import { Banner, Sidebar } from '@cloudflare/kumo'
import { useEffect, useRef, useState } from 'react'
import { Outlet, useLocation, useNavigate } from 'react-router'
import { PageAsideSlot } from '../components/PageAside'
import { api } from '../api/client'
import { useT } from '../i18n/I18nProvider'
import { useSession, useSessionUser } from '../session/SessionProvider'
import { ShortcutsDialog } from '../shortcuts/ShortcutsDialog'
import { ShortcutsProvider } from '../shortcuts/ShortcutsProvider'
import { AppFooter } from './AppFooter'
import { AppSidebar } from './AppSidebar'
import { Topbar } from './Topbar'

function ImpersonationBanner() {
  const user = useSessionUser()
  const { refresh } = useSession()
  const navigate = useNavigate()
  const t = useT()
  if (!user?.look) return null
  // Logging out while viewing as a user ends the view and returns to the admin account.
  const returnToAdmin = async () => {
    await api.post('/auth/logout')
    await refresh()
    navigate('/', { replace: true })
  }
  return (
    <Banner
      variant="alert"
      className="shrink-0 border-b border-kumo-line"
      title={t('You are viewing the panel as %s.', user.look)}
      action={<Banner.Action onClick={() => void returnToAdmin()}>{t('Return to %s', user.name)}</Banner.Action>}
    />
  )
}

function ReadOnlyBanner() {
  const user = useSessionUser()
  const t = useT()
  if (!user?.readOnly) return null
  return (
    <Banner variant="secondary" className="shrink-0 border-b border-kumo-line">
      {t('This account is read-only. You can look around, but changes are disabled.')}
    </Banner>
  )
}

/**
 * The shell is exactly one viewport tall: the sidebar and top bar stay put and the
 * content area scrolls. The footer scrolls with the content: it follows the end of
 * long pages and sits at the bottom of short ones.
 */
export function AppLayout() {
  const { pathname } = useLocation()
  const scrollRef = useRef<HTMLDivElement>(null)
  const [asideSlot, setAsideSlot] = useState<HTMLElement | null>(null)
  const t = useT()

  // The window no longer scrolls, so reset the content pane on navigation.
  useEffect(() => {
    scrollRef.current?.scrollTo({ top: 0 })
  }, [pathname])

  return (
    <ShortcutsProvider>
      <Sidebar.Provider defaultOpen peekable className="h-svh overflow-clip">
        <AppSidebar />
        <div className="flex h-svh min-w-0 flex-1 flex-col">
          <Topbar />
          <ImpersonationBanner />
          <ReadOnlyBanner />
          <div ref={scrollRef} className="flex min-h-0 flex-1 flex-col overflow-y-auto">
            <div className="flex flex-1 flex-col 3xl:flex-row">
              <main className="mx-auto w-full max-w-6xl min-w-0 flex-1 px-4 py-5 sm:px-6 sm:py-6">
                <PageAsideSlot.Provider value={asideSlot}>
                  <Outlet />
                </PageAsideSlot.Provider>
              </main>
              {/* Filled by <PageAside>; hidden while empty. A full-height rail from 3xl (1760px),
                where wide tables still fit beside it; below that it stacks under the page. */}
              <aside
                ref={setAsideSlot}
                aria-label={t('Summary')}
                className="mx-auto grid w-full max-w-6xl grid-cols-[minmax(0,1fr)] content-start items-start gap-6 border-t border-kumo-line px-4 py-5 empty:hidden sm:grid-cols-2 sm:px-6 3xl:mx-0 3xl:w-88 3xl:max-w-none 3xl:shrink-0 3xl:grid-cols-[minmax(0,1fr)] 3xl:border-t-0 3xl:border-l 3xl:py-6"
              />
            </div>
            <AppFooter />
          </div>
        </div>
      </Sidebar.Provider>
      <ShortcutsDialog />
    </ShortcutsProvider>
  )
}
