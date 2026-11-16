import { describe, expect, it } from 'vitest'
import { nativeLanguageName } from './language'

describe('nativeLanguageName', () => {
  it('names a language in itself, capitalised as a menu item', () => {
    expect(nativeLanguageName('de')).toBe('Deutsch')
    expect(nativeLanguageName('fr')).toBe('Français')
    expect(nativeLanguageName('en')).toBe('English')
  })
  it('falls back to the code', () => {
    expect(nativeLanguageName('zz-not-a-language-tag!')).toBe('zz-not-a-language-tag!')
  })
})
