import type { Translate } from '../i18n/I18nProvider'
import { ApiError } from './client'
import type { BulkFailure } from './types.gen'

/**
 * Messages whose args are field labels (msgids the UI also shows), so the args are
 * translated too. Every other message's args are values: names, ports or text a user
 * typed, shown as sent.
 */
export const argMsgids: readonly string[] = ['Field "%s" can not be blank.', 'Field "%s" is invalid.']

/**
 * The text to show for a failed request. Validation errors carry args that fill the %s
 * of their message: in order when the message has one %s per arg, otherwise joined into
 * the first %s (a list of blank fields). Hestia's own output has no args and is shown as
 * sent (translated only if the catalog knows it). Messages may span lines, so render
 * with whitespace-pre-line.
 */
export function apiErrorText(err: unknown, t: Translate): string {
  if (err instanceof ApiError && err.message) return messageText(err.message, err.args, t)
  if (err instanceof ApiError && err.status === 403) return t("You don't have permission to view this page.")
  if (err instanceof ApiError && err.code === 'timeout') return t('The server took too long to respond. Try again.')
  return t('Something went wrong. Try again.')
}

/** The queries' retry rule: one retry, but never for a 403 (asking again won't help). */
export function retryQuery(failureCount: number, err: unknown): boolean {
  if (err instanceof ApiError && err.status === 403) return false
  return failureCount < 1
}

/** message translated, its %s filled from args as apiErrorText does (a job's failure, say). */
export function messageText(message: string, rawArgs: string[], t: Translate): string {
  const text = t(message)
  const args = argMsgids.includes(message) ? rawArgs.map((a) => t(a)) : rawArgs
  if (!args.length) return text
  if (text.split('%s').length - 1 === args.length) {
    let i = 0
    return text.replace(/%s/g, () => args[i++])
  }
  return text.replace('%s', () => args.join(', '))
}

/** One refused bulk item's message: Hestia's output, or a msgid filled from its args. */
export function bulkMessage(f: BulkFailure, t: Translate): string {
  return messageText(f.message, f.args ?? [], t)
}

/** One refused bulk item as "name: message", for a list of failures. */
export function failureText(f: BulkFailure, t: Translate): string {
  return `${f.name}: ${bulkMessage(f, t)}`
}
