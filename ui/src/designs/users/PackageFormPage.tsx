import { Banner, Button, Empty, Input, LinkButton, useKumoToastManager } from '@cloudflare/kumo'
import { PackageIcon, WarningIcon } from '@phosphor-icons/react'
import { useState, type FormEvent } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { ApiError } from '../../api/client'
import { apiErrorText } from '../../api/errors'
import type { Package } from '../../api/hestia.gen'
import type { PackageOptions, PackageRequest } from '../../api/types.gen'
import { usePackage, usePackageMutation, usePackageOptions } from '../../api/users'
import { NameServersField, SelectField } from '../../components/fields'
import { braceMessage } from '../../components/BraceMessage'
import { FormPage } from '../../components/FormPage'
import { FormSection } from '../../components/FormSection'
import { FormWithToc, TocAnchor } from '../../components/FormWithToc'
import { PageHeader } from '../../components/PageHeader'
import { useT } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { formatBackend } from '../../lib/format'
import { useSessionUser } from '../../session/SessionProvider'
import { DnsError, DnsLoading } from '../dns/shared'
import { LimitInput } from './fields'

const back = { href: '/packages', label: msgid('Packages') }

/** Hestia's add-package defaults (users §2.4). */
const NEW_PACKAGE: Package = {
  NAME: '',
  WEB_TEMPLATE: 'default',
  BACKEND_TEMPLATE: 'default',
  PROXY_TEMPLATE: 'default',
  DNS_TEMPLATE: 'default',
  WEB_DOMAINS: '1',
  WEB_ALIASES: '5',
  DNS_DOMAINS: '1',
  DNS_RECORDS: 'unlimited',
  MAIL_DOMAINS: '1',
  MAIL_ACCOUNTS: '5',
  RATE_LIMIT: '200',
  DATABASES: '1',
  CRON_JOBS: '1',
  DISK_QUOTA: '1000',
  CPU_QUOTA: 'unlimited',
  CPU_QUOTA_PERIOD: 'unlimited',
  MEMORY_LIMIT: 'unlimited',
  SWAP_LIMIT: 'unlimited',
  BANDWIDTH: '1000',
  NS: 'ns1.example.tld,ns2.example.tld',
  SHELL: 'nologin',
  BACKUPS: '1',
  BACKUPS_INCREMENTAL: 'no',
  TIME: '',
  DATE: '',
}

const SECTIONS = [
  { id: 'package', label: msgid('Package') },
  { id: 'storage', label: msgid('Storage and backups') },
  { id: 'web', label: msgid('Web') },
  { id: 'dns', label: 'DNS' },
  { id: 'mail', label: msgid('Mail') },
  { id: 'databases', label: msgid('Databases') },
  { id: 'system', label: msgid('System') },
  { id: 'resources', label: msgid('System resources') },
]

const choices = (list: string[], format: (v: string) => string = (v) => v) =>
  list.map((v) => ({ value: v, label: format(v) }))

/** The stored value when the list still offers it; a package may name a template that was removed. */
const withCurrent = (list: string[], current: string) => (current && !list.includes(current) ? [current, ...list] : list)

function templateLabel(label: string, system: string) {
  return (
    <>
      {label} <span className="font-normal text-kumo-subtle">({system.toUpperCase()})</span>
    </>
  )
}

export function PackageAddPage() {
  const t = useT()
  const options = usePackageOptions()
  if (options.error || !options.data) {
    return (
      <FormPage title={t('Add package')} back={{ ...back, label: t(back.label) }} readOnly>
        {options.error ? <DnsError error={options.error} /> : <DnsLoading />}
      </FormPage>
    )
  }
  return <PackageForm mode="add" options={options.data} pkg={NEW_PACKAGE} />
}

