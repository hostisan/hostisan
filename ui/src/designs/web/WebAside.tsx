import { Text } from '@cloudflare/kumo'
import { useWebDomains, useWebOptions } from '../../api/web'
import {
  AsideCard,
  AsideSection,
  DetailRow,
  PageAside,
  StatGrid,
  StatTile,
  UsageMeter,
} from '../../components/PageAside'
import { useT } from '../../i18n/I18nProvider'
import { formatMB } from '../../lib/format'

export function WebAside() {
  const t = useT()
  const data = useWebDomains().data
  const options = useWebOptions().data
  const domains = data?.domains ?? []
  const usage = data?.usage
  const disk = domains.reduce((n, d) => n + (Number(d.U_DISK) || 0), 0)
  const ips = [...new Set(domains.map((d) => data?.displayIps[d.IP] ?? d.IP))]
  const server = [options?.proxySystem, options?.webSystem].filter(Boolean).join(' + ')

  return (
    <PageAside>
      <AsideSection title={t('Usage')}>
        <div className="grid gap-3">
          <UsageMeter label={t('Web domains')} used={Number(usage?.domains ?? domains.length)} limit={usage?.domainsLimit ?? 'unlimited'} />
          <UsageMeter label={t('Aliases')} used={Number(usage?.aliases ?? 0)} limit={usage?.aliasesLimit ?? 'unlimited'} />
        </div>
      </AsideSection>
      <StatGrid>
        <StatTile
          label={t('SSL enabled')}
          value={domains.filter((d) => d.SSL === 'yes').length}
          hint={t('Domains serving HTTPS with any certificate.')}
        />
        <StatTile
          label="Let's Encrypt"
          value={domains.filter((d) => d.LETSENCRYPT === 'yes').length}
          hint={t('Certificates issued and renewed automatically.')}
        />
        <StatTile label={t('Disk used')} value={formatMB(String(disk))} hint={t('Total across all web domains.')} />
        <StatTile label={t('Suspended')} value={Number(usage?.suspended ?? 0)} />
      </StatGrid>
      {options && (
        <AsideCard title={t('Hosting details')}>
          <div>
            {server !== '' && (
              <DetailRow label={t('Web server')}>
                <Text as="span">{server.toUpperCase()}</Text>
              </DetailRow>
            )}
            {ips.length > 0 && (
              <DetailRow label={t('IP addresses')}>
                <Text variant="mono" as="span">
                  {ips.join(', ')}
                </Text>
              </DetailRow>
            )}
            <DetailRow label={t('Home')}>
              <Text variant="mono" as="span">
                {options.webRoot}
              </Text>
            </DetailRow>
          </div>
        </AsideCard>
      )}
    </PageAside>
  )
}
