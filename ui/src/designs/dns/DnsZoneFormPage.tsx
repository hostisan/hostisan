import { NameServersField } from '../../components/fields'
import { DateField, Optional, SelectField } from '../../components/fields'
import { Banner, Button, Checkbox, Collapsible, Input, LinkButton, useKumoToastManager } from '@cloudflare/kumo'
import { WarningIcon } from '@phosphor-icons/react'
import { useState, type FormEvent } from 'react'
import { useNavigate, useParams } from 'react-router'
import { useDnsMutation, useDnsOptions, useDnsZone } from '../../api/dns'
import { apiErrorText } from '../../api/errors'
import type { DNSDomain } from '../../api/hestia.gen'
import type { DNSOptions } from '../../api/types.gen'
import { FormPage } from '../../components/FormPage'
import { FormSection } from '../../components/FormSection'
import { PageHeader } from '../../components/PageHeader'
import { StatusBadge } from '../../components/StatusBadge'
import { useT, type Translate } from '../../i18n/I18nProvider'
import { useSessionUser } from '../../session/SessionProvider'
import { DnsError, DnsLoading, IpField } from './shared'

// Hestia's add_dns form: ns1 and ns2 are required, up to ns8

const back = { href: '/dns', label: 'DNS' }

/** The name the server stores: a leading "www." is removed, then lower-cased. */
const normaliseDomain = (name: string) =>
  name
    .trim()
    .replace(/^www\./i, '')
    .toLowerCase()

export function DnsZoneFormPage({ mode }: { mode: 'add' | 'edit' }) {
  const { domain = '' } = useParams()
  const options = useDnsOptions()
  const zone = useDnsZone(domain)
  const t = useT()
  const title = mode === 'edit' ? t('Edit DNS domain') : t('Add DNS zone')

  const failed = options.error ?? (mode === 'edit' ? zone.error : null)
  if (failed || !options.data || (mode === 'edit' && !zone.data)) {
    return (
      <FormPage title={title} back={back} readOnly>
        {failed ? <DnsError error={failed} /> : <DnsLoading />}
      </FormPage>
    )
  }
  return mode === 'edit' && zone.data ? (
    <EditZoneForm domain={domain} zone={zone.data} options={options.data} />
  ) : (
    <AddZoneForm options={options.data} />
  )
}

type Shared = {
  ip: string
  setIp: (v: string) => void
  template: string
  setTemplate: (v: string) => void
  dnssec: boolean
  setDnssec: (v: boolean) => void
  exp: string
  setExp: (v: string) => void
  ttl: string
  setTtl: (v: string) => void
}

function useZoneState(init: { ip: string; template: string; dnssec: boolean; exp: string; ttl: string }) {
  const [ip, setIp] = useState(init.ip)
  const [template, setTemplate] = useState(init.template)
  const [dnssec, setDnssec] = useState(init.dnssec)
  const [exp, setExp] = useState(init.exp)
  const [ttl, setTtl] = useState(init.ttl)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  return {
    ip,
    setIp,
    template,
    setTemplate,
    dnssec,
    setDnssec,
    exp,
    setExp,
    ttl,
    setTtl,
    error,
    setError,
    busy,
    setBusy,
  }
}

function useSubmit(state: ReturnType<typeof useZoneState>, run: () => Promise<string>, to: string) {
  const t = useT()
  const toast = useKumoToastManager()
  const navigate = useNavigate()
  return async (e: FormEvent) => {
    void e
    state.setError('')
    state.setBusy(true)
    try {
      const message = await run()
      toast.add({ title: message, type: 'success' })
      navigate(to)
    } catch (err) {
      state.setError(apiErrorText(err, t))
      state.setBusy(false)
    }
  }
}

function ErrorBanner({ text }: { text: string }) {
  if (!text) return null
  return <Banner variant="error" icon={<WarningIcon weight="fill" />} title={text} className="whitespace-pre-line" />
}

function zoneFields({
  s,
  options,
  required,
  t,
}: {
  s: Shared
  options: DNSOptions
  required?: boolean
  t: Translate
}) {
  const templateField = options.canEditTemplates && (
    <SelectField
      label={
        <>
          {t('Template')} <Optional>{options.dnsSystem.toUpperCase()}</Optional>
        </>
      }
      value={s.template}
      onChange={s.setTemplate}
      options={options.templates.map((tpl) => ({ value: tpl, label: tpl }))}
    />
  )
  const dnssecField = options.supportsDnssec && (
    <Checkbox label={t('Enable DNSSEC')} checked={s.dnssec} onCheckedChange={(v) => s.setDnssec(Boolean(v))} />
  )
  const expField = (
    <DateField
      label={t('Expiration date')}
      description={t('When the domain registration expires.')}
      value={s.exp}
      onChange={s.setExp}
    />
  )
  const ttlField = (
    <Input
      label="TTL"
      description={t('Default time to live for records in this zone, in seconds.')}
      inputMode="numeric"
      required={required}
      value={s.ttl}
      onChange={(e) => s.setTtl(e.target.value)}
    />
  )
  return { templateField, dnssecField, expField, ttlField }
}

