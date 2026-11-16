import {
  Banner,
  Checkbox,
  Input,
  InputArea,
  InputGroup,
  LinkButton,
  Button,
  Switch,
  Text,
  useKumoToastManager,
} from '@cloudflare/kumo'
import { InfoIcon, WarningIcon } from '@phosphor-icons/react'
import { useState, type FormEvent, type ReactNode } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { apiErrorText } from '../../api/errors'
import type { AddMailDomainRequest, EditMailDomainRequest, MailOptions, MailSSLRequest } from '../../api/types.gen'
import type { MailDomainDetail, MailSSL } from '../../api/hestia.gen'
import { useMailDomain, useMailMutation, useMailOptions } from '../../api/mail'
import { Optional, SelectField } from '../../components/fields'
import { FormPage } from '../../components/FormPage'
import { FormSection } from '../../components/FormSection'
import { PageHeader } from '../../components/PageHeader'
import { PasswordInput } from '../../components/PasswordInput'
import { StatusBadge } from '../../components/StatusBadge'
import { useT } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { useSessionUser } from '../../session/SessionProvider'
import { interpolate } from '../../lib/interpolate'
import { DnsError, DnsLoading } from '../dns/shared'

const back = { href: '/mail', label: msgid('Mail') }

const webmailLabels: Record<string, string> = { roundcube: 'Roundcube', snappymail: 'SnappyMail' }
const keyAlgoLabels: Record<string, string> = {
  'ecdsa-256': msgid('ECDSA P-256 (recommended)'),
  'ecdsa-384': 'ECDSA P-384',
  rsa: 'RSA 4096',
}
const DISABLED = 'disabled'

/** Parity 8: trimmed, a leading "www." removed, lowercased (the server does the same). */
const normaliseDomain = (name: string) =>
  name
    .trim()
    .replace(/^www\./i, '')
    .toLowerCase()

function ErrorBanner({ text }: { text: string }) {
  if (!text) return null
  return <Banner variant="error" icon={<WarningIcon weight="fill" />} title={text} className="whitespace-pre-line" />
}

/** The create-success message: its {...} links to the new domain's accounts. */
function createdMessage(t: (s: string) => string, domain: string): ReactNode {
  const parts = t('Mail domain {%s} has been created successfully.').split(/(\{[^}]*\})/)
  return (
    <>
      {parts.map((part, i) => {
        const inner = /^\{([^}]*)\}$/.exec(part)?.[1]
        if (inner === undefined) return part
        return (
          <Link key={i} to={`/mail/${encodeURIComponent(domain)}`} className="underline">
            {inner.replace('%s', () => domain)}
          </Link>
        )
      })}
    </>
  )
}

function CertDetails({ ssl }: { ssl: MailSSL }) {
  const t = useT()
  const rows: [string, string][] = [
    [t('Issued to'), ssl.SUBJECT],
    // Hestia separates the alternate names with commas
    [t('Alternate'), ssl.ALIASES.split(',').filter(Boolean).join(', ')],
    [t('Not before'), ssl.NOT_BEFORE],
    [t('Not after'), ssl.NOT_AFTER],
    [t('Signature'), ssl.SIGNATURE],
    [t('Key size'), ssl.PUB_KEY],
    [t('Issued by'), ssl.ISSUER],
  ]
  return (
    <dl className="grid gap-x-6 gap-y-2 rounded-lg bg-kumo-recessed px-4 py-3 sm:grid-cols-[9rem_1fr]">
      {rows
        .filter(([, v]) => v !== '')
        .map(([k, v]) => (
          <div key={k} className="contents">
            <dt className="text-kumo-subtle">{k}</dt>
            <dd className="break-words text-kumo-default">{v}</dd>
          </div>
        ))}
    </dl>
  )
}

/** Parity 2.3: a stored key type outside the offered set falls back to rsa when SSL is on. */
function initialKeyAlgo(d: MailDomainDetail | undefined, offered: string[]) {
  const stored = (d?.SSL_KEY_ALGO ?? '').toLowerCase()
  if (stored === 'ecdsa') return 'ecdsa-256'
  if (offered.includes(stored)) return stored
  return d?.SSL === 'yes' ? 'rsa' : 'ecdsa-256'
}

