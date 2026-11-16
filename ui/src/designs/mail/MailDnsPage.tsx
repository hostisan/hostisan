import { Banner, InlineCopyText, LayerCard, LinkButton, Table, Text } from '@cloudflare/kumo'
import { CheckCircleIcon, InfoIcon } from '@phosphor-icons/react'
import { useParams } from 'react-router'
import { PageHeader } from '../../components/PageHeader'
import { useDnsZones } from '../../api/dns'
import { useT } from '../../i18n/I18nProvider'
import { useMailAccounts, useMailDns } from '../../api/mail'
import { useSessionUser } from '../../session/SessionProvider'
import { DnsError, DnsLoading, RecordTypeBadge } from '../dns/shared'

export function MailDnsPage() {
  const t = useT()
  const { domain = '' } = useParams()
  const dns = useMailDns(domain)
  // The accounts list carries the domain without its SSL key, unlike the domain detail
  const detail = useMailAccounts(domain).data?.domain
  const dnsEnabled = useSessionUser()?.features.dns ?? false
  const zones = useDnsZones({ enabled: dnsEnabled })
  const back = { href: `/mail/${domain}`, label: t('%s accounts', domain) }

  if (dns.isPending || dns.isError) {
    return (
      <div className="grid gap-6">
        <PageHeader title={t('DNS records')} back={{ href: '/mail', label: t('Mail') }} />
        {dns.isError ? <DnsError error={dns.error} /> : <DnsLoading />}
      </div>
    )
  }

  const rows = dns.data.records
  const hostedHere = (zones.data?.zones ?? []).some((z) => z.NAME === domain)

  return (
    <div className="grid gap-6">
      <PageHeader
        title={t('DNS records')}
        description={t(
          'Add these records at the DNS provider for %s so mail is delivered and passes SPF, DKIM and DMARC checks.',
          domain,
        )}
        back={back}
      />

      {hostedHere ? (
        <Banner
          icon={<CheckCircleIcon weight="fill" />}
          title={t("%s uses this server's DNS", domain)}
          description={t(
            "These records were added to the zone automatically. You only need them if the domain's DNS is hosted elsewhere.",
          )}
          action={
            <LinkButton href={`/dns/${domain}`} variant="secondary">
              {t('View DNS zone')}
            </LinkButton>
          }
        />
      ) : (
        <Banner
          variant="secondary"
          icon={<InfoIcon weight="fill" />}
          title={t('DNS is hosted elsewhere')}
          description={t(
            'Create each record below at your registrar or DNS provider. Changes can take a few hours to propagate.',
          )}
        />
      )}

      <LayerCard className="overflow-x-auto p-0">
        <Table className="whitespace-nowrap">
          <Table.Header>
            <Table.Row>
              <Table.Head>{t('Record')}</Table.Head>
              <Table.Head>{t('Type')}</Table.Head>
              <Table.Head className="text-right">{t('Priority')}</Table.Head>
              <Table.Head className="text-right">TTL</Table.Head>
              <Table.Head>{t('IP or value')}</Table.Head>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {rows.map((r, i) => (
              <Table.Row key={i}>
                <Table.Cell>
                  <InlineCopyText variant="body" bold labels={{ copyAction: t('Copy %s', r.record), copied: t('Copied') }}>
                    {r.record}
                  </InlineCopyText>
                </Table.Cell>
                <Table.Cell>
                  <RecordTypeBadge type={r.type} />
                </Table.Cell>
                <Table.Cell className="text-right tabular-nums">
                  {r.priority || <span className="text-kumo-subtle">–</span>}
                </Table.Cell>
                <Table.Cell className="text-right tabular-nums">{r.ttl}</Table.Cell>
                <Table.Cell>
                  <InlineCopyText
                    variant="mono"
                    value={r.value}
                    labels={{ copyAction: t('Copy %s value', r.type), copied: t('Copied') }}
                  >
                    <span className="block max-w-[28rem] truncate" title={r.value}>
                      {r.value}
                    </span>
                  </InlineCopyText>
                </Table.Cell>
              </Table.Row>
            ))}
          </Table.Body>
        </Table>
      </LayerCard>

      {detail && detail.DKIM !== 'yes' && (
        <Text variant="secondary" size="sm">
          {t(
            'DKIM is disabled for this domain, so no DKIM record is listed. Enable DKIM support on the mail domain to sign outgoing mail.',
          )}
        </Text>
      )}
    </div>
  )
}
