import type { Translate } from '../i18n/I18nProvider'
import { msgid } from '../i18n/msgid'

/** Hestia's month msgids, January first. */
export const MONTHS = [
  msgid('Jan'),
  msgid('Feb'),
  msgid('Mar'),
  msgid('Apr'),
  msgid('May'),
  msgid('Jun'),
  msgid('Jul'),
  msgid('Aug'),
  msgid('Sep'),
  msgid('Oct'),
  msgid('Nov'),
  msgid('Dec'),
] as const

/**
 * "2026-10-05" as "05 Oct 2026" with the month translated: Hestia's translate_date
 * (PHP "d M Y"). Anything that isn't a Y-m-d date comes back unchanged.
 */
export function translateDate(ymd: string, t: Translate): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(ymd)
  const month = m ? MONTHS[Number(m[2]) - 1] : undefined
  if (!m || !month) return ymd
  return `${m[3]} ${t(month)} ${m[1]}`
}
