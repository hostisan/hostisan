import { KumoLocaleProvider } from '@cloudflare/kumo'
import { useQuery } from '@tanstack/react-query'
import { createContext, useCallback, useContext, useEffect, useMemo, type ReactNode } from 'react'
import { rewriteBrand } from './brand'

// UI strings are Hestia's English msgids, so the server's gettext catalogs translate
// them directly. Missing entries fall back to the English text, with Hestia's name
// replaced by Hostisan's.

/**
 * Translates a msgid and fills its %s/%d placeholders with `args`, in order. When the
 * placeholder count differs from the number of args, the text comes back unfilled.
 */
export type Translate = (msgid: string, ...args: Array<string | number>) => string

const PLACEHOLDER = /%[sd]/g

function fill(text: string, args: Array<string | number>): string {
  if (args.length === 0 || (text.match(PLACEHOLDER)?.length ?? 0) !== args.length) return text
  let i = 0
  return text.replace(PLACEHOLDER, () => String(args[i++]))
}

const I18nContext = createContext<Translate>((msgid, ...args) => fill(msgid, args))

/**
 * Loads `lang`'s catalog and provides t(). `version` (the server's) is part of the
 * catalog URL, so an upgrade isn't hidden behind the browser's cache.
 */
export function I18nProvider({ lang, version, children }: { lang: string; version?: string; children: ReactNode }) {
  const { data } = useQuery({
    queryKey: ['i18n', lang, version ?? ''],
    queryFn: async (): Promise<Record<string, string>> => {
      const query = version ? `?v=${encodeURIComponent(version)}` : ''
      const res = await fetch(`/api/v1/i18n/${encodeURIComponent(lang)}${query}`, { credentials: 'same-origin' })
      // A failure is an error, not an empty catalogue: English shows meanwhile, and
      // the next load asks again instead of keeping {} forever.
      if (!res.ok) throw new Error(`i18n ${lang}: HTTP ${res.status}`)
      return res.json()
    },
    enabled: lang !== 'en',
    staleTime: Infinity,
  })
  const t = useCallback<Translate>((msgid, ...args) => fill(data?.[msgid] || rewriteBrand(msgid), args), [data])

  useEffect(() => {
    document.documentElement.lang = lang
  }, [lang])

  // Kumo's own copy, such as the close button of its dialogs.
  const kumo = useMemo(() => ({ layerDialog: { close: t('Close'), cancel: t('Cancel') } }), [t])

  return (
    <I18nContext.Provider value={t}>
      <KumoLocaleProvider translations={kumo}>{children}</KumoLocaleProvider>
    </I18nContext.Provider>
  )
}

export function useT(): Translate {
  return useContext(I18nContext)
}
