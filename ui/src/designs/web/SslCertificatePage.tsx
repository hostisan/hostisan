import { ValuesList } from '../../components/ValuesList'
import { CopyBlock } from '../../components/CopyBlock'
import { Badge, Empty, LayerCard, LinkButton, Text } from '@cloudflare/kumo'
import { LockSimpleOpenIcon, PencilSimpleIcon, SealCheckIcon } from '@phosphor-icons/react'
import { PageHeader } from '../../components/PageHeader'
import { DnsError, DnsLoading } from '../dns/shared'
import { ApiError } from '../../api/client'
import { useT } from '../../i18n/I18nProvider'
import { MONTHS } from '../../lib/date'
import { DomainNotFound, SslBadge, useWebDomainParam } from './shared'

// openssl dates look like "Dec  3 01:00:01 2026 GMT"
function daysLeft(notAfter: string, now = new Date()) {
  const [mon, day, time, year] = notAfter.trim().split(/\s+/)
  const [h, m, s] = time.split(':').map(Number)
  const end = Date.UTC(Number(year), MONTHS.indexOf(mon as (typeof MONTHS)[number]), Number(day), h, m, s)
  return Math.round((end - now.getTime()) / 86_400_000)
}

export function SslCertificatePage() {
  const t = useT()
  const { name, query } = useWebDomainParam()
  if (query.error instanceof ApiError && query.error.status === 404) return <DomainNotFound name={name} />
  if (!query.data) return query.error ? <DnsError error={query.error} /> : <DnsLoading />
  const domain = query.data.domain
  const ssl = query.data.ssl
  const back = { href: `/web/${name}`, label: name }

  if (domain.SSL !== 'yes' || !ssl) {
    return (
      <div className="grid gap-6">
        <PageHeader title={t('SSL certificate')} back={back} />
        <LayerCard className="p-0">
          <Empty
            icon={<LockSimpleOpenIcon size={40} className="text-kumo-inactive" />}
            title={t('SSL is not enabled for this domain')}
            description={t("Turn on SSL in the domain settings to request a Let's Encrypt certificate or upload your own.")}
            contents={
              <LinkButton href={`/web/${name}`} variant="primary">
                {t('Enable SSL')}
              </LinkButton>
            }
            className="py-12"
          />
        </LayerCard>
      </div>
    )
  }

  const remaining = daysLeft(ssl.NOT_AFTER)

  return (
    <div className="grid gap-6 pb-4">
      <PageHeader
        title={t('SSL certificate')}
        description={<span className="font-mono text-[0.9em]">{name}</span>}
        back={back}
        actions={
          <LinkButton href={`/web/${name}`} variant="secondary" icon={PencilSimpleIcon}>
            {t('Edit SSL settings')}
          </LinkButton>
        }
      />

      <LayerCard className="grid gap-4 px-5 py-4">
        <div className="flex flex-wrap items-center gap-2">
          <SealCheckIcon size={20} className="text-kumo-success" />
          <Text variant="heading" as="h2">
            {ssl.SUBJECT}
          </Text>
          <SslBadge domain={domain} />
          {remaining < 0 ? (
            <Badge variant="red">{t('Expired')}</Badge>
          ) : remaining <= 30 ? (
            <Badge variant="orange">{t('Expires in %s days', remaining)}</Badge>
          ) : (
            <Badge variant="neutral">{t('Valid for %s more days', remaining)}</Badge>
          )}
        </div>
        <ValuesList
          items={[
            { label: t('Issued to'), value: ssl.SUBJECT },
            { label: t('Alternate'), value: ssl.ALIASES.split(',').join(', ') },
            { label: t('Not before'), value: <span className="tabular-nums">{ssl.NOT_BEFORE}</span> },
            { label: t('Not after'), value: <span className="tabular-nums">{ssl.NOT_AFTER}</span> },
            { label: t('Signature'), value: <span className="font-mono text-[0.9em]">{ssl.SIGNATURE}</span> },
            { label: t('Key size'), value: ssl.PUB_KEY },
            { label: t('Issued by'), value: ssl.ISSUER },
          ]}
        />
        {domain.LETSENCRYPT === 'yes' && (
          <Text variant="secondary">
            {t("Let's Encrypt renews this certificate automatically 30 days before it expires.")}
          </Text>
        )}
      </LayerCard>

      <div className="grid gap-3">
        <Text variant="heading" as="h2">
          {t('Certificate files')}
        </Text>
        <LayerCard className="grid gap-6 px-5 py-4">
          <CopyBlock layout="scroll" label={t('SSL certificate')} filename={`${name}.crt`} value={ssl.CRT} />
          <CopyBlock layout="scroll" label={t('SSL private key')} filename={`${name}.key`} value={ssl.KEY} />
          {ssl.CA && (
            <CopyBlock
              layout="scroll"
              label={t('SSL certificate authority / intermediate')}
              filename={`${name}.ca`}
              value={ssl.CA}
            />
          )}
        </LayerCard>
      </div>
    </div>
  )
}
