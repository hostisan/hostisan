import { Link, Text } from '@cloudflare/kumo'
import { useBackups } from '../../api/backup'
import { AsideCard, AsideSection, PageAside, StatGrid, StatTile, UsageMeter } from '../../components/PageAside'
import { useT } from '../../i18n/I18nProvider'
import { translateDate } from '../../lib/date'
import { formatBackupSize } from './BackupTabs'

export function BackupAside() {
  const t = useT()
  const data = useBackups().data
  if (!data) return null
  const latest = data.backups[0]
  const size = data.backups.reduce((n, b) => n + Number(b.SIZE), 0)

  return (
    <PageAside>
      <AsideSection title={t('Usage')}>
        <UsageMeter label={t('Backups kept')} used={Number(data.usage.backups) || 0} limit={data.usage.limit} />
      </AsideSection>
      <StatGrid>
        <StatTile
          label={t('Last backup')}
          value={latest ? translateDate(latest.DATE, t) : '-'}
          hint={t('The newest backup of this account.')}
        />
        <StatTile label={t('Total size')} value={formatBackupSize(String(size))} />
      </StatGrid>
      <AsideCard title={t('Exclusions')} description={t('Folders, databases and mailboxes left out of every backup.')}>
        <Link href="/backup/exclusions">{t('Manage exclusions')}</Link>
      </AsideCard>
      <Text variant="secondary">{t('Finished backups are announced by email and in your notifications.')}</Text>
    </PageAside>
  )
}
