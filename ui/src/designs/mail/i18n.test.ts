import { it } from 'vitest'
import { expectTranslated } from '../../test/i18nAudit'

it('mail pages have no untranslated text', () => expectTranslated('src/designs/mail'))
