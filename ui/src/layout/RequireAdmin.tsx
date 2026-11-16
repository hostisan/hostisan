import { Navigate, Outlet } from 'react-router'
import type { Features } from '../api/types.gen'
import { useSessionUser } from '../session/SessionProvider'

/**
 * Route guard for the admin-only sections (owner ruling 23): anyone who isn't an admin,
 * or an admin logged in as another user, goes to their own landing page instead of a
 * page the API refuses. With feature, the section also needs that feature (the server
 * pages are hidden from a non-root admin under POLICY_SYSTEM_HIDE_SERVICES).
 */
export function RequireAdmin({ feature }: { feature?: keyof Features }) {
  const user = useSessionUser()
  if (!user) return null
  const allowed = user.role === 'admin' && user.look === '' && (!feature || user.features[feature])
  return allowed ? <Outlet /> : <Navigate to={user.landing || '/web'} replace />
}
