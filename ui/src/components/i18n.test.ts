import { expect, it } from 'vitest'
import { findUntranslated } from '../test/i18nAudit'

// FormPage and fields/ are audited by Track D, PageHeader and table/ by Track C.
// ConfirmDialog is a Task 0 file. Everything else here, plus DesignPending.
const others = (file: string) =>
  file === 'src/components/FormPage.tsx' ||
  file === 'src/components/PageHeader.tsx' ||
  file === 'src/components/ConfirmDialog.tsx' ||
  file.startsWith('src/components/fields/') ||
  file.startsWith('src/components/table/')

it('shared components have no untranslated text', () => {
  const lines = findUntranslated(['src/components'])
    .filter((f) => !others(f.file))
    .concat(findUntranslated(['src/designs']).filter((f) => f.file === 'src/designs/DesignPending.tsx'))
    .map((f) => `${f.file}:${f.line} ${f.kind} ${JSON.stringify(f.text)}`)
  expect(lines, 'UI text that does not go through t()').toEqual([])
})
