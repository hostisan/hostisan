import { it } from 'vitest'
import { expectTranslated } from '../../test/i18nAudit'

it('account pages have no untranslated text', () => expectTranslated('src/designs/account'))
