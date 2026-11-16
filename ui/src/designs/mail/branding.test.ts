import { describe, expect, it } from 'vitest'

/** Source with block, JSX and line comments removed (`://` in URLs is kept). */
function withoutComments(src: string): string {
  return src.replace(/\/\*[\s\S]*?\*\//g, '').replace(/(^|[^:])\/\/.*$/gm, '$1')
}

describe('branding', () => {
  it('no mail screen shows the upstream panel name', () => {
    const files = import.meta.glob(['./*.tsx', '!./*.test.tsx'], {
      query: '?raw',
      import: 'default',
      eager: true,
    }) as Record<string, string>
    expect(Object.keys(files).length).toBeGreaterThanOrEqual(8)
    for (const [file, src] of Object.entries(files)) {
      const hits = withoutComments(src)
        .split('\n')
        .filter((line) => /Hestia/.test(line))
      expect(hits, file).toEqual([])
    }
  })
})
