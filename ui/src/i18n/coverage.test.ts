import { describe, it } from 'vitest'
import { expectTranslated } from '../test/i18nAudit'

// Plan 7 Task 10: the whole tree, not just the folders each track swept.
describe('translations', () => {
  it('no file in ui/src has untranslated text', () => expectTranslated('src'))
})
