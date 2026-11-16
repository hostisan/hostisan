import { Banner, Button, Collapsible, Input, LinkButton, useKumoToastManager } from '@cloudflare/kumo'
import { ArrowSquareOutIcon, WarningIcon } from '@phosphor-icons/react'
import { useState, type FormEvent, type ReactNode } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { phpMyAdminSsoUrl, useDatabase, useDbMutation, useDbOptions } from '../../api/db'
import { apiErrorText } from '../../api/errors'
import type { Database } from '../../api/hestia.gen'
import type { AddDBReply, DBOptions } from '../../api/types.gen'
import { SelectField } from '../../components/fields'
import { FormPage } from '../../components/FormPage'
import { FormSection } from '../../components/FormSection'
import { PasswordInput } from '../../components/PasswordInput'
import { StatusBadge } from '../../components/StatusBadge'
import { useT } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { useSessionUser } from '../../session/SessionProvider'
import { DnsError, DnsLoading } from '../dns/shared'
import { dbAdminName, dbTypeLabel } from './dbHelpers'
import { interpolate } from '../../lib/interpolate'

const back = { href: '/db', label: msgid('Databases') }

type DbType = 'mysql' | 'pgsql'

const hostsOf = (options: DBOptions, type: string) => options.hosts.filter((h) => h.type === type)

/** Parity 3: PostgreSQL offers UTF8 plus the host's own list; MySQL the static list. */
function charsetsFor(options: DBOptions, type: string, host: string): string[] {
  if (type !== 'pgsql') return options.mysqlCharsets
  const own = options.hosts.find((h) => h.type === type && h.host === host)?.charsets ?? []
  return [...new Set(['UTF8', ...own])]
}

const defaultCharsetOf = (options: DBOptions, type: string) =>
  options.defaultCharset[type] ?? (type === 'pgsql' ? 'UTF8' : 'utf8mb4')

/** `text` is a translated sentence whose %s is the full name, shown in mono. */
function PrefixHint({ prefix, value, text }: { prefix: string; value: string; text: string }) {
  if (!value.trim()) return null
  return interpolate(text, <span className="font-mono text-[0.9em]">{prefix + value.trim()}</span>)
}

function ErrorBanner({ text }: { text: string }) {
  if (!text) return null
  return <Banner variant="error" icon={<WarningIcon weight="fill" />} title={text} className="whitespace-pre-line" />
}

/** The create-success message: its first {...} links to the edit page, the second to the manager. */
function createdMessage(t: (s: string) => string, reply: AddDBReply): ReactNode {
  const parts = t('Database {%s} has been created successfully. / {Open %s}').split(/(\{[^}]*\})/)
  let braces = 0
  const out: ReactNode[] = []
  parts.forEach((part, i) => {
    const inner = /^\{([^}]*)\}$/.exec(part)?.[1]
    if (inner === undefined) {
      out.push(part)
      return
    }
    braces++
    if (braces === 1) {
      out.push(
        <Link key={i} to={`/db/${encodeURIComponent(reply.database)}`} className="underline">
          {inner.replace('%s', () => reply.database)}
        </Link>,
      )
    } else if (reply.dbAdminUrl) {
      out.push(
        <a key={i} href={reply.dbAdminUrl} target="_blank" rel="noopener noreferrer" className="underline">
          {inner.replace('%s', () => reply.dbAdmin)}
        </a>,
      )
    } else {
      // No manager link: drop the separator before it too.
      const last = out.length - 1
      if (typeof out[last] === 'string') out[last] = (out[last] as string).replace(/\s*\/\s*$/, '')
    }
  })
  return <>{out}</>
}

function Prefix({ prefix, children }: { prefix: string; children: (node: ReactNode) => ReactNode }) {
  const t = useT()
  return children(
    interpolate(
      t('Prefix %s will be automatically added to database name and database user'),
      <span className="font-mono text-[0.9em] text-kumo-default">{prefix}</span>,
    ),
  )
}

export function AddDatabasePage() {
  const t = useT()
  const options = useDbOptions()
  if (options.error || !options.data) {
    return (
      <FormPage title={t('Add database')} back={{ ...back, label: t(back.label) }} readOnly>
        {options.error ? <DnsError error={options.error} /> : <DnsLoading />}
      </FormPage>
    )
  }
  return <AddDatabaseForm options={options.data} />
}

