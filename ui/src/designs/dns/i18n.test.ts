import { it } from 'vitest'
import { expectTranslated } from '../../test/i18nAudit'

it('dns pages have no untranslated text', () => expectTranslated('src/designs/dns'))
