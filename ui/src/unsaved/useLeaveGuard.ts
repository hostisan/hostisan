import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useLayoutEffect, useRef } from 'react'
import { useBlocker, type Blocker, type BlockerFunction } from 'react-router'
import type { Bootstrap } from '../api/types.gen'

/** Who is signed in, as the session cache knows it right now. */
function identity(b: Bootstrap | undefined): string {
  if (!b?.authenticated) return ''
  return `${b.user?.name ?? ''}\u0000${b.user?.look ?? ''}`
}

/**
 * Blocks in-app navigation away from the page while `armed()` is true, and asks the
 * browser to confirm a reload or close. `armed` is read at navigation time, so a save
 * that disarms and navigates in the same tick is never blocked.
 *
 * Never blocked: a move to /login (a server-ended session, log out), a change of the
 * query or hash on the same page, and any navigation once the signed-in identity has
 * changed (log out while viewing as another user).
 */
export function useLeaveGuard(armed: () => boolean): { blocker: Blocker } {
  const client = useQueryClient()
  const armedRef = useRef(armed)
  // The first signed-in identity this page saw; a later different one means log out.
  const signedIn = useRef('')
  useLayoutEffect(() => {
    armedRef.current = armed
    if (!signedIn.current) signedIn.current = identity(client.getQueryData<Bootstrap>(['bootstrap']))
  })

  const shouldBlock: BlockerFunction = ({ currentLocation, nextLocation }) => {
    if (nextLocation.pathname === '/login') return false
    if (nextLocation.pathname === currentLocation.pathname) return false
    const now = identity(client.getQueryData<Bootstrap>(['bootstrap']))
    if (signedIn.current && now !== signedIn.current) return false
    return armedRef.current()
  }
  const blocker = useBlocker(shouldBlock)

  useEffect(() => {
    // Checked when the event fires, so it only asks while there are changes.
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      if (!armedRef.current()) return
      e.preventDefault()
      // Chromium before 119 asks only for a truthy returnValue; older browsers read
      // the returned string. The browser writes its own text either way.
      e.returnValue = true
      return ''
    }
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => window.removeEventListener('beforeunload', onBeforeUnload)
  }, [])

  return { blocker }
}
