import { scrollContentToTop } from '../../lib/scroll'
import { FormPage } from '../../components/FormPage'
import { CopyBlock } from '../../components/CopyBlock'
import { Banner, Button, Input, InputArea, LayerCard, LinkButton, Select, useKumoToastManager } from '@cloudflare/kumo'
import { ArrowCounterClockwiseIcon, CheckCircleIcon } from '@phosphor-icons/react'
import { useState } from 'react'
import { FormSection } from '../../components/FormSection'
import { PageHeader } from '../../components/PageHeader'
import { useParams, useSearchParams } from 'react-router'
import { ApiError } from '../../api/client'
import { apiErrorText } from '../../api/errors'
import type { WebGeneratedSSL } from '../../api/types.gen'
import { useWebDomains, useWebMutation } from '../../api/web'
import { useT } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { useSessionUser } from '../../session/SessionProvider'
import type { FormGuard } from '../../unsaved/types'

const KEY_TYPES = {
  'ecdsa-256': msgid('ECDSA P-256 (recommended)'),
  'ecdsa-384': 'ECDSA P-384',
  rsa: 'RSA 4096',
}

export function GenerateSslPage() {
  const { domain: name = '' } = useParams()
  const [params] = useSearchParams()
  const t = useT()
  const user = useSessionUser()
  const m = useWebMutation()
  const toast = useKumoToastManager()
  // Only the list is read: the generator needs no web domain, it just prefills the aliases of one
  const list = useWebDomains()
  const stored = list.data?.domains.find((d) => d.NAME === name)
  const [csrDomain, setCsrDomain] = useState(name)
  const [typedAliases, setTypedAliases] = useState<string>()
  const aliases = typedAliases ?? stored?.ALIAS.split(',').filter(Boolean).join('\n') ?? ''
  const setAliases = setTypedAliases
  const [email, setEmail] = useState('')
  const [country, setCountry] = useState('US')
  const [state, setState] = useState('California')
  const [locality, setLocality] = useState('San Francisco')
  const [org, setOrg] = useState('MyCompany Inc')
  const [keyAlgo, setKeyAlgo] = useState('ecdsa-256')
  const [result, setResult] = useState<WebGeneratedSSL | null>(null)
  const [error, setError] = useState('')
  // The API names the offending field in Args[0] of a 400 invalid
  const [badField, setBadField] = useState('')
  const [busy, setBusy] = useState(false)

  // ?back= is a path inside the panel (the mail SSL form sends /mail/<domain>)
  const requested = params.get('back') ?? ''
  const home = `/web/${name}`
  const target =
    requested.startsWith('/') && !requested.startsWith('//') && !requested.includes('\\') ? requested : home
  const back = { href: target, label: target === home ? name : target.startsWith('/mail/') ? t('Mail domain') : t('Back') }
  const fieldError = (label: string) => (badField === label ? error : undefined)
  const countryError = country && !/^[A-Za-z]{2}$/.test(country) ? t('Use a two-letter code, like US or DE.') : undefined

  const onSubmit = async (guard: FormGuard) => {
    if (countryError) {
      guard.rearm()
      return
    }
    setError('')
    setBadField('')
    setBusy(true)
    try {
      setResult(
        await m.generateSsl({
          domain: csrDomain,
          aliases,
          email,
          country,
          state,
          locality,
          org,
          keyAlgo,
        }),
      )
      toast.add({ title: t('Certificate has been generated successfully.'), type: 'success' })
      scrollContentToTop()
    } catch (err) {
      setError(apiErrorText(err, t))
      if (err instanceof ApiError && err.code === 'invalid') setBadField(err.args[0] ?? '')
    } finally {
      setBusy(false)
    }
  }

  if (result) {
    return (
      <div className="grid gap-8 pb-4">
        <PageHeader
          title={t('Generate self-signed SSL certificate')}
          description={t('For %s. Copy these now; the private key is not stored.', csrDomain)}
          back={back}
          actions={
            <>
              <Button icon={ArrowCounterClockwiseIcon} onClick={() => setResult(null)}>
                {t('Generate again')}
              </Button>
              <LinkButton href={back.href} variant="primary">
                {target === home ? t('Back to domain') : t('Done')}
              </LinkButton>
            </>
          }
        />
        <Banner
          icon={<CheckCircleIcon weight="fill" />}
          title={t('Certificate and signing request generated')}
          description={t(
            'Paste the certificate and private key into the SSL section of the domain to use the self-signed certificate, or send the CSR to a certificate authority.',
          )}
        />
        <LayerCard className="grid gap-6 px-5 py-4">
          <CopyBlock layout="scroll" label={t('SSL certificate')} filename={`${csrDomain}.crt`} value={result.crt} />
          <CopyBlock layout="scroll" label={t('SSL private key')} filename={`${csrDomain}.key`} value={result.key} />
          <CopyBlock
            layout="scroll"
            label={t('SSL CSR')}
            filename={`${csrDomain}.csr`}
            value={result.csr}
            description={t('Certificate signing request. Submit it to a certificate authority to get a trusted certificate.')}
          />
        </LayerCard>
      </div>
    )
  }

  return (
    <FormPage
      title={t('Generate self-signed SSL certificate')}
      description={t('Creates a private key, a certificate signing request (CSR) and a self-signed certificate.')}
      back={back}
      submitLabel={t('Generate')}
      onSubmit={(_e, guard) => void onSubmit(guard)}
      submitDisabled={busy}
      readOnly={user?.readOnly}
    >
      {error && <Banner variant="error" title={error} className="whitespace-pre-line" />}

      <FormSection title={t('Hostnames')} description={t('The certificate covers the domain and every alias.')}>
        <Input
          label={t('Domain')}
          value={csrDomain}
          onChange={(e) => setCsrDomain(e.target.value)}
          required
          error={fieldError('Domain')}
        />
        <InputArea
          label={t('Aliases')}
          error={fieldError('Aliases')}
          description={t('One domain per line.')}
          value={aliases}
          onValueChange={setAliases}
          rows={3}
        />
      </FormSection>

      <FormSection title={t('Subject')} description={t('Identifies who the certificate was issued to.')}>
        <Input
          label={
            <span>
              {t('Email')} <span className="font-normal text-kumo-subtle">({t('optional')})</span>
            </span>
          }
          type="email"
          placeholder="hostmaster@acme-bakery.com"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          error={fieldError('Email')}
        />
        <div className="grid gap-4 sm:grid-cols-2">
          <Input
            label={t('Country')}
            description={t('ISO 3166-1 alpha-2 two-letter code')}
            maxLength={2}
            value={country}
            onChange={(e) => setCountry(e.target.value.toUpperCase())}
            error={countryError ?? fieldError('Country')}
          />
          <Input
            label={t('State / province')}
            value={state}
            onChange={(e) => setState(e.target.value)}
            error={fieldError('State')}
          />
          <Input
            label={t('City / locality')}
            value={locality}
            onChange={(e) => setLocality(e.target.value)}
            error={fieldError('City')}
          />
          <Input
            label={t('Organization')}
            value={org}
            onChange={(e) => setOrg(e.target.value)}
            error={fieldError('Organization')}
          />
        </div>
      </FormSection>

      <FormSection title={t('Key')} description={t('ECDSA keys are smaller and faster; RSA works with older clients.')}>
        <Select
          label={t('Key type')}
          className="sm:w-72"
          value={keyAlgo}
          onValueChange={(v) => setKeyAlgo(v ?? keyAlgo)}
          items={{ ...KEY_TYPES, 'ecdsa-256': t(KEY_TYPES['ecdsa-256']) }}
        />
      </FormSection>
    </FormPage>
  )
}
