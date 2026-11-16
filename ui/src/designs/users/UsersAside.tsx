import { AsideSection, PageAside, StatGrid, StatTile } from '../../components/PageAside'
import { usePackages, useUsers } from '../../api/users'
import { useT } from '../../i18n/I18nProvider'

export function UsersAside() {
  const t = useT()
  const users = useUsers().data?.users
  const packages = usePackages().data?.packages
  if (!users) return null

  return (
    <PageAside>
      <AsideSection title={t('Overview')}>
        <StatGrid>
          <StatTile label={t('Users')} value={users.length} />
          <StatTile label={t('Suspended')} value={users.filter((u) => u.SUSPENDED === 'yes').length} />
          <StatTile label={t('Administrators')} value={users.filter((u) => u.ROLE === 'admin').length} />
          {packages && <StatTile label={t('Packages')} value={packages.length} />}
        </StatGrid>
      </AsideSection>
    </PageAside>
  )
}
