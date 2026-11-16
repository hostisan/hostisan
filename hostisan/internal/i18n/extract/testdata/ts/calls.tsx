import { useT } from '../i18n/I18nProvider'
import { msgid } from '../i18n/msgid'

const re = /'/g
const half = 4 / 2
const LABELS = [msgid('d'), msgid("Shared")]

export function Page({ name, b, variable, item }: Props) {
  const t = useT()
  // t('In a comment')
  /* t('In a block comment') */
  const s = "t('In a string')"
  return (
    <div title={t("b")}>
      <p>Don't stop {t('a')}</p>
      {t(`c`)}
      {t('Say \'hi\' to "them"')}
      {t("Line one\nline two")}
      {t(
        'Multi-line call %s',
        name,
      )}
      {t(`Hello ${name}`)}
      {t('a' + b)}
      {format(t(variable))}
      {item.t('Method form')}
      {t('Unicode été')}
    </div>
  )
}
