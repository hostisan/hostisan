import { LinkButton } from '@cloudflare/kumo'
import { ArrowSquareOutIcon } from '@phosphor-icons/react'
import { AsideCard, AsideSection, PageAside, StatGrid, StatTile, UsageMeter } from '../../components/PageAside'
import { useDatabases, useDbOptions } from '../../api/db'
import { formatMB } from '../../lib/format'
import { useT } from '../../i18n/I18nProvider'
import { useSessionUser } from '../../session/SessionProvider'

export function DbAside() {
  const t = useT()
  const readOnly = useSessionUser()?.readOnly ?? false
  const query = useDatabases().data
  const options = useDbOptions().data
  const dbs = query?.databases ?? []
  const usage = query?.usage
  const disk = dbs.reduce((n, d) => n + Number(d.U_DISK), 0)
  const myAdmin = options?.phpMyAdminUrl ?? ''
  const pgAdmin = options?.phpPgAdminUrl ?? ''

  return (
    <PageAside>
      <AsideSection title={t('Usage')}>
        <UsageMeter
          label={
            Number(usage?.suspended) > 0 ? t('Databases (%s suspended)', usage?.suspended ?? '') : t('Databases')
          }
          used={Number(usage?.databases ?? dbs.length)}
          limit={usage?.databasesLimit ?? 'unlimited'}
        />
      </AsideSection>
      <StatGrid>
        <StatTile label="MySQL" value={dbs.filter((d) => d.TYPE === 'mysql').length} />
        <StatTile label="PostgreSQL" value={dbs.filter((d) => d.TYPE === 'pgsql').length} />
        <StatTile label={t('Disk used')} value={formatMB(String(disk))} />
        <StatTile label={t('Suspended')} value={dbs.filter((d) => d.SUSPENDED === 'yes').length} />
      </StatGrid>
      {!readOnly && (myAdmin !== '' || pgAdmin !== '') && (
        <AsideCard title={t('Database tools')} description={t('Browse tables and run queries.')}>
          <div className="flex flex-wrap gap-2">
            {myAdmin !== '' && (
              <LinkButton href={myAdmin} target="_blank" icon={ArrowSquareOutIcon}>
                phpMyAdmin
              </LinkButton>
            )}
            {pgAdmin !== '' && (
              <LinkButton href={pgAdmin} target="_blank" icon={ArrowSquareOutIcon}>
                phpPgAdmin
              </LinkButton>
            )}
          </div>
        </AsideCard>
      )}
    </PageAside>
  )
}