export function PackageEditPage() {
  const t = useT()
  const { package: name = '' } = useParams()
  const options = usePackageOptions()
  const stored = usePackage(name)
  const failed = options.error ?? stored.error
  if (failed || !options.data || !stored.data) {
    if (stored.error && stored.error instanceof ApiError && stored.error.status === 404) return <NotFound name={name} />
    return (
      <FormPage title={t('Edit package')} back={{ ...back, label: t(back.label) }} readOnly>
        {failed ? <DnsError error={failed} /> : <DnsLoading />}
      </FormPage>
    )
  }
  return <PackageForm key={name} mode="edit" name={name} options={options.data} pkg={stored.data} />
}

function NotFound({ name }: { name: string }) {
  const t = useT()
  return (
    <div className="grid gap-6">
      <PageHeader title={t('Package not found')} back={{ ...back, label: t(back.label) }} />
      <Empty
        icon={<PackageIcon size={32} />}
        title={t('There\'s no package named “%s”', name)}
        description={t('It may have been deleted or renamed.')}
        contents={<LinkButton href="/packages">{t('Back to packages')}</LinkButton>}
      />
    </div>
  )
}

type FormProps = { options: PackageOptions; pkg: Package } & ({ mode: 'add'; name?: undefined } | { mode: 'edit'; name: string })

