import { Banner, Checkbox, Empty, Input, LayerCard, LinkButton, Loader, Select, Text, useKumoToastManager } from '@cloudflare/kumo'
import { MagicWandIcon, WarningIcon } from '@phosphor-icons/react'
import { useEffect, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router'
import { ApiError } from '../../api/client'
import { apiErrorText } from '../../api/errors'
import type { WebAppField, WebAppForm } from '../../api/types.gen'
import { useWebApp, useWebAppJob, useWebMutation } from '../../api/web'
import { FormPage } from '../../components/FormPage'
import { FormSection } from '../../components/FormSection'
import { PageHeader } from '../../components/PageHeader'
import { PasswordInput } from '../../components/PasswordInput'
import { useT } from '../../i18n/I18nProvider'
import { interpolate } from '../../lib/interpolate'
import { DnsError, DnsLoading } from '../dns/shared'
import { AppMark } from './WebAppsPage'

const initialValue = (field: WebAppField) => {
  if (field.type === 'select') return field.value !== '' ? field.value : (field.options[0]?.value ?? '')
  return field.value
}

export function SetupWebAppPage() {
  const t = useT()
  const { domain: name = '', app: id = '' } = useParams()
  const query = useWebApp(name, id)
  const status = query.error instanceof ApiError ? query.error.status : 0

  const message = query.error instanceof ApiError ? query.error.message : ''
  // The server answers 404 with no message for an unknown installer; anything else has its own text
  if ((status === 404 && message === '') || status === 403) {
    const off = status === 403
    return (
      <div className="grid gap-6 pb-4">
        <PageHeader title={t('Quick install app')} back={{ href: `/web/${name}/apps`, label: t('Quick install app') }} />
        <LayerCard className="p-0">
          <Empty
            icon={<MagicWandIcon size={40} className="text-kumo-inactive" />}
            title={off ? t('Quick install is turned off') : t('App not found')}
            description={
              off
                ? t('The administrator has disabled the application installer on this server.')
                : t('There is no installer called %s.', id)
            }
            contents={<LinkButton href={`/web/${name}/apps`}>{t('Back to apps')}</LinkButton>}
            className="py-10"
          />
        </LayerCard>
      </div>
    )
  }
  if (query.error) return <DnsError error={query.error} />
  if (!query.data) return <DnsLoading />
  return <SetupForm key={query.data.app.id} domainName={name} form={query.data} />
}

function SetupForm({ domainName, form }: { domainName: string; form: WebAppForm }) {
  const t = useT()
  const toast = useKumoToastManager()
  const navigate = useNavigate()
  const m = useWebMutation()
  const { app, fields, phpVersions, database } = form
  const [values, setValues] = useState<Record<string, string>>(() =>
    Object.fromEntries(fields.map((f) => [f.key, initialValue(f)])),
  )
  const [php, setPhp] = useState(phpVersions[phpVersions.length - 1] ?? '')
  const [dbCreate, setDbCreate] = useState(true)
  const [dbHost, setDbHost] = useState(database?.hosts[0] ?? '')
  const [dbName, setDbName] = useState(database?.name ?? '')
  const [dbUser, setDbUser] = useState(database?.name ?? '')
  const [dbPassword, setDbPassword] = useState('')
  const [jobId, setJobId] = useState('')
  const [error, setError] = useState('')
  const [starting, setStarting] = useState(false)
  const job = useWebAppJob(domainName, jobId)
  const state = job.data?.state
  const running = starting || (jobId !== '' && (job.isPending || state === 'running'))
  const finished = useRef(false)

  const set = (key: string, value: string) => setValues((v) => ({ ...v, [key]: value }))
  const back = `/web/${domainName}/apps`

  // The job's outcome: done leaves for the apps list, failed or lost stays on the form
  const jobFailure = state === 'failed' ? job.data?.error : null
  const jobLost = jobId !== '' && job.isError
  useEffect(() => {
    if (state === 'done' && !finished.current) {
      finished.current = true
      toast.add({ title: t('%s installed successfully.', app.name), type: 'success' })
      navigate(back)
    }
  }, [state, toast, t, app.name, navigate, back])

  const jobError = jobFailure
    ? apiErrorText(new ApiError(0, jobFailure.code, jobFailure.message, jobFailure.args ?? []), t)
    : jobLost
      ? job.error instanceof ApiError && job.error.status === 404
        ? t('Installation status is unknown')
        : apiErrorText(job.error, t)
      : ''

  const onSubmit = async () => {
    setError('')
    setJobId('')
    setStarting(true)
    try {
      const ref = await m.installApp(domainName, app.id, {
        values,
        phpVersion: php,
        ...(database ? { database: { create: dbCreate, host: dbHost, name: dbName, user: dbUser, password: dbPassword } } : {}),
      })
      setJobId(ref.jobId)
    } catch (err) {
      setError(apiErrorText(err, t))
    } finally {
      setStarting(false)
    }
  }

  const shownError = error || jobError
  return (
    <FormPage
      title={t('Install %s', app.name)}
      description={
        <span className="flex flex-wrap items-center gap-2">
          <AppMark name={app.name} />
          <span>
            {interpolate(t('Version %s into %s'), app.version, <span className="font-mono text-[0.9em]">{domainName}</span>)}
          </span>
        </span>
      }
      back={{ href: back, label: t('Quick install app') }}
      submitLabel={t('Install')}
      onSubmit={onSubmit}
      submitDisabled={running}
    >
      {!form.docrootClean && (
        <Banner
          variant="alert"
          icon={<WarningIcon weight="fill" />}
          title={t('Data loss warning!')}
          description={
            <span>
              {t('Your web folder already has files uploaded to it. The installer will overwrite your files and/or the installation might fail.')}{' '}
              {t('Please make sure ~/web/%s/public_html is empty!', domainName)}
            </span>
          }
        />
      )}
      {shownError && (
        <Banner variant="error" icon={<WarningIcon weight="fill" />} title={shownError} className="whitespace-pre-line" />
      )}
      {running && (
        <div role="status" aria-label={t('Installation progress')} className="flex items-center gap-3 rounded-lg bg-kumo-recessed px-4 py-3">
          <Loader size="sm" />
          <Text>{job.data?.step ? t(job.data.step) : t('Starting the installer')}</Text>
        </div>
      )}

      {fields.length > 0 && (
        <FormSection title={t('Application')} description={t('Settings passed to the %s installer.', app.name)}>
          {fields.map((field) => {
            const value = values[field.key] ?? ''
            if (field.type === 'boolean') {
              return (
                <Checkbox
                  key={field.key}
                  label={field.label}
                  checked={value === 'true'}
                  onCheckedChange={(v) => set(field.key, v ? 'true' : '')}
                  disabled={running}
                />
              )
            }
            if (field.type === 'select') {
              return (
                <Select
                  key={field.key}
                  label={field.label}
                  value={value}
                  onValueChange={(v) => set(field.key, v ?? value)}
                  items={Object.fromEntries(field.options.map((o) => [o.value, o.label]))}
                  disabled={running}
                />
              )
            }
            if (field.type === 'password') {
              return (
                <PasswordInput
                  key={field.key}
                  label={field.label}
                  value={value}
                  placeholder={field.placeholder}
                  onChange={(v) => set(field.key, v)}
                  disabled={running}
                />
              )
            }
            return (
              <Input
                key={field.key}
                label={field.label}
                placeholder={field.placeholder}
                value={value}
                onChange={(e) => set(field.key, e.target.value)}
                disabled={running}
              />
            )
          })}
        </FormSection>
      )}

      <FormSection title="PHP" description={t('Versions supported by %s and installed on this server.', app.name)}>
        <Select
          label={t('PHP version')}
          className="sm:w-48"
          value={php}
          onValueChange={(v) => setPhp(v ?? php)}
          items={Object.fromEntries(phpVersions.map((v) => [v, `PHP ${v}`]))}
          disabled={running}
        />
      </FormSection>

      {database && (
        <FormSection title={t('Database')} description={t('A MySQL database for the application.')}>
          <Checkbox label={t('Create database')} checked={dbCreate} onCheckedChange={setDbCreate} disabled={running} />
          {!dbCreate && <Text variant="secondary">{t('The database below must already exist and be owned by you.')}</Text>}
          <Select
            label={t('Database host')}
            value={dbHost}
            onValueChange={(v) => setDbHost(v ?? dbHost)}
            items={Object.fromEntries(database.hosts.map((h) => [h, h]))}
            disabled={running}
          />
          <div className="grid gap-4 sm:grid-cols-2">
            <Input label={t('Database name')} value={dbName} onChange={(e) => setDbName(e.target.value)} disabled={running} />
            <Input label={t('Database user')} value={dbUser} onChange={(e) => setDbUser(e.target.value)} disabled={running} />
          </div>
          <PasswordInput
            label={t('Database password')}
            value={dbPassword}
            placeholder={t('auto')}
            onChange={setDbPassword}
            description={t('Leave empty to generate one automatically.')}
            disabled={running}
          />
        </FormSection>
      )}
    </FormPage>
  )
}
