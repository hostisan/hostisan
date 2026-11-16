import { Optional, SelectField } from '../../components/fields'
import { Banner, Input, InputArea, InputGroup, useKumoToastManager } from '@cloudflare/kumo'
import { WarningIcon } from '@phosphor-icons/react'
import { useState, type FormEvent } from 'react'
import { useNavigate, useParams } from 'react-router'
import { useDnsMutation, useDnsOptions, useDnsRecords } from '../../api/dns'
import { apiErrorText } from '../../api/errors'
import type { DNSDomain, DNSRecord } from '../../api/hestia.gen'
import { FormPage } from '../../components/FormPage'
import { FormSection } from '../../components/FormSection'
import { StatusBadge } from '../../components/StatusBadge'
import { useT } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { useSessionUser } from '../../session/SessionProvider'
import { interpolate } from '../../lib/interpolate'
import { DnsError, DnsLoading, IpField, dnsRecordTypes } from './shared'

const valueHints: Record<string, string> = {
  A: msgid('IPv4 address, e.g. 203.0.113.24'),
  AAAA: msgid('IPv6 address, e.g. 2001:db8::25'),
  CNAME: msgid('Target host name ending with a dot, e.g. acme-bakery.com.'),
  MX: msgid('Mail server host name ending with a dot, e.g. mail.acme-bakery.com.'),
  NS: msgid('Name server host name ending with a dot'),
  TXT: msgid('Free text. Quotes are added automatically when missing.'),
  SPF: msgid('SPF policy, e.g. v=spf1 a mx -all'),
  SRV: msgid('Weight, port and target, e.g. 0 993 mail.acme-bakery.com.'),
  CAA: msgid('Flags, tag and value, e.g. 0 issue "letsencrypt.org"'),
  TLSA: msgid('Usage, selector, matching type and certificate data'),
  PTR: msgid('Host name this address points back to'),
}

const hasPriority = (t: string) => t === 'MX' || t === 'SRV'
const isAddress = (t: string) => t === 'A' || t === 'AAAA'
const isText = (t: string) => ['TXT', 'SPF', 'DNSKEY', 'KEY', 'IPSECKEY', 'DS', 'TLSA'].includes(t)

export function DnsRecordFormPage({ mode }: { mode: 'add' | 'edit' }) {
  const { domain = '', id = '' } = useParams()
  const t = useT()
  const records = useDnsRecords(domain)
  const options = useDnsOptions()
  const back = { href: `/dns/${domain}`, label: t('%s records', domain) }
  const title = mode === 'add' ? t('Add DNS record') : t('Edit DNS record')
  const record = mode === 'edit' ? records.data?.records.find((r) => r.ID === id) : undefined

  if (records.error || !records.data) {
    return (
      <FormPage title={title} back={back} readOnly>
        {records.error ? <DnsError error={records.error} /> : <DnsLoading />}
      </FormPage>
    )
  }
  if (mode === 'edit' && !record) {
    return (
      <FormPage title={title} back={back} readOnly>
        <Banner variant="error" icon={<WarningIcon weight="fill" />} title={t('Error: unknown record ID.')} />
      </FormPage>
    )
  }
  return (
    <RecordForm
      mode={mode}
      domain={domain}
      id={id}
      zone={records.data.zone}
      record={record}
      ips={options.data?.ips ?? []}
    />
  )
}