export function MailDomainFormPage({ mode }: { mode: 'add' | 'edit' }) {
  return mode === 'add' ? <AddMailDomainPage /> : <EditMailDomainPage />
}

function AddMailDomainPage() {
  const t = useT()
  const options = useMailOptions()
  if (options.error || !options.data) {
    return (
      <FormPage title={t('Add mail domain')} back={{ ...back, label: t(back.label) }} readOnly>
        {options.error ? <DnsError error={options.error} /> : <DnsLoading />}
      </FormPage>
    )
  }
  return <MailDomainForm mode="add" options={options.data} />
}

function EditMailDomainPage() {
  const t = useT()
  const { domain = '' } = useParams()
  const options = useMailOptions()
  const view = useMailDomain(domain)
  const failed = options.error ?? view.error
  if (failed || !options.data || !view.data) {
    return (
      <FormPage title={t('Edit mail domain')} back={{ ...back, label: t(back.label) }} readOnly>
        {failed ? <DnsError error={failed} /> : <DnsLoading />}
      </FormPage>
    )
  }
  return <MailDomainForm mode="edit" options={options.data} detail={view.data.domain} ssl={view.data.ssl} />
}

type FormProps = { options: MailOptions } & (
  { mode: 'add'; detail?: undefined; ssl?: undefined } | { mode: 'edit'; detail: MailDomainDetail; ssl?: MailSSL }
)

