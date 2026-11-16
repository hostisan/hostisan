import { it } from 'vitest'
import { expectTranslated } from '../../test/i18nAudit'

it('web pages have no untranslated text', () => expectTranslated('src/designs/web'))
