import { CopyBlock } from '../../components/CopyBlock'
import { Banner, ClipboardText, Empty, LayerCard, LinkButton, Text } from '@cloudflare/kumo'
import { InfoIcon, KeyIcon, WarningIcon } from '@phosphor-icons/react'
import { useParams } from 'react-router'
import { PageHeader } from '../../components/PageHeader'
import { useDnssec } from '../../api/dns'
import { ApiError } from '../../api/client'
import { apiErrorText } from '../../api/errors'
import { useT } from '../../i18n/I18nProvider'
import { DnsError, DnsLoading } from './shared'

function Fact({ label, value }: { label: string; value: string }) {
  return (
    <div className="grid gap-1.5">
      <Text bold as="span">
        {label}
      </Text>
      <ClipboardText text={value} size="base" />
    </div>
  )
}

export function DnssecPage() {
  const { domain = '' } = useParams()
  const t = useT()
  const query = useDnssec(domain)
  const zone = query.data?.zone
  const key = query.data?.key
  const back = { href: `/dns/${domain}`, label: t('%s records', domain) }

  return (
    <div className="grid gap-6">
      <PageHeader
        title={t('View public DNSSEC key')}
        description={t('Publish these values at your registrar so resolvers can validate %s.', domain)}
        back={back}
      />

      {query.isPending ? (
        <DnsLoading />
      ) : query.isError || !zone || !key ? (
        query.error instanceof ApiError && query.error.status === 404 ? (
          <Banner
            variant="error"
            icon={<WarningIcon weight="fill" />}
            title={t("DNS domain %s doesn't exist", domain)}
            description={<span className="whitespace-pre-line">{apiErrorText(query.error, t)}</span>}
          />
        ) : (
          <DnsError error={query.error} />
        )
      ) : !key.DS || zone.DNSSEC !== 'yes' ? (
        <LayerCard className="p-0">
          <Empty
            icon={<KeyIcon size={40} className="text-kumo-inactive" />}
            title={t('DNSSEC is disabled for this zone')}
            description={t('Enable DNSSEC on the DNS domain to generate a signing key.')}
            contents={
              <LinkButton href={`/dns/${domain}/edit`} variant="primary">
                {t('Edit DNS domain')}
              </LinkButton>
            }
            className="py-10"
          />
        </LayerCard>
      ) : (
        <>
          <Banner
            icon={<InfoIcon weight="fill" />}
            title={t('Most registrars ask for the DS record')}
            description={t(
              'Some, mostly for ccTLDs, ask for the DNSKEY record or the public key with its flag and algorithm instead.',
            )}
          />
          <div className="grid gap-6 lg:grid-cols-[1fr_20rem]">
            <LayerCard className="grid h-fit gap-5 px-5 py-4">
              <CopyBlock label={t('DS record')} value={key.DS} />
              <CopyBlock label={t('DNSKEY record')} value={key.RECORD} />
              <CopyBlock label={t('Public key')} value={key.KEY} />
            </LayerCard>
            <LayerCard className="grid h-fit gap-4 px-5 py-4">
              <Fact label={t('Key type / flag')} value={query.data.flagLabel} />
              <Fact label={t('Key tag')} value={key.KEYTAG} />
              <Fact label={t('Flag')} value={key.FLAG} />
              <Fact label={t('Algorithm')} value={query.data.algorithmLabel} />
            </LayerCard>
          </div>
        </>
      )}
    </div>
  )
}
