import { describe, it } from 'vitest'
import { expectTranslated } from '../../test/i18nAudit'

describe('fields i18n', () => {
  it('every field string goes through t()', () => {
    expectTranslated('src/components/fields')
  })
})
