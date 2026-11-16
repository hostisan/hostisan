import { describe, expect, it } from 'vitest'

// Screens read the API: nothing shows design data (owner ruling 21).
describe('no mock data', () => {
  it('no screen imports mocks, and there are none', () => {
    const files = import.meta.glob('../**/*.{ts,tsx}', { query: '?raw', import: 'default', eager: true }) as Record<
      string,
      string
    >
    const importers = Object.entries(files)
      .filter(([file, src]) => !file.includes('.test.') && /from '[./]*\/?mocks\//.test(src))
      .map(([file]) => file)
    expect(importers).toEqual([])
    expect(Object.keys(files).filter((f) => f.startsWith('../mocks/'))).toEqual([])
  })
})
