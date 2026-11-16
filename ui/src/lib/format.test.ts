import { describe, expect, it } from 'vitest'
import { formatMB } from './format'

// Hestia's humanize_usage_size + humanize_usage_measure (users §7.15), unit upper-cased.
describe('formatMB', () => {
  it.each([
    ['0', '0.00 MB'],
    ['512', '512.00 MB'],
    ['1000', '0.98 GB'],
    ['1024', '1.00 GB'],
    ['7342', '7.17 GB'],
    ['1048576', '1.00 TB'],
    ['unlimited', '∞'],
  ])('%s MB -> %s', (input, want) => {
    expect(formatMB(input)).toBe(want)
  })

  it('rounds as asked (the users list uses one decimal)', () => {
    expect(formatMB('7342', 1)).toBe('7.2 GB')
    expect(formatMB('5000', 1)).toBe('4.9 GB')
  })
})
