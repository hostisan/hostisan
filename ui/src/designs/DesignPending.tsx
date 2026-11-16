import { Empty } from '@cloudflare/kumo'
import { PaintBrushIcon } from '@phosphor-icons/react'
import { useLocation } from 'react-router'
import { PageHeader } from '../components/PageHeader'
import { useT } from '../i18n/I18nProvider'
import { findNavLabel } from '../layout/nav'

export function DesignPending() {
  const t = useT()
  const { pathname } = useLocation()
  const { page } = findNavLabel(pathname)
  return (
    <div className="grid gap-6">
      <PageHeader title={page ? t(page) : t('Page not found')} />
      <Empty
        icon={<PaintBrushIcon size={40} className="text-kumo-inactive" />}
        title={t('Design pending')}
        description={t("This section hasn't been designed yet.")}
        className="py-16"
      />
    </div>
  )
}