function AddDatabaseForm({ options }: { options: DBOptions }) {
  const t = useT()
  const user = useSessionUser()
  const m = useDbMutation()
  const toast = useKumoToastManager()
  const navigate = useNavigate()
  const [accepted, setAccepted] = useState(!options.adminGate)
  const [database, setDatabase] = useState('')
  const [dbuser, setDbuser] = useState('')
  const [password, setPassword] = useState('')
  const [email, setEmail] = useState('')
  const [type, setType] = useState(options.types[0] ?? 'mysql')
  const [host, setHost] = useState(hostsOf(options, options.types[0] ?? 'mysql')[0]?.host ?? 'localhost')
  const [charset, setCharset] = useState(defaultCharsetOf(options, options.types[0] ?? 'mysql'))
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const hosts = hostsOf(options, type)
  const charsets = charsetsFor(options, type, host)

  const changeType = (next: string) => {
    setType(next)
    setHost(hostsOf(options, next)[0]?.host ?? 'localhost')
    setCharset(defaultCharsetOf(options, next))
  }
  const changeHost = (next: string) => {
    setHost(next)
    const available = charsetsFor(options, type, next)
    if (!available.includes(charset)) setCharset(defaultCharsetOf(options, type))
  }

  const onSubmit = async (e: FormEvent) => {
    void e
    setError('')
    setBusy(true)
    try {
      const reply = await m.addDb({ database, dbuser, password, type, host, charset, email })
      toast.add({ title: createdMessage(t, reply), type: 'success' })
      navigate('/db')
    } catch (err) {
      setError(apiErrorText(err, t))
      setBusy(false)
    }
  }

  return (
    <FormPage
      title={t('Add database')}
      back={{ ...back, label: t(back.label) }}
      submitLabel={t('Add database')}
      onSubmit={onSubmit}
      submitDisabled={busy}
      readOnly={user?.readOnly || !accepted}
    >
      {!accepted && (
        <div className="grid gap-4">
          <Banner
            variant="error"
            icon={<WarningIcon weight="fill" />}
            title={t('Consider using a standard user account')}
            description={t(
              'It is strongly advised to {create a standard user account} before adding %s to the server due to the increased privileges the admin account possesses and potential security risks.',
              t('a database'),
            ).replace(/[{}]/g, '')}
          />
          <div className="grid gap-2 sm:grid-cols-2">
            <LinkButton href="/users/new" variant="secondary" className="w-full justify-center">
              {t('Add user')}
            </LinkButton>
            <Button variant="destructive" className="w-full justify-center" onClick={() => setAccepted(true)}>
              {t('Continue')}
            </Button>
          </div>
        </div>
      )}

      {accepted && (
        <>
          <ErrorBanner text={error} />
          <Prefix prefix={options.prefix}>
            {(prefixText) => (
              <FormSection title={t('Database')} description={prefixText}>
                <Input
                  label={t('Database')}
                  value={database}
                  onChange={(e) => setDatabase(e.target.value)}
                  description={<PrefixHint prefix={options.prefix} value={database} text={t('Will be created as %s')} />}
                />
                <SelectField
                  label={t('Type')}
                  value={type}
                  onChange={changeType}
                  options={options.types.map((v) => ({ value: v, label: dbTypeLabel(v as DbType) }))}
                />
              </FormSection>
            )}
          </Prefix>

          <FormSection title={t('Database user')} description={t('The account your application uses to connect.')}>
            <Input
              label={t('Username')}
              labelTooltip={t('Maximum %s characters length, including prefix', options.maxUserLength)}
              description={
                <>
                  <PrefixHint prefix={options.prefix} value={dbuser} text={t('Will be created as %s')} />
                  <span className="block">
                    {t('Maximum %s characters length, including prefix', options.maxUserLength)}
                  </span>
                </>
              }
              value={dbuser}
              onChange={(e) => setDbuser(e.target.value)}
            />
            <PasswordInput value={password} onChange={setPassword} />
            <Input
              label={t('Email login credentials to:')}
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="ops@northwind-shop.io"
            />
          </FormSection>

          <FormSection
            title={t('Advanced options')}
            description={t('Pick a different database server or character set.')}
          >
            <Collapsible.Root open={advancedOpen} onOpenChange={setAdvancedOpen}>
              <Collapsible.DefaultTrigger>
                {advancedOpen ? t('Hide advanced options') : t('Show advanced options')}
              </Collapsible.DefaultTrigger>
              <Collapsible.DefaultPanel>
                <div className="grid gap-4 pt-2">
                  <SelectField
                    label={t('Host')}
                    value={host}
                    onChange={changeHost}
                    options={hosts.map((h) => ({ value: h.host, label: h.host }))}
                  />
                  <SelectField
                    label={t('Charset')}
                    value={charset}
                    onChange={setCharset}
                    options={charsets.map((c) => ({ value: c, label: c }))}
                  />
                </div>
              </Collapsible.DefaultPanel>
            </Collapsible.Root>
          </FormSection>
        </>
      )}
    </FormPage>
  )
}

