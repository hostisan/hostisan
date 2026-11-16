import { describe, expect, it } from 'vitest'
import type { Translate } from '../../i18n/I18nProvider'
import { describeCron, formatServerTime } from './describeCron'

// English: the msgid itself, with its placeholders filled.
const en: Translate = (msgid, ...args) => {
  let i = 0
  return msgid.replace(/%[sd]/g, () => String(args[i++]))
}

describe('describeCron', () => {
  it.each([
    [['*', '*', '*', '*', '*'], 'Every minute'],
    [['*/5', '*', '*', '*', '*'], 'Every 5 minutes'],
    [['0', '*', '*', '*', '*'], 'Every hour at minute 00'],
    [['15', '*/6', '*', '*', '*'], 'Every 6 hours at minute 15'],
    [['0', '12', '*', '*', '*'], 'Every day at 12:00'],
    [['30', '3', '*', '*', '1,2,3,4,5'], 'Every weekday at 03:30'],
    [['30', '3', '*', '*', '0,6'], 'Every weekend at 03:30'],
    [['30', '3', '*', '*', '1,5'], 'Every Monday, Friday at 03:30'],
    [['0', '12', '1', '*', '*'], 'Monthly on the 1st at 12:00'],
    [['0', '12', '22', '*', '*'], 'Monthly on the 22nd at 12:00'],
    [['0', '12', '13', '*', '*'], 'Monthly on the 13th at 12:00'],
    [['0', '12', '5', '1,6', '*'], 'On the 5th in Jan, Jun at 12:00'],
    [['0', '12', '*', '*/3', '*'], 'Every 3 months at 12:00'],
    [['0', '*/6', '1-31/2', '*', '0,6'], 'Custom schedule'],
  ])('%j', (f, text) => {
    expect(describeCron(f[0], f[1], f[2], f[3], f[4], en)).toBe(text)
  })
})

describe('formatServerTime', () => {
  it('gives d Mon Y, H:i in the zone, with the month translated', () => {
    expect(formatServerTime('2026-10-21T12:00:00Z', 'UTC', en)).toBe('21 Oct 2026, 12:00')
    expect(formatServerTime('2026-10-21T23:30:00Z', 'Europe/Paris', en)).toBe('22 Oct 2026, 01:30')
    expect(formatServerTime('2026-01-05T00:05:00Z', 'Not/AZone', en)).toBe('05 Jan 2026, 00:05')
    const fr: Translate = (m, ...a) => en(m === 'Oct' ? 'oct.' : m, ...a)
    expect(formatServerTime('2026-10-21T12:00:00Z', 'UTC', fr)).toBe('21 oct. 2026, 12:00')
  })
})
