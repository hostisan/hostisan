import { LinkProvider, Toasty, TooltipProvider, type LinkComponentProps } from '@cloudflare/kumo'
import { forwardRef, useEffect, type ReactNode } from 'react'
import { Navigate, Outlet, Link as RouterLink, type RouteObject } from 'react-router'
import { setSessionLostHandler } from './api/client'
import { DesignPending } from './designs/DesignPending'
import { routes as accountRoutes } from './designs/account/routes'
import { routes as authRoutes } from './designs/auth/routes'
import { ServerUnreachable } from './designs/auth/ServerUnreachable'
import { routes as backupRoutes } from './designs/backup/routes'
import { routes as cronRoutes } from './designs/cron/routes'
import { routes as dbRoutes } from './designs/db/routes'
import { routes as dnsRoutes } from './designs/dns/routes'
import { routes as mailRoutes } from './designs/mail/routes'
import { routes as serverRoutes } from './designs/server/routes'
import { routes as toolsRoutes } from './designs/tools/routes'
import { routes as usersRoutes } from './designs/users/routes'
import { routes as webRoutes } from './designs/web/routes'
import { AppLayout } from './layout/AppLayout'
import { RequireAdmin } from './layout/RequireAdmin'
import { applyServerTheme } from './lib/theme'
import { useSession } from './session/SessionProvider'

/** The app shell only renders for a signed-in session. */
function RequireSession({ children }: { children: ReactNode }) {
  const { bootstrap } = useSession()
  if (!bootstrap) return null
  if (!bootstrap.authenticated) return <Navigate to="/login" replace />
  return children
}

function Landing() {
  const { bootstrap } = useSession()
  return <Navigate to={bootstrap?.user?.landing ?? '/web'} replace />
}

// Each section owns its routes in src/designs/<section>/routes.tsx. They run as the
// children of Root inside a data router (createBrowserRouter), so useBlocker works.
export const appRoutes: RouteObject[] = [
  ...authRoutes,
  {
    element: (
      <RequireSession>
        <AppLayout />
      </RequireSession>
    ),
    children: [
      { index: true, element: <Landing /> },
      ...webRoutes,
      ...dnsRoutes,
      ...mailRoutes,
      ...dbRoutes,
      ...cronRoutes,
      ...backupRoutes,
      { element: <RequireAdmin />, children: usersRoutes },
      ...accountRoutes,
      { element: <RequireAdmin feature="server" />, children: serverRoutes },
      ...toolsRoutes,
      { path: '*', element: <DesignPending /> },
    ],
  },
]

// Lets Kumo's href-based components navigate through React Router.
const AppLink = forwardRef<HTMLAnchorElement, LinkComponentProps>(({ href, ...rest }, ref) => (
  <RouterLink ref={ref} to={href ?? ''} {...rest} />
))

/** The root route's element: app-wide providers and effects around the matched route. */
export function Root() {
  const { bootstrap, status, refresh, setSignedOutReason } = useSession()

  // When the server ends the session (timeout, IP change, suspension…), remember why
  // and reload the session; RequireSession then sends the visitor to the login page,
  // which shows the reason.
  useEffect(() => {
    setSessionLostHandler((message) => {
      setSignedOutReason(message)
      void refresh()
    })
    return () => setSessionLostHandler(null)
  }, [refresh, setSignedOutReason])

  useEffect(() => applyServerTheme(bootstrap?.theme ?? ''), [bootstrap?.theme])

  return (
    <LinkProvider component={AppLink}>
      <TooltipProvider>
        <Toasty>{status === 'error' ? <ServerUnreachable /> : <Outlet />}</Toasty>
      </TooltipProvider>
    </LinkProvider>
  )
}