function RecordForm({
  mode,
  domain,
  id,
  zone,
  record,
  ips,
}: {
  mode: 'add' | 'edit'
  domain: string
  id: string
  zone: DNSDomain
  record?: DNSRecord
  ips: string[]
}) {
  const t = useT()
  const user = useSessionUser()
  const toast = useKumoToastManager()
  const navigate = useNavigate()
  const m = useDnsMutation()
  const [rec, setRec] = useState(record?.RECORD ?? '@')
  const [type, setType] = useState(record?.TYPE ?? 'A')
  const [value, setValue] = useState(record?.VALUE ?? '')
  const [priority, setPriority] = useState(record?.PRIORITY ?? '')
  const [ttl, setTtl] = useState(record?.TTL ?? '')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const back = { href: `/dns/${domain}`, label: t('%s records', domain) }
  const title = mode === 'add' ? t('Add DNS record') : t('Edit DNS record')
  const hint = (k: string) => (valueHints[k] ? t(valueHints[k]) : undefined)

  const suspended = record?.SUSPENDED === 'yes' || zone.SUSPENDED === 'yes'
  const apex = rec === '@' || rec === '' || rec === domain
  const fqdn = apex ? domain : `${rec}.${domain}`

  const onSubmit = async (e: FormEvent) => {
    void e
    setError('')
    setBusy(true)
    const body = { record: rec, type, value, priority: hasPriority(type) ? priority : '', ttl }
    try {
      if (mode === 'add') {
        await m.addRecord(domain, body)
        toast.add({
          title: t('Record {%s.%s} has been created successfully.', rec, domain).replace(/[{}]/g, ''),
          type: 'success',
        })
      } else {
        await m.editRecord(domain, id, body)
        toast.add({ title: t('Changes have been saved.'), type: 'success' })
      }
      navigate(back.href)
    } catch (err) {
      setError(apiErrorText(err, t))
      setBusy(false)
    }
  }

  return (
    <FormPage
      title={title}
      description={
        mode === 'edit' ? (
          <span className="inline-flex flex-wrap items-center gap-2">
            {fqdn} <StatusBadge suspended={suspended} />
          </span>
        ) : (
          t('New record in the %s zone.', domain)
        )
      }
      back={back}
      submitLabel={mode === 'add' ? t('Add record') : t('Save')}
      onSubmit={onSubmit}
      submitDisabled={busy}
      readOnly={user?.readOnly || suspended}
    >
      {suspended && (
        <Banner
          variant="alert"
          icon={<WarningIcon weight="fill" />}
          title={t('This record is suspended')}
          description={t('Unsuspend it from the records list before editing.')}
        />
      )}
      {error && (
        <Banner variant="error" icon={<WarningIcon weight="fill" />} title={error} className="whitespace-pre-line" />
      )}
      <FormSection title={t('Record')} description={t('The host name inside the zone and the record type.')}>
        <Input label={t('Domain')} value={domain} disabled />
        <InputGroup
          label={t('Record')}
          description={
            <>
              {interpolate(t('Use %s for the zone apex.'), <span className="font-mono text-[0.9em]">@</span>)}
              {!apex && (
                <>
                  {' '}
                  {interpolate(t('Resolves as %s'), <span className="font-mono text-[0.9em]">{fqdn}</span>)}
                </>
              )}
            </>
          }
        >
          <InputGroup.Input
            aria-label={t('Record')}
            value={rec}
            onChange={(e) => setRec(e.target.value)}
            placeholder="www"
            required
          />
          {!apex && <InputGroup.Suffix>.{domain}</InputGroup.Suffix>}
        </InputGroup>
        <SelectField
          label={t('Type')}
          value={type}
          onChange={setType}
          options={dnsRecordTypes.map((k) => ({ value: k, label: k }))}
        />
      </FormSection>

      <FormSection
        title={t('Value')}
        description={t('What the record answers with, and for how long resolvers may cache it.')}
      >
        {isAddress(type) ? (
          <IpField label={t('IP or value')} description={hint(type)} value={value} onChange={setValue} ips={ips} />
        ) : isText(type) ? (
          <InputArea
            label={t('IP or value')}
            description={hint(type)}
            value={value}
            onValueChange={setValue}
            autoResize
            minRows={3}
            className="font-mono"
          />
        ) : (
          <Input
            label={t('IP or value')}
            description={hint(type)}
            value={value}
            onChange={(e) => setValue(e.target.value)}
            className="font-mono"
          />
        )}
        {hasPriority(type) && (
          <Input
            label={
              <>
                {t('Priority')} <Optional />
              </>
            }
            description={t('Lower values are tried first.')}
            inputMode="numeric"
            value={priority}
            onChange={(e) => setPriority(e.target.value)}
            placeholder="10"
          />
        )}
        <Input
          label={
            <>
              TTL <Optional />
            </>
          }
          description={t('Seconds. Leave empty to use the zone default (%s).', zone.TTL)}
          inputMode="numeric"
          value={ttl}
          onChange={(e) => setTtl(e.target.value)}
          placeholder={zone.TTL}
        />
      </FormSection>
    </FormPage>
  )
}
