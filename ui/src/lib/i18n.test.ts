import { expect, it } from 'vitest'
import { findUntranslated } from '../test/i18nAudit'

// date.ts and language.ts are Task 0 files; the rest of lib/ and api/errors.ts are B4's.
const frozen = ['src/lib/date.ts', 'src/lib/language.ts']

it('lib and api errors have no untranslated text', () => {
  const lines = findUntranslated(['src/lib'])
    .filter((f) => !frozen.includes(f.file))
    .concat(findUntranslated(['src/api']).filter((f) => f.file === 'src/api/errors.ts'))
    .map((f) => `${f.file}:${f.line} ${f.kind} ${JSON.stringify(f.text)}`)
  expect(lines, 'UI text that does not go through t()').toEqual([])
})
