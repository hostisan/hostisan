import { ClipboardText, Table, Text } from '@cloudflare/kumo'
import { useT } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { mailHostname, mailWebmailUrl } from './shared'

const protocols = [
  { name: msgid('IMAP settings'), ports: { ssl: '993', starttls: '143', none: '143' } },
  { name: msgid('POP3 settings'), ports: { ssl: '995', starttls: '110', none: '110' } },
  { name: msgid('SMTP settings'), ports: { ssl: '465', starttls: '587', none: '25' } },
]

function Copyable({ label, value }: { label: string; value: string }) {
  return (
    <div className="grid gap-1.5">
      <Text bold as="span">
        {label}
      </Text>
      <ClipboardText text={value} size="base" />
    </div>
  )
}

/** Port of Hestia's includes/email-settings-panel.php. Lives inside a FormSection. */
export function EmailSettingsPanel({
  account,
  domain,
  password,
  webmailAlias,
}: {
  account: string
  domain: string
  password: string
  /** The system webmail alias; leave out when webmail is off. */
  webmailAlias?: string
}) {
  const t = useT()
  const username = `${account || 'account'}@${domain}`
  return (
    <div className="grid gap-5">
      <div className="grid gap-4">
        <Text>{t('Connect to this account using credentials:')}</Text>
        <div className="grid gap-4 sm:grid-cols-2">
          <Copyable label={t('Username')} value={username} />
          <Copyable label={t('Password')} value={password || '–'} />
          {webmailAlias !== undefined && <Copyable label={t('Webmail')} value={mailWebmailUrl(webmailAlias, domain)} />}
          <Copyable label={t('Hostname')} value={mailHostname(domain)} />
        </div>
      </div>
      <div className="-mx-5 overflow-x-auto border-t border-kumo-line">
        <Table className="whitespace-nowrap">
          <Table.Header>
            <Table.Row>
              <Table.Head className="pl-5">{t('Protocol')}</Table.Head>
              <Table.Head>{t('Authentication')}</Table.Head>
              <Table.Head>SSL/TLS</Table.Head>
              <Table.Head>STARTTLS</Table.Head>
              <Table.Head className="pr-5">{t('No encryption')}</Table.Head>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {protocols.map((p) => (
              <Table.Row key={p.name}>
                <Table.Cell className="pl-5">
                  <Text bold as="span">
                    {t(p.name)}
                  </Text>
                </Table.Cell>
                <Table.Cell>{t('Normal password')}</Table.Cell>
                <Table.Cell className="tabular-nums">{t('Port')} {p.ports.ssl}</Table.Cell>
                <Table.Cell className="tabular-nums">{t('Port')} {p.ports.starttls}</Table.Cell>
                <Table.Cell className="pr-5 tabular-nums">{t('Port')} {p.ports.none}</Table.Cell>
              </Table.Row>
            ))}
          </Table.Body>
        </Table>
      </div>
    </div>
  )
}
