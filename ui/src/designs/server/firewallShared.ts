import { ApiError } from '../../api/client'
import { apiErrorText } from '../../api/errors'
import { useT } from '../../i18n/I18nProvider'

// Firewall helpers shared by the rules, ban and IP-list pages.

/** The chains a ban can target (the core's list). */
export const banChains = ['SSH', 'WEB', 'FTP', 'DNS', 'MAIL', 'DB', 'HESTIA']

/** The core stores the panel rule's comment as HESTIA; the screen calls it Panel. */
export const ruleComment = (comment: string) => (comment === 'HESTIA' ? 'Panel' : comment)

/** What a failed rule action shows: the refusal, plus a note when the list was stale. */
export function useRuleError() {
  const t = useT()
  return (err: unknown): string => {
    const text = apiErrorText(err, t)
    return err instanceof ApiError && err.code === 'changed'
      ? `${text}\n${t('The firewall was changed elsewhere. The list has been reloaded.')}`
      : text
  }
}
