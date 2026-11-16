import type { Translate } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { MONTHS, translateDate } from '../../lib/date'

/** Hestia's weekday msgids, Sunday first (cron's 0). */
export const WEEKDAYS = [
  msgid('Sunday'),
  msgid('Monday'),
  msgid('Tuesday'),
  msgid('Wednesday'),
  msgid('Thursday'),
  msgid('Friday'),
  msgid('Saturday'),
] as const

const isNum = (v: string) => /^\d+$/.test(v)
const pad = (v: string) => v.padStart(2, '0')
const step = (v: string) => /^\*\/(\d+)$/.exec(v)?.[1]

/** "1st", "2nd", "11th": each suffix is its own msgid, so a language can drop or change it. */
function ordinal(n: number, t: Translate) {
  const v = n % 100
  if (v >= 11 && v <= 13) return t('%sth', n)
  switch (n % 10) {
    case 1:
      return t('%sst', n)
    case 2:
      return t('%snd', n)
    case 3:
      return t('%srd', n)
    default:
      return t('%sth', n)
  }
}

const dayNames = (days: string[], t: Translate) => days.map((d) => t(WEEKDAYS[Number(d) % 7])).join(', ')

function describeWday(wday: string, t: Translate): string | null {
  if (wday === '*') return null
  if (wday === '1,2,3,4,5' || wday === '1-5') return t('on weekdays')
  if (wday === '0,6' || wday === '6,0') return t('on weekends')
  const days = wday.split(',')
  if (days.every(isNum)) return t('on %s', dayNames(days, t))
  return t('on weekdays %s', wday)
}

function describeMonth(month: string, t: Translate): string | null {
  if (month === '*') return null
  if (month === '1-11/2') return t('in odd months')
  if (month === '*/2') return t('in even months')
  const n = step(month)
  if (n) return t('every %s months', n)
  const list = month.split(',')
  if (list.every(isNum)) {
    return t('in %s', list.map((m) => (MONTHS[Number(m) - 1] ? t(MONTHS[Number(m) - 1]) : m)).join(', '))
  }
  return t('in months %s', month)
}

function describeDay(day: string, t: Translate): string | null {
  if (day === '*') return null
  if (day === '1-31/2') return t('on odd days')
  if (day === '*/2') return t('on even days')
  const n = step(day)
  if (n) return t('every %s days', n)
  if (isNum(day)) return t('on the %s', ordinal(Number(day), t))
  return t('on days %s', day)
}

/** Turns Hestia's five cron fields into a short sentence such as "Every day at 03:00". */
export function describeCron(min: string, hour: string, day: string, month: string, wday: string, t: Translate): string {
  const all = (...v: string[]) => v.every((x) => x === '*')
  const minStep = step(min)
  const hourStep = step(hour)

  if (all(min, hour, day, month, wday)) return t('Every minute')
  if (minStep && all(hour, day, month, wday)) return t('Every %s minutes', minStep)
  if (isNum(min) && all(hour, day, month, wday)) return t('Every hour at minute %s', pad(min))
  if (isNum(min) && hourStep && all(day, month, wday)) return t('Every %s hours at minute %s', hourStep, pad(min))

  if (isNum(min) && isNum(hour)) {
    const time = `${pad(hour)}:${pad(min)}`
    const parts = [describeDay(day, t), describeWday(wday, t), describeMonth(month, t)].filter(Boolean)
    if (parts.length === 0) return t('Every day at %s', time)
    if (day === '*' && month === '*' && wday !== '*') {
      if (wday === '1,2,3,4,5' || wday === '1-5') return t('Every weekday at %s', time)
      if (wday === '0,6' || wday === '6,0') return t('Every weekend at %s', time)
      const days = wday.split(',')
      const which = days.every(isNum) ? dayNames(days, t) : t('weekdays %s', wday)
      return t('Every %s at %s', which, time)
    }
    if (isNum(day) && month === '*' && wday === '*') return t('Monthly on the %s at %s', ordinal(Number(day), t), time)
    const first = parts.join(' ')
    return t('%s at %s', `${first.charAt(0).toUpperCase()}${first.slice(1)}`, time)
  }

  return t('Custom schedule')
}

/**
 * The server's time as "21 Oct 2026, 12:00" in its own time zone (UTC for an unknown
 * zone), with the month translated.
 */
export function formatServerTime(iso: string, timeZone: string, t: Translate): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  const opts: Intl.DateTimeFormatOptions = {
    day: '2-digit',
    month: '2-digit',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  }
  let parts: Intl.DateTimeFormatPart[]
  try {
    parts = new Intl.DateTimeFormat('en-GB', { ...opts, timeZone }).formatToParts(d)
  } catch {
    parts = new Intl.DateTimeFormat('en-GB', { ...opts, timeZone: 'UTC' }).formatToParts(d)
  }
  const part = (type: Intl.DateTimeFormatPartTypes) => parts.find((p) => p.type === type)?.value ?? ''
  return `${translateDate(`${part('year')}-${part('month')}-${part('day')}`, t)}, ${part('hour')}:${part('minute')}`
}