function EditZoneForm({ domain, zone, options }: { domain: string; zone: DNSDomain; options: DNSOptions }) {
  const t = useT()
  const user = useSessionUser()
  const m = useDnsMutation()
  const state = useZoneState({
    ip: zone.IP,
    template: zone.TPL,
    dnssec: zone.DNSSEC === 'yes',
    exp: zone.EXP,
    ttl: zone.TTL,
  })
  const [soa, setSoa] = useState(zone.SOA)
  // Edit requires every zone setting: the server rejects a blank one.
  const { templateField, dnssecField, expField, ttlField } = zoneFields({ s: state, options, required: true, t })
  const suspended = zone.SUSPENDED === 'yes'
  const onSubmit = useSubmit(
    state,
    async () => {
      await m.editZone(domain, {
        ip: state.ip,
        ...(options.canEditTemplates ? { template: state.template } : {}),
        soa,
        exp: state.exp,
        ttl: state.ttl,
        ...(options.supportsDnssec ? { dnssec: state.dnssec } : {}),
      })
      return t('Changes have been saved.')
    },
    '/dns',
  )
  return (
    <FormPage
      title={t('Edit DNS domain')}
      description={
        <span className="inline-flex flex-wrap items-center gap-2">
          {domain} <StatusBadge suspended={suspended} />
        </span>
      }
      back={back}
      onSubmit={onSubmit}
      submitDisabled={state.busy}
      readOnly={user?.readOnly || suspended}
      actions={
        <LinkButton href={`/dns/${domain}`} variant="secondary">
          {t('DNS records')}
        </LinkButton>
      }
    >
      {suspended && (
        <Banner
          variant="alert"
          icon={<WarningIcon weight="fill" />}
          title={t('This DNS domain is suspended')}
          description={t('The zone is not served until it is unsuspended.')}
        />
      )}
      <ErrorBanner text={state.error} />
      <FormSection title={t('Domain')} description={t("Where the zone's default records point.")}>
        <Input label={t('Domain')} value={domain} disabled />
        <IpField value={state.ip} onChange={state.setIp} ips={options.ips} required />
        {templateField}
      </FormSection>
      <FormSection title={t('Zone settings')} description={t('Expiry, start of authority and default TTL.')}>
        {dnssecField}
        {expField}
        <Input label="SOA" required value={soa} onChange={(e) => setSoa(e.target.value)} />
        {ttlField}
      </FormSection>
    </FormPage>
  )
}

function AddZoneForm({ options }: { options: DNSOptions }) {
  const t = useT()
  const user = useSessionUser()
  const m = useDnsMutation()
  const state = useZoneState({
    ip: options.ips[0] ?? '',
    template: options.defaultTemplate,
    dnssec: false,
    exp: options.defaultExpiry,
    ttl: options.defaultTTL,
  })
  const [name, setName] = useState('')
  const [nameservers, setNameservers] = useState(options.nameservers)
  const [advanced, setAdvanced] = useState(false)
  const [continued, setContinued] = useState(false)
  const { templateField, dnssecField, expField, ttlField } = zoneFields({ s: state, options, t })
  const onSubmit = useSubmit(
    state,
    async () => {
      await m.addZone({
        domain: name.trim(),
        ip: state.ip,
        ...(options.canEditTemplates ? { template: state.template } : {}),
        dnssec: options.supportsDnssec ? state.dnssec : false,
        exp: state.exp,
        ttl: state.ttl,
        nameservers,
      })
      return t('DNS zone {%s} has been created successfully.', normaliseDomain(name)).replace(/[{}]/g, '')
    },
    '/dns',
  )

  if (options.adminGate && !continued) {
    return (
      <div className="grid gap-8">
        <PageHeader
          title={t('Add DNS zone')}
          description={t('Create a zone and populate it from a DNS template.')}
          back={back}
        />
        <Banner
          variant="alert"
          icon={<WarningIcon weight="fill" />}
          title={t("You're signed in as admin")}
          description={t(
            'It is strongly advised to {create a standard user account} before adding %s to the server due to the increased privileges the admin account possesses and potential security risks.',
            t('a dns domain'),
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
      title={t('Add DNS zone')}
      description={t('Create a zone and populate it from a DNS template.')}
      back={back}
      submitLabel={t('Add DNS domain')}
      onSubmit={onSubmit}
      submitDisabled={state.busy}
      readOnly={user?.readOnly}
    >
      <ErrorBanner text={state.error} />
      <FormSection title={t('Domain')} description={t('The zone name and the IP address its default records point to.')}>
        <Input
          label={t('Domain')}
          required
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="acme-bakery.com"
        />
        <IpField value={state.ip} onChange={state.setIp} ips={options.ips} />
        {templateField}
      </FormSection>

      <Collapsible.Root open={advanced} onOpenChange={setAdvanced}>
        <Collapsible.DefaultTrigger>{t('Advanced options')}</Collapsible.DefaultTrigger>
        <Collapsible.Panel>
          <div className="grid gap-8 pt-6">
            <FormSection title={t('Zone settings')} description={t('DNSSEC signing, expiry and default TTL.')}>
              {dnssecField}
              {expField}
              {ttlField}
            </FormSection>
            <FormSection
              title={t('Name servers')}
              description={t("Up to eight authoritative name servers for the zone's NS records.")}
            >
              <NameServersField defaultValue={nameservers} onChange={setNameservers} />
            </FormSection>
          </div>
        </Collapsible.Panel>
      </Collapsible.Root>
    </FormPage>
  )
}
