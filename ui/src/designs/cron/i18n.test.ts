import { it } from 'vitest'
import { expectTranslated } from '../../test/i18nAudit'

it('cron pages have no untranslated text', () => expectTranslated('src/designs/cron'))
