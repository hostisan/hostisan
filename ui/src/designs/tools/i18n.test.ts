import { it } from 'vitest'
import { expectTranslated } from '../../test/i18nAudit'

it('tools pages have no untranslated text', () => expectTranslated('src/designs/tools'))
