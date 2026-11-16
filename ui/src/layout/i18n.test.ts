import { expect, it } from 'vitest'
import { findUntranslated } from '../test/i18nAudit'

// Track C owns the shell files and audits them in shortcuts-i18n.test.ts.
const trackC = ['AppLayout.tsx', 'AppSidebar.tsx', 'Topbar.tsx', 'CommandMenu.tsx', 'nav.ts'].map((f) => `src/layout/${f}`)

it('layout files have no untranslated text', () => {
  const lines = findUntranslated(['src/layout'])
    .filter((f) => !trackC.includes(f.file))
    .map((f) => `${f.file}:${f.line} ${f.kind} ${JSON.stringify(f.text)}`)
  expect(lines, 'UI text that does not go through t()').toEqual([])
})
