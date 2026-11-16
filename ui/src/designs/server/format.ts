import type { Translate } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { StepMsgids } from '../../api/types.gen'
import { humanizeUsage } from '../../lib/format'

export { humanizeUsage }

// Display helpers shared by the server screens. They port the PHP panel's humanize_*
// helpers (web/inc/main.php; sysinfo checklist §6.5) so values read as they did.

/**
 * humanize_time: minutes as "N minutes", or rounded to hours from 60 minutes, or to
 * days from 24 (rounded) hours. Hestia reports uptime and service RTIME in minutes.
 * Pass the screen's t (useT): the units are Hestia's msgids ("%d minute",
 * "%d minutes", …), picked by count as ngettext does for English.
 */
export function humanizeMinutes(value: string, t: (msgid: string) => string): string {
  if (value.trim() === '') return '-'
  const minutes = Number(value)
  if (!Number.isFinite(minutes)) return '-'
  const plural = (n: number, one: string, many: string) => t(n === 1 ? one : many).replace('%d', () => String(n))
  if (minutes < 60) return plural(Math.round(minutes), msgid('%d minute'), msgid('%d minutes'))
  const hours = Math.round(minutes / 60)
  if (hours < 24) return plural(hours, msgid('%d hour'), msgid('%d hours'))
  return plural(Math.round(hours / 24), msgid('%d day'), msgid('%d days'))
}

/** humanizeUsage as one string: "2.00 gb", or "∞". */
export function formatUsage(mb: string, round = 2): string {
  const { size, measure } = humanizeUsage(mb, round)
  return measure ? `${size} ${measure}` : size
}

/** Bytes per second as B/s, KB/s, MB/s or GB/s (the network graphs, decision 23). */
export function formatBytesPerSecond(bytes: number): string {
  const units = ['KB/s', 'MB/s', 'GB/s']
  if (Math.abs(bytes) < 1024) return `${Math.round(bytes)} B/s`
  let value = bytes / 1024
  let i = 0
  while (Math.abs(value) >= 1024 && i < units.length - 1) {
    value /= 1024
    i++
  }
  return `${value.toFixed(1)} ${units[i]}`
}

const acronyms: Record<string, string> = { php: 'PHP', dns: 'DNS', ftp: 'FTP', ssh: 'SSH', 'imap/pop3': 'IMAP/POP3' }

// Hestia's lowercase service roles ("php interpreter") in sentence case.
export function formatServiceRole(system: string): string {
  const words = system.split(' ').map((w) => acronyms[w] ?? w)
  const first = words[0]
  words[0] = first === first.toUpperCase() ? first : first.charAt(0).toUpperCase() + first.slice(1)
  return words.join(' ')
}

/** The core's title case ("Memory Usage") in sentence case, keeping names like NGINX and MySQL. */
export function sentenceCase(title: string): string {
  return title
    .split(' ')
    .map((w, i) => (i === 0 || /[A-Z]/.test(w.slice(1)) ? w : w.toLowerCase()))
    .join(' ')
}

/**
 * One pattern per generated StepMsgids entry (hostisan/internal/httpapi/steps.go), the
 * most specific first: the wire sends a step as one string ("Check nginx"), and %s
 * matches its argument.
 */
const stepPatterns = StepMsgids.map((id) => ({
  id,
  re: new RegExp(`^${id.replace(/[.*+?^${}()|[\]\\]/g, '\\$&').replace('%s', '(.+)')}$`),
}))

/**
 * A job step or title as the user reads it. A step that is a msgid of its own
 * ("Restart server", "Check services") is translated whole; otherwise the first
 * matching StepMsgids pattern is translated with the matched text as its argument.
 */
export function stepText(step: string, t: Translate): string {
  if (t(step) !== step) return t(step)
  for (const { id, re } of stepPatterns) {
    const m = re.exec(step)
    if (m) return t(id, m[1])
  }
  return t(step)
}

/**
 * The chart x-axis label for a period, in the browser's locale and time zone: the time
 * for a day, the date for a week to a year, and the month and year for two and three
 * years (whose axis spans several years).
 */
export function axisTimeFormat(period: string): (v: number) => string {
  const options: Intl.DateTimeFormatOptions =
    period === 'daily'
      ? { hour: '2-digit', minute: '2-digit' }
      : period === 'biennially' || period === 'triennially'
        ? { month: 'short', year: 'numeric' }
        : { month: 'short', day: 'numeric' }
  return (v) => new Date(v).toLocaleString(undefined, options)
}
