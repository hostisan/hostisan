import {
  Banner,
  Checkbox,
  Collapsible,
  Input,
  InputArea,
  InputGroup,
  Switch,
  Text,
  useKumoToastManager,
} from '@cloudflare/kumo'
import { WarningIcon } from '@phosphor-icons/react'
import { useState, type FormEvent, type ReactNode } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { apiErrorText } from '../../api/errors'
import type { MailAccountDetail, MailDomainDetail } from '../../api/hestia.gen'
import { useMailAccount, useMailAccounts, useMailMutation, useMailOptions } from '../../api/mail'
import type { MailOptions } from '../../api/types.gen'
import { Optional, TagField } from '../../components/fields'
import { FormPage } from '../../components/FormPage'
import { FormSection } from '../../components/FormSection'
import { PasswordInput } from '../../components/PasswordInput'
import { StatusBadge } from '../../components/StatusBadge'
import { useT, type Translate } from '../../i18n/I18nProvider'
import { isEmail, isEmailLocalPart } from '../../lib/validate'
import { useSessionUser } from '../../session/SessionProvider'
import { DnsError, DnsLoading } from '../dns/shared'
import { EmailSettingsPanel } from './EmailSettingsPanel'

const list = (s: string) => s.split(',').filter(Boolean)

/** An account with no quota set is unlimited (Hestia stores 0 or "unlimited"). */
const isUnlimited = (quota: string) => quota === '' || quota === 'unlimited' || quota === '0'

/** Hestia reports "system" for the default rate limit; the field shows that as empty. */
const rateOf = (rate: string) => (rate === 'system' ? '' : rate)

const backTo = (domain: string, t: Translate) => ({
  href: `/mail/${encodeURIComponent(domain)}`,
  label: t('%s accounts', domain),
})

function ErrorBanner({ text }: { text: string }) {
  if (!text) return null
  return <Banner variant="error" icon={<WarningIcon weight="fill" />} title={text} className="whitespace-pre-line" />
}

/** The create-success message: its {...} links to the edit page. */
function createdMessage(t: (s: string) => string, domain: string, account: string): ReactNode {
  const parts = t('Mail account {%s@%s} has been created successfully.').split(/(\{[^}]*\})/)
  return (
    <>
      {parts.map((part, i) =>
        /^\{[^}]*\}$/.test(part) ? (
          <Link
            key={i}
            to={`/mail/${encodeURIComponent(domain)}/accounts/${encodeURIComponent(account)}`}
            className="underline"
          >
            {`${account}@${domain}`}
          </Link>
        ) : (
          part
        ),
      )}
    </>
  )
}

/** Webmail is offered by the server and set on the domain (a client other than "disabled"). */
const webmailOn = (options: MailOptions, domain: MailDomainDetail) =>
  options.webmail && domain.WEBMAIL !== '' && domain.WEBMAIL !== 'disabled'

export function MailAccountFormPage({ mode }: { mode: 'add' | 'edit' }) {
  const t = useT()
  const { domain = '', account = '' } = useParams()
  const title = mode === 'add' ? t('Add mail account') : t('Edit mail account')
  const options = useMailOptions()
  const accounts = useMailAccounts(domain)
  const detail = useMailAccount(domain, mode === 'edit' ? account : '')
  const failed = options.error ?? accounts.error ?? detail.error
  if (failed || !options.data || !accounts.data || (mode === 'edit' && !detail.data)) {
    return (
      <FormPage title={title} back={backTo(domain, t)} readOnly>
        {failed ? <DnsError error={failed} /> : <DnsLoading />}
      </FormPage>
    )
  }
  return (
    <MailAccountForm
      mode={mode}
      domain={accounts.data.domain}
      options={options.data}
      account={detail.data?.account}
      autoreplyMessage={detail.data?.autoreplyMessage ?? ''}
    />
  )
}

