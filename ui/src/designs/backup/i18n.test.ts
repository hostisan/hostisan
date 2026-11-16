import { it } from 'vitest'
import { expectTranslated } from '../../test/i18nAudit'

it('backup pages have no untranslated text', () => expectTranslated('src/designs/backup'))