export function EditDatabasePage() {
  const t = useT()
  const { database = '' } = useParams()
  const options = useDbOptions()
  const db = useDatabase(database)
  const failed = options.error ?? db.error
  if (failed || !options.data || !db.data) {
    return (
      <FormPage title={t('Edit database')} back={{ ...back, label: t(back.label) }} readOnly>
        {failed ? <DnsError error={failed} /> : <DnsLoading />}
      </FormPage>
    )
  }
  return <EditDatabaseForm name={database} db={db.data} options={options.data} />
}

function EditDatabaseForm({ name, db, options }: { name: string; db: Database; options: DBOptions }) {
  const t = useT()
  const user = useSessionUser()
  const m = useDbMutation()
  const toast = useKumoToastManager()
  const navigate = useNavigate()
  const [dbuser, setDbuser] = useState(
    db.DBUSER.startsWith(options.prefix) ? db.DBUSER.slice(options.prefix.length) : db.DBUSER,
  )
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const suspended = db.SUSPENDED === 'yes'
  const readOnly = Boolean(user?.readOnly) || suspended
  const limit = t('Maximum %s characters length, including prefix', options.maxUserLength)

  const onSubmit = async (e: FormEvent) => {
    void e
    setError('')
    setBusy(true)
    try {
      // The server leaves the password alone when it is absent.
      await m.editDb(name, { dbuser, ...(password ? { password } : {}) } as { dbuser: string; password: string })
      toast.add({ title: t('Changes have been saved.'), type: 'success' })
      navigate('/db')
    } catch (err) {
      setError(apiErrorText(err, t))
      setBusy(false)
    }
  }

  return (
    <FormPage
      title={t('Edit database')}
      description={
        <span className="flex flex-wrap items-center gap-2">
          <span className="font-mono text-[0.9em]">{db.DATABASE}</span>
          <StatusBadge suspended={suspended} />
        </span>
      }
      back={{ ...back, label: t(back.label) }}
      onSubmit={onSubmit}
      submitDisabled={busy}
      readOnly={readOnly}
      actions={
        db.TYPE === 'mysql' && options.ssoEnabled && !user?.readOnly ? (
          <LinkButton href={phpMyAdminSsoUrl(name)} target="_blank" rel="noopener noreferrer" icon={ArrowSquareOutIcon}>
            {t('Open in %s', dbAdminName(db.TYPE))}
          </LinkButton>
        ) : undefined
      }
    >
      {suspended && (
        <Banner
          variant="alert"
          icon={<WarningIcon weight="fill" />}
          title={t('This database is suspended')}
          description={t('Unsuspend it from the databases list before changing its user or password.')}
        />
      )}
      <ErrorBanner text={error} />

      <FormSection
        title={t('Database')}
        description={t("The name, type, host and charset can't be changed after creation.")}
      >
        <Input label={t('Database')} value={db.DATABASE} disabled />
        <div className="grid gap-4 sm:grid-cols-3">
          <Input label={t('Type')} value={dbTypeLabel(db.TYPE)} disabled />
          <Input label={t('Host')} value={db.HOST} disabled />
          <Input label={t('Charset')} value={db.CHARSET} disabled />
        </div>
      </FormSection>

      <FormSection
        title={t('Database user')}
        description={interpolate(
          t('Prefix %s is added automatically. Leave the password empty to keep the current one.'),
          <span className="font-mono text-[0.9em] text-kumo-default">{options.prefix}</span>,
        )}
      >
        <Input
          label={t('Username')}
          labelTooltip={limit}
          value={dbuser}
          onChange={(e) => setDbuser(e.target.value)}
          description={<PrefixHint prefix={options.prefix} value={dbuser} text={t('Saved as %s')} />}
          disabled={readOnly}
        />
        <PasswordInput value={password} onChange={setPassword} disabled={readOnly} />
      </FormSection>
    </FormPage>
  )
}
