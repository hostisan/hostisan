import { InlineCopyText, Text } from '@cloudflare/kumo'
import { useMailDomains } from '../../api/mail'
import { useT } from '../../i18n/I18nProvider'
import {
  AsideCard,
  AsideSection,
  DetailRow,
  PageAside,
  StatGrid,
  StatTile,
  UsageMeter,
} from '../../components/PageAside'
import { mailHostname } from './shared'

export function MailAside() {
  const t = useT()
  const query = useMailDomains().data
  const domains = query?.domains ?? []
  const usage = query?.usage
  const suspended = Number(usage?.suspended)
  // Mail clients connect to mail.<domain>; with several domains the first one is the example
  const host = domains.length > 0 ? mailHostname(domains[0].NAME) : ''

  return (
    <PageAside>
      <AsideSection title={t('Usage')}>
        <div className="grid gap-3">
          <UsageMeter
            label={suspended > 0 ? t('Mail domains (%s suspended)', suspended) : t('Mail domains')}
            used={Number(usage?.domains ?? domains.length)}
            limit={usage?.domainsLimit ?? 'unlimited'}
          />
          <UsageMeter
            label={t('Mail accounts')}
            used={Number(usage?.accounts ?? 0)}
            limit={usage?.accountsLimit ?? 'unlimited'}
          />
        </div>
      </AsideSection>
      <StatGrid>
        <StatTile
          label={t('DKIM signed')}
          value={domains.filter((d) => d.DKIM === 'yes').length}
          hint={t('Outgoing mail is signed, which helps it reach the inbox.')}
        />
        <StatTile
          label={t('Spam filter')}
          value={domains.filter((d) => d.ANTISPAM === 'yes').length}
          hint={t('Domains with SpamAssassin filtering incoming mail.')}
        />
      </StatGrid>
      {host !== '' && (
        <AsideCard title={t('Mail client settings')} description={t('Use these in Outlook, Apple Mail or a phone.')}>
          <div>
            <DetailRow label={t('Server')}>
              <InlineCopyText value={host}>{host}</InlineCopyText>
            </DetailRow>
            <DetailRow label="IMAP">
              <Text as="span">993 SSL/TLS</Text>
            </DetailRow>
            <DetailRow label="POP3">
              <Text as="span">995 SSL/TLS</Text>
            </DetailRow>
            <DetailRow label="SMTP">
              <Text as="span">465 SSL/TLS, 587 STARTTLS</Text>
            </DetailRow>
          </div>
        </AsideCard>
      )}
    </PageAside>
  )
}
