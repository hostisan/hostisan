import { describe, expect, it } from 'vitest'
import { rewriteBrand } from './brand'

describe('rewriteBrand', () => {
  it('replaces the longest form first', () => {
    expect(rewriteBrand('Hestia Control Panel')).toBe('Hostisan')
    expect(rewriteBrand('HestiaCP')).toBe('Hostisan')
    expect(rewriteBrand('Restrict phpMyAdmin to Hestia login')).toBe('Restrict phpMyAdmin to Hostisan login')
    expect(rewriteBrand('nothing to change')).toBe('nothing to change')
  })
})
