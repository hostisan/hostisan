import { Banner, Button, Checkbox, Input, LinkButton, Select, useKumoToastManager } from '@cloudflare/kumo'
import { WarningIcon } from '@phosphor-icons/react'
import { useState, type ReactNode } from 'react'
import { Link, useNavigate } from 'react-router'
import { apiErrorText } from '../../api/errors'
import type { WebOptions } from '../../api/types.gen'
import { useWebMutation, useWebOptions } from '../../api/web'
import { FormPage } from '../../components/FormPage'
import { FormSection } from '../../components/FormSection'
import { useT, type Translate } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { useSessionUser } from '../../session/SessionProvider'
import { DnsError, DnsLoading } from '../dns/shared'

const back = { href: '/web', label: msgid('Web domains') }

/** Trimmed, a leading "www." removed, lowercased: what the server does with the typed name. */
const normaliseDomain = (name: string) =>
  name
    .trim()
    .replace(/^www\./i, '')
    .toLowerCase()

/** The create-success message: its {...} links to the new domain. */
function createdMessage(t: Translate, domain: string): ReactNode {
  const parts = t('Domain {%s} has been created successfully.').split(/(\{[^}]*\})/)
  return (
    <>
      {parts.map((part, i) => {
        const inner = /^\{([^}]*)\}$/.exec(part)?.[1]
        if (inner === undefined) return part
        return (
          <Link key={i} to={`/web/${encodeURIComponent(domain)}`} className="underline">
            {inner.replace('%s', () => domain)}
          </Link>
        )
      })}
    </>
  )
}

export function AddWebDomainPage() {
  const t = useT()
  const options = useWebOptions()
  if (options.error || !options.data) {
    return (
      <FormPage title={t('Add web domain')} back={{ ...back, label: t(back.label) }} readOnly>
        {options.error ? <DnsError error={options.error} /> : <DnsLoading />}
      </FormPage>
    )
  }
  return <AddWebDomainForm options={options.data} />
}

function AddWebDomainForm({ options }: { options: WebOptions }) {
  const t = useT()
  const toast = useKumoToastManager()
  const navigate = useNavigate()
  const m = useWebMutation()
  const readOnly = useSessionUser()?.readOnly ?? false
  const [accepted, setAccepted] = useState(!options.adminGate)
  const [domain, setDomain] = useState('')
  const [ip, setIp] = useState(options.ips[0]?.ip ?? '')
  const [dns, setDns] = useState(true)
  const [mail, setMail] = useState(true)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const onSubmit = async () => {
    setError('')
    setBusy(true)
    try {
      // A box the server doesn't offer is ignored there; send it off
      await m.addDomain({ domain, ip, dns: options.dns && dns, mail: options.mail && mail })
      toast.add({ title: createdMessage(t, normaliseDomain(domain)), type: 'success' })
      navigate('/web')
    } catch (err) {
      setError(apiErrorText(err, t))
      setBusy(false)
    }
  }

  return (
    <FormPage
      title={t('Add web domain')}
      description={t('Create a website. DNS and mail domains can be created alongside it.')}
      back={{ ...back, label: t(back.label) }}
      submitLabel={t('Add web domain')}
      onSubmit={onSubmit}
      submitDisabled={busy || !accepted || options.ips.length === 0}
      readOnly={readOnly}
    >
      {options.adminGate && (
        <Banner
          variant="alert"
          icon={<WarningIcon weight="fill" />}
          title={t('You are adding a web domain to the admin account')}
          description={t(
            'It is strongly advised to {create a standard user account} before adding %s to the server due to the increased privileges the admin account possesses and potential security risks.',
            t('a web domain'),
          ).replace(/[{}]/g, '')}
          action={
            accepted ? undefined : (
              <>
                <LinkButton href="/users/new" size="sm">
                  {t('Add user')}
                </LinkButton>
                <Button type="button" size="sm" variant="destructive" onClick={() => setAccepted(true)}>
                  {t('Continue')}
                </Button>
              </>
            )
          }
        />
      )}
      {error && <Banner variant="error" icon={<WarningIcon weight="fill" />} title={error} className="whitespace-pre-line" />}

      <FormSection title={t('Domain')} description={t('The www. prefix is removed automatically and added as an alias.')}>
        <Input
          label={t('Domain')}
          placeholder="acme-bakery.com"
          value={domain}
          onChange={(e) => setDomain(e.target.value)}
          required
          disabled={!accepted}
        />
        {options.ips.length === 0 ? (
          <Banner
            variant="alert"
            icon={<WarningIcon weight="fill" />}
            title={t('No IP address available')}
            description={t('This account has no IP address to host a website on. Ask your administrator to assign one.')}
          />
        ) : (
        <Select
          label={t('IP address')}
          value={ip}
          onValueChange={(v) => setIp(v ?? ip)}
          items={Object.fromEntries(options.ips.map((i) => [i.ip, i.display]))}
          disabled={!accepted}
        />
        )}
      </FormSection>

      {(options.dns || options.mail) && (
        <FormSection title={t('Related services')} description={t('Create matching zones on this server.')}>
          {options.dns && <Checkbox label={t('DNS support')} checked={dns} onCheckedChange={setDns} disabled={!accepted} />}
          {options.mail && <Checkbox label={t('Mail support')} checked={mail} onCheckedChange={setMail} disabled={!accepted} />}
        </FormSection>
      )}
    </FormPage>
  )
}