function MailDomainForm({ mode, options, detail: d, ssl }: FormProps) {
  const t = useT()
  const user = useSessionUser()
  const m = useMailMutation()
  const toast = useKumoToastManager()
  const navigate = useNavigate()
  const edit = mode === 'edit'
  const clients = options.webmailClients
  // Webmail needs both an IMAP and a webmail system, as the server checks
  const showWebmail = options.imap && options.webmail && clients.length > 0
  const storedWebmail = d?.WEBMAIL ?? ''

  const [continued, setContinued] = useState(false)
  const [name, setName] = useState(d?.NAME ?? '')
  const [webmail, setWebmail] = useState(
    edit
      ? storedWebmail === '' || storedWebmail === DISABLED
        ? DISABLED
        : storedWebmail
      : clients.includes('roundcube')
        ? 'roundcube'
        : (clients[0] ?? DISABLED),
  )
  const [catchall, setCatchall] = useState(d?.CATCHALL ?? '')
  const [rate, setRate] = useState(d?.RATE_LIMIT ?? '')
  const [antispam, setAntispam] = useState(d ? d.ANTISPAM === 'yes' : true)
  const [reject, setReject] = useState(d ? d.REJECT === 'yes' : true)
  const [antivirus, setAntivirus] = useState(d ? d.ANTIVIRUS === 'yes' : true)
  const [dkim, setDkim] = useState(d ? d.DKIM === 'yes' : true)
  const [selector, setSelector] = useState(d?.DKIM_SELECTOR || 'mail')
  const initial = {
    ssl: d?.SSL === 'yes',
    le: d ? d.LETSENCRYPT === 'yes' : true,
    keyAlgo: initialKeyAlgo(d, options.keyAlgos),
    crt: ssl?.CRT ?? '',
    key: ssl?.KEY ?? '',
    ca: ssl?.CA ?? '',
  }
  const [sslOn, setSslOn] = useState(initial.ssl)
  const [le, setLe] = useState(initial.le)
  const [keyAlgo, setKeyAlgo] = useState(initial.keyAlgo)
  const [crt, setCrt] = useState(initial.crt)
  const [key, setKey] = useState(initial.key)
  const [ca, setCa] = useState(initial.ca)
  const relayStored = d?.U_SMTP_RELAY === 'true'
  const [relay, setRelay] = useState(relayStored)
  const [relayHost, setRelayHost] = useState(d?.U_SMTP_RELAY_HOST ?? '')
  const [relayPort, setRelayPort] = useState(d?.U_SMTP_RELAY_PORT || '587')
  const [relayUser, setRelayUser] = useState(d?.U_SMTP_RELAY_USERNAME ?? '')
  const [relayPass, setRelayPass] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const backLink = { ...back, label: t(back.label) }

  const shown = normaliseDomain(name) || 'acme-bakery.com'
  const suspended = d?.SUSPENDED === 'yes'
  const readOnly = user?.readOnly ?? false
  const webmailHost = `${options.webmailAlias || 'webmail'}.${edit ? d.NAME : shown}`
  const relayBody = { host: relayHost, port: relayPort, user: relayUser, password: relayPass }

  const sslChanged =
    sslOn !== initial.ssl ||
    (sslOn &&
      (le !== initial.le ||
        (le ? keyAlgo !== initial.keyAlgo : crt !== initial.crt || key !== initial.key || ca !== initial.ca)))
  // The fields still hold the Let's Encrypt certificate after Let's Encrypt is turned off
  const keepsLeCert =
    initial.ssl && initial.le && sslOn && !le && initial.crt !== '' && crt === initial.crt && key === initial.key
  const sslBody: MailSSLRequest = {
    ssl: sslOn,
    letsencrypt: sslOn && le,
    keyAlgo,
    crt: sslOn && !le ? crt : '',
    key: sslOn && !le ? key : '',
    ca: sslOn && !le ? ca : '',
  }

  const onSubmit = async (e: FormEvent) => {
    void e
    setError('')
    setBusy(true)
    try {
      if (!edit) {
        // Fields the system doesn't offer are left out of the request
        const body: AddMailDomainRequest = {
          domain: name,
          webmail: showWebmail && webmail !== DISABLED ? webmail : '',
          ...(options.antispam ? { antispam, reject } : {}),
          ...(options.antivirus ? { antivirus } : {}),
          dkim,
          dkimSelector: selector,
          ...(relay ? { smtpRelay: relayBody } : {}),
        }
        await m.addDomain(body)
        toast.add({ title: createdMessage(t, normaliseDomain(name)), type: 'success' })
      } else {
        const body: EditMailDomainRequest = {
          // "disabled" keeps Hestia's own value when that is what is stored; a client that was
          // never set stays removed
          ...(showWebmail
            ? { webmail: webmail === DISABLED ? (storedWebmail === DISABLED ? DISABLED : '') : webmail }
            : {}),
          catchall,
          ...(options.canSetRate ? { rate } : {}),
          ...(options.antispam ? { antispam, reject } : {}),
          ...(options.antivirus ? { antivirus } : {}),
          dkim,
          dkimSelector: selector,
          smtpRelay: relay ? relayBody : null,
        }
        await m.editDomain(d.NAME, body)
        // SSL is its own endpoint; it runs only when the section changed, after the settings
        if (sslChanged) await m.saveSsl(d.NAME, sslBody)
        toast.add({ title: t('Changes have been saved.'), type: 'success' })
      }
      navigate('/mail')
    } catch (err) {
      setError(apiErrorText(err, t))
      setBusy(false)
    }
  }

  if (!edit && options.adminGate && !continued) {
    return (
      <div className="grid gap-8">
        <PageHeader
          title={t('Add mail domain')}
          description={t('Receive and send email for a domain hosted on this server.')}
          back={backLink}
        />
        <Banner
          variant="alert"
          icon={<WarningIcon weight="fill" />}
          title={t("You're signed in as admin")}
          description={t(
            'It is strongly advised to {create a standard user account} before adding %s to the server due to the increased privileges the admin account possesses and potential security risks.',
            t('a mail domain'),
          ).replace(/[{}]/g, '')}
          action={
            <span className="inline-flex gap-2">
              <LinkButton href="/users/new">{t('Add user')}</LinkButton>
              <Button variant="secondary" onClick={() => setContinued(true)}>
                {t('Continue')}
              </Button>
            </span>
          }
        />
      </div>
    )
  }

  return (
    <FormPage
      title={edit ? t('Edit mail domain') : t('Add mail domain')}
      description={
        edit ? (
          <span className="inline-flex flex-wrap items-center gap-2">
            {d.NAME} <StatusBadge suspended={suspended} />
          </span>
        ) : (
          t('Receive and send email for a domain hosted on this server.')
        )
      }
      back={backLink}
      submitLabel={edit ? t('Save') : t('Add mail domain')}
      onSubmit={onSubmit}
      submitDisabled={busy}
      readOnly={readOnly}
      actions={
        edit && (
          <>
            <LinkButton href={`/mail/${d.NAME}/dns`} variant="secondary">
              {t('DNS records')}
            </LinkButton>
            <LinkButton href={`/mail/${d.NAME}`} variant="secondary">
              {t('Mail accounts')}
            </LinkButton>
          </>
        )
      }
    >
      {suspended && (
        <Banner
          variant="alert"
          icon={<WarningIcon weight="fill" />}
          title={t('This mail domain is suspended')}
          description={t("Incoming mail is rejected and accounts can't sign in until it is unsuspended.")}
        />
      )}
      <ErrorBanner text={error} />

      <FormSection
        title={t('Domain')}
        description={t('The domain name and the webmail client served at its webmail alias.')}
      >
        <Input
          label={t('Domain')}
          required
          disabled={edit}
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="acme-bakery.com"
        />
        {showWebmail && (
          <SelectField
            label={t('Webmail client')}
            description={
              webmail !== DISABLED && (
                <>{interpolate(t('Served at %s'), <span className="font-mono text-[0.9em]">{webmailHost}</span>)}</>
              )
            }
            value={webmail}
            onChange={setWebmail}
            options={[
              ...clients.map((c) => ({ value: c, label: webmailLabels[c] ?? c })),
              { value: DISABLED, label: t('Disabled') },
            ]}
          />
        )}
      </FormSection>

      {edit && (
        <FormSection
          title={t('Delivery')}
          description={t('Where unmatched mail goes and how much each account may send.')}
        >
          <Input
            label={
              <>
                {t('Catch-all email')} <Optional />
              </>
            }
            description={t("Receives mail sent to addresses that don't exist on this domain.")}
            type="email"
            value={catchall}
            onChange={(e) => setCatchall(e.target.value)}
            placeholder={`hello@${d.NAME}`}
          />
          <InputGroup label={t('Rate limit')} disabled={!options.canSetRate}>
            <InputGroup.Input
              aria-label={t('Rate limit')}
              inputMode="numeric"
              placeholder={t('System default')}
              value={rate}
              onChange={(e) => setRate(e.target.value)}
            />
            <InputGroup.Addon align="end">{t('email / hour / account')}</InputGroup.Addon>
          </InputGroup>
        </FormSection>
      )}

      <FormSection title={t('Protection')} description={t('Filtering on incoming mail and signing on outgoing mail.')}>
        {options.antispam && (
          <div className="grid gap-2">
            <Switch label={t('Spam filter')} checked={antispam} onCheckedChange={setAntispam} />
            {antispam && (
              <div className="pl-11">
                <Checkbox label={t('Reject spam')} checked={reject} onCheckedChange={(v) => setReject(Boolean(v))} />
                <Text variant="secondary" size="sm">
                  {t('Refuse spam at SMTP time instead of delivering it to the spam folder.')}
                </Text>
              </div>
            )}
          </div>
        )}
        {options.antivirus && <Switch label={t('Anti-virus')} checked={antivirus} onCheckedChange={setAntivirus} />}
        <div className="grid gap-2">
          <Switch label={t('DKIM support')} checked={dkim} onCheckedChange={setDkim} />
          {dkim && (
            <div className="pl-11">
              <Input
                label={t('DKIM selector')}
                description={t('Use a-z, 0-9, hyphen; no leading, trailing or consecutive hyphens')}
                value={selector}
                onChange={(e) => setSelector(e.target.value)}
                pattern="[a-z0-9]+(-[a-z0-9]+)*"
              />
            </div>
          )}
        </div>
      </FormSection>

      {edit && (
        <FormSection
          title="SSL"
          description={t('Certificate used for IMAP, POP3 and SMTP on %s.', `mail.${d.NAME}`)}
          aside={<Switch label={t('Enable SSL for this domain')} checked={sslOn} onCheckedChange={setSslOn} />}
        >
          {!sslOn ? (
            <Text variant="secondary">{t('Mail clients connect to this domain without a dedicated certificate.')}</Text>
          ) : (
            <>
              <Checkbox
                label={t("Use Let's Encrypt to obtain SSL certificate")}
                checked={le}
                onCheckedChange={(v) => setLe(Boolean(v))}
              />
              {le ? (
                <>
                  <SelectField
                    label={t('Key type')}
                    value={keyAlgo}
                    onChange={setKeyAlgo}
                    options={options.keyAlgos.map((a) => ({ value: a, label: t(keyAlgoLabels[a] ?? a) }))}
                  />
                  <Banner
                    icon={<InfoIcon weight="fill" />}
                    title={t('DNS records required')}
                    description={t(
                      "To enable Let's Encrypt SSL, ensure that DNS records exist for mail.%s and %s!",
                      d.NAME,
                      webmailHost,
                    )}
                  />
                </>
              ) : (
                <>
                  {keepsLeCert && (
                    <Text variant="secondary">
                      {t("This is the current Let's Encrypt certificate. It will no longer renew automatically.")}
                    </Text>
                  )}
                  <InputArea
                    label={t('SSL certificate')}
                    description={
                      <Link to={`/web/${d.NAME}/ssl/generate?back=/mail/${d.NAME}/edit`} className="underline">
                        {t('Generate self-signed SSL certificate')}
                      </Link>
                    }
                    required
                    value={crt}
                    onValueChange={setCrt}
                    autoResize
                    minRows={4}
                    maxRows={10}
                    className="font-mono text-[13px]"
                    placeholder="-----BEGIN CERTIFICATE-----"
                  />
                  <InputArea
                    label={t('SSL private key')}
                    required
                    value={key}
                    onValueChange={setKey}
                    autoResize
                    minRows={4}
                    maxRows={10}
                    className="font-mono text-[13px]"
                    placeholder="-----BEGIN PRIVATE KEY-----"
                  />
                  <InputArea
                    label={
                      <>
                        {t('SSL certificate authority / intermediate')} <Optional />
                      </>
                    }
                    value={ca}
                    onValueChange={setCa}
                    autoResize
                    minRows={4}
                    maxRows={10}
                    className="font-mono text-[13px]"
                  />
                </>
              )}
              {ssl && <CertDetails ssl={ssl} />}
            </>
          )}
        </FormSection>
      )}

      <FormSection
        title={t('SMTP relay')}
        description={t('Send outgoing mail for this domain through an external SMTP server.')}
        aside={<Switch label={t('SMTP relay')} checked={relay} onCheckedChange={setRelay} />}
      >
        {relay ? (
          <>
            <div className="grid gap-4 sm:grid-cols-[1fr_8rem]">
              <Input
                label={t('Host')}
                required
                value={relayHost}
                onChange={(e) => setRelayHost(e.target.value)}
                placeholder="smtp.postmarkapp.com"
              />
              <Input
                label={t('Port')}
                inputMode="numeric"
                value={relayPort}
                onChange={(e) => setRelayPort(e.target.value)}
              />
            </div>
            <Input
              label={t('Username')}
              value={relayUser}
              onChange={(e) => setRelayUser(e.target.value)}
              autoComplete="off"
            />
            <PasswordInput
              label={t('Password')}
              value={relayPass}
              onChange={setRelayPass}
              isNew={false}
              autoComplete="off"
              description={
                relayStored
                  ? t("The stored password can't be kept. Type it again whenever you change the host, port or username.")
                  : undefined
              }
            />
          </>
        ) : (
          <Text variant="secondary">{t('Outgoing mail is delivered directly from this server.')}</Text>
        )}
      </FormSection>
    </FormPage>
  )
}
