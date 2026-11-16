import { useCallback } from 'react'
import { useNavigate } from 'react-router'
import { api } from '../../api/client'
import type { StatusReply } from '../../api/types.gen'
import { useSession } from '../../session/SessionProvider'

/**
 * Returns a function that signs the admin in as another user, refreshes the session and
 * goes to that user's landing page. It throws the API error for the caller to show.
 */
export function useLoginAs() {
  const { refresh } = useSession()
  const navigate = useNavigate()
  return useCallback(
    async (user: string) => {
      const r = await api.post<StatusReply>('/auth/login-as', { user })
      await refresh()
      navigate(r.landing || '/')
    },
    [refresh, navigate],
  )
}
