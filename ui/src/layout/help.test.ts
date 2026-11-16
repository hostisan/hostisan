import { describe, expect, it } from 'vitest'
import { docsUrl, helpLink } from './help'

describe('helpLink', () => {
  it('returns the docs URL when docs are shown', () => {
    expect(helpLink(false, 'https://x')).toBe('https://x')
  })

  it('returns null when HIDE_DOCS is set', () => {
    expect(helpLink(true, 'https://x')).toBeNull()
  })

  it('returns null while there is no docs site', () => {
    expect(helpLink(false, '')).toBeNull()
    expect(helpLink(false, docsUrl)).toBeNull()
  })
})
