import { Input, Select, useKumoToastManager } from '@cloudflare/kumo'
import { useRef, useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router'
import { useServerMutation } from '../../api/server'
import type { JobStatus } from '../../api/types.gen'
import { FormPage } from '../../components/FormPage'
import type { FormGuard } from '../../unsaved/types'
import { FormSection } from '../../components/FormSection'
import { useT } from '../../i18n/I18nProvider'
import { banChains, ruleComment } from './firewallShared'
import { ErrorNote, JobPanel } from './parts'
import { ipsetSources } from './IpsetCountryMap'

export function FirewallBanFormPage() {
  const t = useT()
  const m = useServerMutation()
  const toast = useKumoToastManager()
  const navigate = useNavigate()
  const back = { href: '/server/firewall/banlist', label: t('Banned IP addresses') }
  const [ip, setIp] = useState('')
  const [chain, setChain] = useState('SSH')
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)

  const onSubmit = async (e: FormEvent) => {
    void e
    setError(null)
    setBusy(true)
    try {
      await m.addBan({ ip: ip.trim(), chain })
      toast.add({ title: t('IP address has been banned successfully.'), type: 'success' })
      navigate(back.href)
    } catch (err) {
      setError(err)
      setBusy(false)
    }
  }

  return (
    <FormPage
      title={t('Add IP address to banlist')}
      description={t('Block an address from reaching one service chain.')}
      back={back}
      submitLabel={t('Ban IP address')}
      onSubmit={onSubmit}
      submitDisabled={busy}
    >
      <ErrorNote error={error} />
      <FormSection title={t('Ban')}>
        <Input
          label={t('IP address')}
          description={t('Support CIDR format')}
          value={ip}
          onChange={(e) => setIp(e.target.value)}
          required
          placeholder="198.51.100.0/24"
          className="font-mono"
        />
        <Select
          label={t('Banlist')}
          value={chain}
          onValueChange={(v) => setChain(String(v))}
          items={Object.fromEntries(banChains.map((c) => [c, ruleComment(c)]))}
          className="w-full"
        />
      </FormSection>
    </FormPage>
  )
}

export function FirewallIpsetFormPage() {
  const t = useT()
  const m = useServerMutation()
  const toast = useKumoToastManager()
  const navigate = useNavigate()
  const back = { href: '/server/firewall/ipsets', label: t('IPset IP lists') }
  const [name, setName] = useState('')
  const [source, setSource] = useState('')
  const [ipVersion, setIpVersion] = useState('v4')
  const [autoupdate, setAutoupdate] = useState('yes')
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)
  const [jobId, setJobId] = useState('')

  // The guard of the save in progress: a job that fails re-arms it.
  const saving = useRef<FormGuard | null>(null)
  // Adding a list downloads it, so it runs as a job (up to three minutes).
  const onSubmit = async (e: FormEvent, guard: FormGuard) => {
    void e
    saving.current = guard
    setError(null)
    setBusy(true)
    try {
      const ref = await m.addIpset({
        name: name.trim(),
        source: source.trim(),
        ipVersion,
        autoupdate: autoupdate === 'yes',
      })
      setJobId(ref.jobId)
    } catch (err) {
      setError(err)
      setBusy(false)
    }
  }
  const onDone = (status: JobStatus) => {
    setBusy(false)
    if (status.state !== 'done') {
      saving.current?.rearm()
      return
    }
    toast.add({ title: t('IP list has been created successfully.'), type: 'success' })
    navigate(back.href)
  }

  return (
    <FormPage
      title={t('Add IPset IP list for firewall')}
      description={t('A named list of addresses, loaded from a URL or the blocklist script.')}
      back={back}
      submitLabel={t('Add IP list')}
      onSubmit={onSubmit}
      submitDisabled={busy}
    >
      <ErrorNote error={error} />
      <JobPanel jobId={jobId} onDone={onDone} />
      <FormSection title={t('IP list')}>
        <Input
          label={t('IP list name')}
          value={name}
          onChange={(e) => setName(e.target.value)}
          maxLength={27}
          required
          placeholder="country-cn"
        />
        <div className="grid gap-2">
          <Input
            label={t('Data source')}
            description={t('An http(s) URL, or choose one of the lists below.')}
            value={source}
            onChange={(e) => setSource(e.target.value)}
            maxLength={255}
            required
            placeholder="https://"
            className="font-mono"
          />
          <Select
            aria-label={t('Choose a preset list')}
            placeholder={t('Choose a preset list')}
            value={ipsetSources.some((s) => s.source === source) ? source : null}
            onValueChange={(v) => setSource(String(v ?? ''))}
            items={[{ label: t('Clear'), value: '' }, ...ipsetSources.map((s) => ({ label: s.name, value: s.source }))]}
            className="w-full sm:w-80"
          />
        </div>
        <div className="grid gap-4 sm:grid-cols-2">
          <Select
            label={t('IP version')}
            value={ipVersion}
            onValueChange={(v) => setIpVersion(String(v))}
            items={{ v4: 'IPv4', v6: 'IPv6' }}
            className="w-full"
          />
          <Select
            label={t('Auto update')}
            value={autoupdate}
            onValueChange={(v) => setAutoupdate(String(v))}
            items={{ yes: t('Yes'), no: t('No') }}
            className="w-full"
          />
        </div>
      </FormSection>
    </FormPage>
  )
}
