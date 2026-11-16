import { describe, expect, it } from 'vitest'
import { findUntranslated } from '../test/i18nAudit'

// Track C's files: the shell (B4 audits the rest of src/layout), the page header,
// the table kit and the shortcuts.
const files = [
  'src/layout/AppLayout.tsx',
  'src/layout/AppSidebar.tsx',
  'src/layout/Topbar.tsx',
  'src/layout/CommandMenu.tsx',
  'src/layout/nav.ts',
  'src/components/PageHeader.tsx',
]
const dirs = ['src/components/table', 'src/shortcuts']

describe('shell and table text', () => {
  it('the shell, tables and shortcuts have no untranslated text', () => {
    const lines = findUntranslated(['src/layout', 'src/components', ...dirs])
      .filter((f) => files.includes(f.file) || dirs.some((d) => f.file.startsWith(`${d}/`)))
      .map((f) => `${f.file}:${f.line} ${f.kind} ${JSON.stringify(f.text)}`)
    expect(lines, 'UI text that does not go through t()').toEqual([])
  })
})