function MailAccountForm({
  mode,
  domain: d,
  options,
  account: a,
  autoreplyMessage,
}: {
  mode: 'add' | 'edit'
  domain: MailDomainDetail
  options: MailOptions
  account?: MailAccountDetail
  autoreplyMessage: string
}) {
  const t = useT()
  const user = useSessionUser()
  const m = useMailMutation()
  const toast = useKumoToastManager()
  const navigate = useNavigate()
  const domain = d.NAME
  const email = a ? `${a.NAME}@${domain}` : ''
  const blackholed = a?.FWD === ':blackhole:'

  const [name, setName] = useState(a?.NAME ?? '')
  const [password, setPassword] = useState('')
  const [quota, setQuota] = useState(a && !isUnlimited(a.QUOTA) ? a.QUOTA : '')
  const [unlimited, setUnlimited] = useState(a ? isUnlimited(a.QUOTA) : false)
  const [aliases, setAliases] = useState<string[]>(list(a?.ALIAS ?? ''))
  const [blackhole, setBlackhole] = useState(blackholed)
  const [fwd, setFwd] = useState<string[]>(blackholed ? [] : list(a?.FWD ?? ''))
  const [fwdOnly, setFwdOnly] = useState(blackholed || a?.FWD_ONLY === 'yes')
  const [autoreply, setAutoreply] = useState(a?.AUTOREPLY === 'yes')
  const [message, setMessage] = useState(autoreplyMessage)
  const [rate, setRate] = useState(rateOf(a?.RATE_LIMIT ?? ''))
  const [sendTo, setSendTo] = useState('')
  const [advanced, setAdvanced] = useState(mode === 'edit')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const back = backTo(domain, t)
  const title = mode === 'add' ? t('Add mail account') : t('Edit mail account')
  const suspended = a?.SUSPENDED === 'yes' || d.SUSPENDED === 'yes'
  const webmailAlias = webmailOn(options, d) ? options.webmailAlias : undefined

  const toggleBlackhole = (on: boolean) => {
    setBlackhole(on)
    // Discard all mail implies "do not store"; unchecking reverses it (discardAllMail.js)
    setFwdOnly(on)
  }

  const onSubmit = async (e: FormEvent) => {
    void e
    setError('')
    setBusy(true)
    const common = {
      quota: unlimited ? 'unlimited' : quota.trim(),
      // Raw text; the server splits it as PHP did
      aliases: aliases.join('\n'),
      forwards: blackhole ? '' : fwd.join('\n'),
      discard: blackhole,
      forwardOnly: blackhole || (fwdOnly && fwd.length > 0),
      // Only an admin may set the rate; absent means "not offered"
      ...(options.canSetRate ? { rate: rate.trim() } : {}),
      sendTo: sendTo.trim(),
    }
    try {
      if (mode === 'add') {
        await m.addAccount(domain, { account: name, password, ...common })
        toast.add({ title: createdMessage(t, domain, name), type: 'success' })
      } else {
        // An empty password keeps the current one
        await m.editAccount(domain, a?.NAME ?? '', {
          password,
          ...common,
          autoreply,
          autoreplyMessage: message,
        })
        toast.add({ title: t('Changes have been saved.'), type: 'success' })
      }
      navigate(back.href)
    } catch (err) {
      setError(apiErrorText(err, t))
      setBusy(false)
    }
  }

  const settings = (
    <>
      <FormSection title={t('Mailbox')} description={t('Storage limit and other addresses that deliver to this mailbox.')}>
        <div className="grid gap-2">
          <InputGroup label={t('Quota')} disabled={unlimited}>
            <InputGroup.Input
              aria-label={t('Quota')}
              inputMode="numeric"
              value={unlimited ? t('Unlimited') : quota}
              onChange={(e) => setQuota(e.target.value)}
            />
            {!unlimited && <InputGroup.Addon align="end">{t('MB')}</InputGroup.Addon>}
          </InputGroup>
          <Checkbox label={t('Unlimited')} checked={unlimited} onCheckedChange={(v) => setUnlimited(Boolean(v))} />
        </div>
        <TagField
          label={
            <>
              {t('Aliases')} <Optional />
            </>
          }
          description={t('Use local-part without domain name, e.g. info for info@%s.', domain)}
          value={aliases}
          onValueChange={setAliases}
          placeholder="info"
          autoComplete="off"
          validateValue={(v, accepted) => isEmailLocalPart(v) && v !== name && !accepted.includes(v)}
        />
        <InputGroup label={t('Rate limit')} disabled={!options.canSetRate}>
          <InputGroup.Input
            aria-label={t('Rate limit')}
            inputMode="numeric"
            value={rate}
            placeholder={options.canSetRate ? t('System default') : undefined}
            onChange={(e) => setRate(e.target.value)}
          />
          <InputGroup.Addon align="end">{t('email / hour')}</InputGroup.Addon>
        </InputGroup>
      </FormSection>

      <FormSection title={t('Forwarding')} description={t('Send a copy of incoming mail to other addresses.')}>
        <Checkbox label={t('Discard all mail')} checked={blackhole} onCheckedChange={(v) => toggleBlackhole(Boolean(v))} />
        {blackhole ? (
          <Text variant="secondary">
            {t('Incoming mail for this account is accepted and deleted without being stored.')}
          </Text>
        ) : (
          <>
            <TagField
              label={
                <>
                  {t('Forward to')} <Optional />
                </>
              }
              description={t('One or more email addresses. Press Enter or comma after each one.')}
              value={fwd}
              onValueChange={setFwd}
              placeholder="orders@northwind-shop.io"
              autoComplete="off"
              validateValue={(v, accepted) => isEmail(v) && !accepted.includes(v)}
            />
            <Checkbox
              label={t('Do not store forwarded mail')}
              checked={fwdOnly}
              disabled={fwd.length === 0}
              onCheckedChange={(v) => setFwdOnly(Boolean(v))}
            />
          </>
        )}
      </FormSection>

      {mode === 'edit' && (
        <FormSection
          title={t('Auto reply')}
          description={t('Answer every incoming message automatically, e.g. while on holiday.')}
          aside={<Switch label={t('Auto reply')} checked={autoreply} onCheckedChange={setAutoreply} />}
        >
          {autoreply ? (
            // Not `required`: an empty message is the server's blank-field error (Parity 5)
            <InputArea label={t('Message')} value={message} onValueChange={setMessage} autoResize minRows={4} />
          ) : (
            <Text variant="secondary">{t('Auto reply is off.')}</Text>
          )}
        </FormSection>
      )}
    </>
  )

  return (
    <FormPage
      title={title}
      description={
        mode === 'edit' ? (
          <span className="inline-flex flex-wrap items-center gap-2">
            {email} <StatusBadge suspended={suspended} />
          </span>
        ) : (
          t('New mailbox on %s.', domain)
        )
      }
      back={back}
      submitLabel={mode === 'add' ? t('Add mail account') : t('Save')}
      onSubmit={onSubmit}
      submitDisabled={busy}
      readOnly={user?.readOnly || suspended}
    >
      {suspended && (
        <Banner
          variant="alert"
          icon={<WarningIcon weight="fill" />}
          title={t('This mail account is suspended')}
          description={t("It can't receive mail or sign in. Unsuspend it from the accounts list before editing.")}
        />
      )}
      <ErrorBanner text={error} />

      <FormSection title={t('Account')} description={t('The address and the password used to sign in.')}>
        <Input label={t('Domain')} value={domain} disabled />
        <InputGroup label={t('Account')} disabled={mode === 'edit'}>
          <InputGroup.Input
            aria-label={t('Account')}
            value={name}
            onChange={(e) => setName(e.target.value.toLowerCase())}
            placeholder="hello"
            pattern="[A-Za-z0-9_.\-]{1,64}"
            title={t('Use letters, numbers, dots, hyphens or underscores')}
            required
          />
          <InputGroup.Suffix>@{domain}</InputGroup.Suffix>
        </InputGroup>
        <PasswordInput
          label={
            mode === 'edit' ? (
              <>
                {t('Password')} <Optional>{t('leave empty to keep current')}</Optional>
              </>
            ) : (
              t('Password')
            )
          }
          value={password}
          onChange={setPassword}
          required={mode === 'add' ? true : undefined}
        />
        <Input
          label={
            <>
              {t('Email login credentials to:')} <Optional />
            </>
          }
          description={t('Sends the address, password and client settings to this email.')}
          type="email"
          value={sendTo}
          onChange={(e) => setSendTo(e.target.value)}
          placeholder="marta@northwind-shop.io"
        />
      </FormSection>

      {mode === 'add' ? (
        <Collapsible.Root open={advanced} onOpenChange={setAdvanced}>
          <Collapsible.DefaultTrigger>{t('Advanced options')}</Collapsible.DefaultTrigger>
          <Collapsible.Panel>
            <div className="grid gap-8 pt-6">{settings}</div>
          </Collapsible.Panel>
        </Collapsible.Root>
      ) : (
        settings
      )}

      <FormSection
        title={t('Common account settings')}
        description={t('Give these to the mailbox owner to set up their mail app.')}
      >
        <EmailSettingsPanel account={name} domain={domain} password={password} webmailAlias={webmailAlias} />
      </FormSection>
    </FormPage>
  )
}
