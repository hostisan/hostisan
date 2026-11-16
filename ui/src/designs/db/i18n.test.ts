import { it } from 'vitest'
import { expectTranslated } from '../../test/i18nAudit'

it('db pages have no untranslated text', () => expectTranslated('src/designs/db'))
