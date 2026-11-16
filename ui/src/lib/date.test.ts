import { describe, expect, it } from 'vitest'
import { MONTHS, translateDate } from './date'

const fr: Record<string, string> = { Jan: 'janv.', Oct: 'oct.' }
const t = (msgid: string) => fr[msgid] ?? msgid

describe('translateDate', () => {
  it('translateDate gives d Mon Y with translated months', () => {
    expect(translateDate('2026-10-20', t)).toBe('20 oct. 2026')
    // PHP's "d" keeps the leading zero.
    expect(translateDate('2027-01-05', t)).toBe('05 janv. 2027')
    expect(translateDate('2026-05-31', t)).toBe('31 May 2026')
  })

  it('leaves anything that is not a Y-m-d date alone', () => {
    expect(translateDate('', t)).toBe('')
    expect(translateDate('2026-13-01', t)).toBe('2026-13-01')
    expect(translateDate('yesterday', t)).toBe('yesterday')
  })

  it('lists the twelve month msgids in order', () => {
    expect(MONTHS).toEqual(['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'])
  })
})
