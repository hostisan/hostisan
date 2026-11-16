import { useQuery, useQueryClient } from '@tanstack/react-query'
import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import { api, setCsrfToken } from '../api/client'
import type { Bootstrap } from '../api/types.gen'
import { I18nProvider } from '../i18n/I18nProvider'

type SessionValue = {
  /** Undefined only while the first load is in flight. */
  bootstrap: Bootstrap | undefined
  /** "error": the API is unreachable and nothing can be shown. */
  status: 'loading' | 'ready' | 'error'
  refresh: () => Promise<void>
  /** Language picked on the sign-in screens, before the account's own applies. */
  setLanguage: (lang: string) => void
  /** Why the server ended the session, shown on the login page; "" when none. */
  signedOutReason: string
  setSignedOutReason: (reason: string) => void
}

const SessionContext = createContext<SessionValue>({
  bootstrap: undefined,
  status: 'loading',
  refresh: async () => {},
  setLanguage: () => {},
  signedOutReason: '',
  setSignedOutReason: () => {},
})

/** Loads /bootstrap, keeps the CSRF token current, and provides translations. */
export function SessionProvider({ children }: { children: ReactNode }) {
  const client = useQueryClient()
  const [language, setLanguage] = useState<string | null>(null)
  const [signedOutReason, setSignedOutReason] = useState('')
  const { data, isError } = useQuery({
    queryKey: ['bootstrap'],
    queryFn: async () => {
      const b = await api.get<Bootstrap>('/bootstrap')
      setCsrfToken(b.csrfToken)
      return b
    },
  })
  const bootstrap = data
  const status = data ? 'ready' : !isError ? 'loading' : 'error'
  // The server ended an expired session while answering /bootstrap.
  useEffect(() => {
    if (data?.signedOutReason) setSignedOutReason(data.signedOutReason)
  }, [data])
  const refresh = useCallback(() => client.invalidateQueries({ queryKey: ['bootstrap'] }), [client])
  const lang = (bootstrap?.authenticated ? bootstrap.language : (language ?? bootstrap?.language)) ?? 'en'
  return (
    <SessionContext.Provider value={{ bootstrap, status, refresh, setLanguage, signedOutReason, setSignedOutReason }}>
      <I18nProvider lang={lang} version={bootstrap?.version}>
        {children}
      </I18nProvider>
    </SessionContext.Provider>
  )
}

export function useSession(): SessionValue {
  return useContext(SessionContext)
}

/** The signed-in user, or undefined; for components inside the authenticated shell. */
export function useSessionUser() {
  return useSession().bootstrap?.user ?? undefined
}
