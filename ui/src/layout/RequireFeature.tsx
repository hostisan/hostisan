import { Navigate, Outlet } from 'react-router'
import type { Features } from '../api/types.gen'
import { useSessionUser } from '../session/SessionProvider'

/**
 * Route guard for a page the account's package or the server settings can switch off:
 * without the feature the visitor goes to the account page (as Hestia redirects to the
 * edit page) instead of seeing a page whose API refuses them.
 */
export function RequireFeature({ feature }: { feature: keyof Features }) {
  const user = useSessionUser()
  if (!user) return null
  return user.features[feature] ? <Outlet /> : <Navigate to="/account" replace />
}