function PackageForm({ mode, name, options, pkg }: FormProps) {
  const t = useT()
  const toast = useKumoToastManager()
  const navigate = useNavigate()
  const user = useSessionUser()
  const m = usePackageMutation()
  const edit = mode === 'edit'
  const [webTemplate, setWebTemplate] = useState(pkg.WEB_TEMPLATE)
  const [backendTemplate, setBackendTemplate] = useState(pkg.BACKEND_TEMPLATE)
  const [proxyTemplate, setProxyTemplate] = useState(pkg.PROXY_TEMPLATE)
  const [dnsTemplate, setDnsTemplate] = useState(pkg.DNS_TEMPLATE)
  const [shell, setShell] = useState(pkg.SHELL)
  const [incremental, setIncremental] = useState(pkg.BACKUPS_INCREMENTAL || 'no')
  const [nameservers, setNameservers] = useState(() => pkg.NS.split(',').filter(Boolean))
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const readOnly = user?.readOnly ?? false

  const submit = async (e: FormEvent) => {
    const fd = new FormData(e.currentTarget as HTMLFormElement)
    // A limit set to Unlimited is a disabled input, which a form leaves out.
    const limit = (key: string) => String(fd.get(key) ?? 'unlimited').trim()
    const text = (key: string) => String(fd.get(key) ?? '').trim()
    const body: PackageRequest = {
      name: text('v_package_new'),
      webTemplate,
      backendTemplate,
      proxyTemplate,
      dnsTemplate,
      webDomains: limit('v_web_domains'),
      webAliases: limit('v_web_aliases'),
      dnsDomains: limit('v_dns_domains'),
      dnsRecords: limit('v_dns_records'),
      mailDomains: limit('v_mail_domains'),
      mailAccounts: limit('v_mail_accounts'),
      rateLimit: text('v_ratelimit'),
      databases: limit('v_databases'),
      cronJobs: limit('v_cron_jobs'),
      diskQuota: limit('v_disk_quota'),
      bandwidth: limit('v_bandwidth'),
      cpuQuota: options.resourcesLimit ? limit('v_cpu_quota') : 'unlimited',
      cpuQuotaPeriod: options.resourcesLimit ? limit('v_cpu_quota_period') : 'unlimited',
      memoryLimit: options.resourcesLimit ? limit('v_memory_limit') : 'unlimited',
      swapLimit: options.resourcesLimit ? limit('v_swap_limit') : 'unlimited',
      backups: text('v_backups'),
      backupsIncremental: incremental,
      shell,
      // Without a DNS system the name servers aren't offered, and none are sent.
      nameservers: options.dnsSystem ? nameservers : [],
    }
    setBusy(true)
    setError('')
    try {
      const saved = edit ? await m.edit(name, body) : await m.add(body)
      if (edit && saved.name === name) {
        toast.add({ title: t('Changes have been saved.'), type: 'success' })
      } else {
        toast.add({
          title: edit
            ? t('Changes have been saved.')
            : braceMessage(t('Package {%s} has been created successfully.'), saved.name, (n) => (
                <Link to={`/packages/${encodeURIComponent(saved.name)}`} className="underline">
                  {n}
                </Link>
              )),
          type: 'success',
        })
        // A rename changes the address; stay on the form under the new name.
        navigate(edit ? `/packages/${encodeURIComponent(saved.name)}` : '/packages', { replace: edit })
      }
    } catch (err) {
      setError(apiErrorText(err, t))
    } finally {
      setBusy(false)
    }
  }

  const copy = async () => {
    try {
      await m.copy(name ?? '')
      toast.add({ title: t('Package %s created', `${name}-copy`), type: 'success' })
      navigate('/packages')
    } catch (err) {
      setError(apiErrorText(err, t))
    }
  }

  return (
    <FormPage
      title={edit ? t('Edit package %s', name) : t('Add package')}
      description={edit ? t('Changes apply to every user on this package.') : t('Define limits and templates you can assign to users.')}
      back={{ ...back, label: t(back.label) }}
      submitLabel={edit ? t('Save') : t('Add package')}
      readOnly={readOnly}
      submitDisabled={busy}
      onSubmit={submit}
      actions={
        edit && (
          <Button type="button" variant="secondary" onClick={copy} disabled={readOnly}>
            {t('Copy package')}
          </Button>
        )
      }
    >
      {error && <Banner variant="error" icon={<WarningIcon weight="fill" />} title={error} className="whitespace-pre-line" />}
      <FormWithToc
        sections={SECTIONS.filter((s) => s.id !== 'resources' || options.resourcesLimit).map((s) => ({ ...s, label: t(s.label) }))}
      >
        <TocAnchor id="package">
          <FormSection title={t('Package')} description={t('A short name shown when assigning the package to a user.')}>
            <Input label={t('Package name')} name="v_package_new" required defaultValue={name ?? ''} placeholder="business" />
          </FormSection>
        </TocAnchor>

        <TocAnchor id="storage">
          <FormSection
            title={t('Storage and backups')}
            description={t('Disk and traffic allowances, and how many backups are kept.')}
          >
            <LimitInput label={t('Quota')} unit={t('in MB')} name="v_disk_quota" defaultValue={pkg.DISK_QUOTA} />
            <LimitInput label={t('Bandwidth')} unit={t('in MB')} name="v_bandwidth" defaultValue={pkg.BANDWIDTH} />
            <LimitInput label={t('Backups')} name="v_backups" defaultValue={pkg.BACKUPS} allowUnlimited={false} />
            <SelectField
              label={t('Incremental backups')}
              value={incremental}
              onChange={setIncremental}
              options={[
                { value: 'no', label: t('Disabled') },
                { value: 'yes', label: t('Enabled') },
              ]}
            />
          </FormSection>
        </TocAnchor>

        <TocAnchor id="web">
          <FormSection title={t('Web')} description={t('Limits and the templates new web domains start with.')}>
            <LimitInput label={t('Web domains')} name="v_web_domains" defaultValue={pkg.WEB_DOMAINS} />
            <LimitInput label={t('Web aliases')} unit={t('per domain')} name="v_web_aliases" defaultValue={pkg.WEB_ALIASES} />
            <SelectField
              label={templateLabel(t('Web template'), options.webSystem)}
              value={webTemplate}
              onChange={setWebTemplate}
              options={choices(withCurrent(options.webTemplates, webTemplate))}
            />
            {options.webBackend !== '' && (
              <SelectField
                label={templateLabel(t('Backend template'), options.webBackend)}
                value={backendTemplate}
                onChange={setBackendTemplate}
                options={choices(withCurrent(options.backendTemplates, backendTemplate), formatBackend)}
              />
            )}
            {options.proxySystem !== '' && (
              <SelectField
                label={templateLabel(t('Proxy template'), options.proxySystem)}
                value={proxyTemplate}
                onChange={setProxyTemplate}
                options={choices(withCurrent(options.proxyTemplates, proxyTemplate))}
              />
            )}
          </FormSection>
        </TocAnchor>

        <TocAnchor id="dns">
          <FormSection title="DNS" description={t('Zone limits, the zone template, and the name servers new zones use.')}>
            <SelectField
              label={options.dnsSystem ? templateLabel(t('DNS template'), options.dnsSystem) : t('DNS template')}
              value={dnsTemplate}
              onChange={setDnsTemplate}
              options={choices(withCurrent(options.dnsTemplates, dnsTemplate))}
            />
            <LimitInput label={t('DNS zones')} name="v_dns_domains" defaultValue={pkg.DNS_DOMAINS} />
            <LimitInput label={t('DNS records')} unit={t('per domain')} name="v_dns_records" defaultValue={pkg.DNS_RECORDS} />
            {options.dnsSystem !== '' && <NameServersField defaultValue={nameservers} onChange={setNameservers} />}
          </FormSection>
        </TocAnchor>

        <TocAnchor id="mail">
          <FormSection title={t('Mail')} description={t('Mail domains, mailboxes and outgoing rate limits.')}>
            <LimitInput label={t('Mail domains')} name="v_mail_domains" defaultValue={pkg.MAIL_DOMAINS} />
            <LimitInput label={t('Mail accounts')} unit={t('per domain')} name="v_mail_accounts" defaultValue={pkg.MAIL_ACCOUNTS} />
            <LimitInput
              label={t('Rate limit')}
              unit={t('per account / hour')}
              name="v_ratelimit"
              defaultValue={pkg.RATE_LIMIT}
              allowUnlimited={false}
              description={t('Maximum number of emails each account can send per hour.')}
            />
          </FormSection>
        </TocAnchor>

        <TocAnchor id="databases">
          <FormSection title={t('Databases')}>
            <LimitInput label={t('Databases')} name="v_databases" defaultValue={pkg.DATABASES} />
          </FormSection>
        </TocAnchor>

        <TocAnchor id="system">
          <FormSection title={t('System')} description={t('Scheduled tasks and shell access.')}>
            <LimitInput label={t('Cron jobs')} name="v_cron_jobs" defaultValue={pkg.CRON_JOBS} />
            <SelectField label={t('SSH access')} value={shell} onChange={setShell} options={choices(withCurrent(options.shells, shell))} />
          </FormSection>
        </TocAnchor>

        {options.resourcesLimit && (
          <TocAnchor id="resources">
            <FormSection
              title={t('System resources')}
              description={t(
                "systemd limits applied to the user's processes. Leave unlimited unless you need to cap noisy accounts.",
              )}
            >
              <LimitInput
                label={t('CPU quota')}
                unit={t('in %')}
                name="v_cpu_quota"
                defaultValue={pkg.CPU_QUOTA}
                description={t('CPUQuota=20% ensures that the executed processes will never get more than 20% CPU time on one CPU.')}
              />
              <LimitInput
                label={t('CPU quota period')}
                unit={t('ms or s')}
                name="v_cpu_quota_period"
                defaultValue={pkg.CPU_QUOTA_PERIOD}
                description={t('CPUQuotaPeriodSec=10ms requests that the CPU quota is measured in periods of 10ms.')}
              />
              <LimitInput
                label={t('Memory limit')}
                unit={t('bytes or units like 2G')}
                name="v_memory_limit"
                defaultValue={pkg.MEMORY_LIMIT}
                description={t('Suffix with K, M, G or T for kilobytes, megabytes, gigabytes or terabytes (base 1024).')}
              />
              <LimitInput
                label={t('Swap limit')}
                unit={t('bytes or units like 2G')}
                name="v_swap_limit"
                defaultValue={pkg.SWAP_LIMIT}
                description={t('Suffix with K, M, G or T for kilobytes, megabytes, gigabytes or terabytes (base 1024).')}
              />
            </FormSection>
          </TocAnchor>
        )}
      </FormWithToc>
    </FormPage>
  )
}
