import { Navigate, type RouteObject } from 'react-router'
import { useSession } from '../../session/SessionProvider'
import { LoginPage } from './LoginPage'
import { ResetPasswordPage } from './ResetPasswordPage'
import { ResetTwofaPage } from './ResetTwofaPage'

/** Hestia sends /reset/ to the login page when POLICY_SYSTEM_PASSWORD_RESET is "no". */
function ResetRoute() {
  const { bootstrap } = useSession()
  if (!bootstrap) return null
  return bootstrap.passwordReset ? <ResetPasswordPage /> : <Navigate to="/login" replace />
}

// Mounted outside the app layout: each page renders its own AuthLayout.
export const routes: RouteObject[] = [
  { path: '/login', element: <LoginPage /> },
  { path: '/reset', element: <ResetRoute /> },
  { path: '/reset-2fa', element: <ResetTwofaPage /> },
]
