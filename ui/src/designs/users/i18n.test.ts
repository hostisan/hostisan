import { it } from 'vitest'
import { expectTranslated } from '../../test/i18nAudit'

it('users pages have no untranslated text', () => expectTranslated('src/designs/users'))
