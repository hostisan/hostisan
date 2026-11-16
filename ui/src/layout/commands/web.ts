import { GlobeIcon, PlusIcon } from '@phosphor-icons/react'
import { useMemo } from 'react'
import { useWebDomains } from '../../api/web'
import type { WebDomains } from '../../api/types.gen'
import type { Translate } from '../../i18n/I18nProvider'
import { useT } from '../../i18n/I18nProvider'
import { useSessionUser } from '../../session/SessionProvider'
import { noCommands, type CommandContribution } from './types'

const same: Translate = (s) => s

/**
 * The palette entries for the signed-in user: nothing without the web feature, nothing
 * to add when read-only. Titles and labels come back translated by t.
 */
export function webCommands(
  web: boolean,
  readOnly: boolean,
  data: WebDomains | undefined,
  t: Translate = same,
): CommandContribution {
  if (!web) return noCommands
  return {
    actions: readOnly ? [] : [{ id: 'new:web', title: t('Add web domain'), icon: PlusIcon, href: '/web/new' }],
    pages: [],
    groups: [
      {
        id: 'web',
        label: t('Web domains'),
        items: (data?.domains ?? []).map((d) => ({
          id: `web:${d.NAME}`,
          title: d.NAME,
          hint: data?.displayIps[d.IP] ?? d.IP,
          icon: GlobeIcon,
          href: `/web/${d.NAME}`,
          keywords: d.ALIAS,
        })),
      },
    ],
  }
}

/** The web section's palette entries: "Add web domain" and the user's web domains. */
export function useWebCommands(open: boolean): CommandContribution {
  const t = useT()
  const user = useSessionUser()
  const web = user?.features.web ?? false
  const readOnly = user?.readOnly ?? false
  const data = useWebDomains({ enabled: open && web }).data
  return useMemo(() => webCommands(web, readOnly, data, t), [web, readOnly, data, t])
}
