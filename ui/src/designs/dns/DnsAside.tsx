import { InlineCopyText } from '@cloudflare/kumo'
import {
  AsideCard,
  AsideSection,
  DetailRow,
  PageAside,
  StatGrid,
  StatTile,
  UsageMeter,
} from '../../components/PageAside'
import { useDnsOptions, useDnsZones } from '../../api/dns'
import { useT } from '../../i18n/I18nProvider'

export function DnsAside() {
  const t = useT()
  const query = useDnsZones().data
  const zones = query?.zones ?? []
  const usage = query?.usage
  const nameservers = useDnsOptions().data?.nameservers ?? []
  const records = zones.reduce((n, z) => n + Number(z.RECORDS), 0)

  return (
    <PageAside>
      <AsideSection title={t('Usage')}>
        <div className="grid gap-3">
          <UsageMeter
            label={
              Number(usage?.suspended) > 0 ? t('DNS zones (%s suspended)', usage?.suspended ?? '') : t('DNS zones')
            }
            used={Number(usage?.zones ?? zones.length)}
            limit={usage?.zonesLimit ?? 'unlimited'}
          />
          <UsageMeter
            label={t('DNS records')}
            used={Number(usage?.records ?? records)}
            limit={usage?.recordsLimit ?? 'unlimited'}
          />
        </div>
      </AsideSection>
      <StatGrid>
        <StatTile
          label={t('DNSSEC signed')}
          value={zones.filter((z) => z.DNSSEC === 'yes').length}
          hint={t('Zones signed with DNSSEC. Publish the DS record at your registrar.')}
        />
        <StatTile
          label={t('From cluster')}
          value={zones.filter((z) => z.SRC).length}
          hint={t('Zones synced from another server in the DNS cluster.')}
        />
      </StatGrid>
      <AsideCard
        title={t('Name servers')}
        description={t('Point your domains at these name servers at your registrar.')}
      >
        <div>
          {nameservers.map((ns, i) => (
            <DetailRow key={ns} label={`NS ${i + 1}`}>
              <InlineCopyText value={ns}>{ns}</InlineCopyText>
            </DetailRow>
          ))}
        </div>
      </AsideCard>
    </PageAside>
  )
}
