import { Banner, Checkbox, Input, InputArea, LinkButton, Link, Switch, Tabs, Text } from '@cloudflare/kumo'
import {
  CheckCircleIcon,
  ClockIcon,
  InfoIcon,
  NetworkIcon,
  PaintBrushIcon,
  PencilSimpleIcon,
  UserIcon,
  WarningCircleIcon,
} from '@phosphor-icons/react'
import { useRef, useState, type FormEvent, type ReactNode } from 'react'
import { ApiError } from '../../api/client'
import { apiErrorText } from '../../api/errors'
import { useServerMutation, useSettings } from '../../api/server'
import type { JobStatus, SettingsRemote, SettingsRequest, SettingsResult, SettingsView } from '../../api/types.gen'
import { ComboboxField, Optional, SelectField } from '../../components/fields'
import { FormPage } from '../../components/FormPage'
import type { FormGuard } from '../../unsaved/types'
import { FormSection } from '../../components/FormSection'
import { PasswordInput } from '../../components/PasswordInput'
import { useT } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { useSession } from '../../session/SessionProvider'
import { scrollContentToTop } from '../../lib/scroll'
import { DnsError, DnsLoading } from '../dns/shared'
import { stepText } from './format'
import { ErrorNote, Fact, JobPanel, ToggleField } from './parts'

const yesNo = [
  { value: 'yes', label: msgid('Yes') },
  { value: 'no', label: msgid('No') },
]

/** Option labels marked with msgid(), translated for display. */
function translateOptions(options: { value: string; label: string }[], t: (m: string) => string) {
  return options.map((o) => ({ ...o, label: t(o.label) }))
}

// Everything the form edits, seeded from the stored settings. Secrets start empty and are
// sent only when typed: the API never lists them, and an empty one keeps the stored value.
type Form = {
  hostname: string
  timezone: string
  theme: string
  themeForAll: boolean
  language: string
  languageForAll: boolean
  debugMode: boolean
  previewFeatures: boolean
  upgradeSendEmail: boolean
  upgradeSendEmailLog: boolean
  php: string[]
  defaultPhp: string
  webmailAlias: string
  relay: boolean
  relayHost: string
  relayPort: string
  relayUser: string
  relayPassword: string
  pmaAlias: string
  pgaAlias: string
  pmaSso: boolean
  pmaRestrict: boolean
  mysqlPassword: string
  backupLocal: boolean
  backupMode: string
  backupLevel: string
  remote: SettingsRemote
  backupPassword: string
  b2Key: string
  incremental: boolean
  restic: SettingsView['backup']['restic']
  sslCrt: string
  sslKey: string
  security: SettingsView['security']
  protection: SettingsView['protection']
  policies: SettingsView['policies']
  plugins: SettingsView['plugins']
}

const seed = (s: SettingsView): Form => ({
  hostname: s.hostname,
  timezone: s.timezone,
  theme: s.theme,
  themeForAll: s.themeForAll,
  language: s.language,
  languageForAll: false,
  debugMode: s.debugMode,
  previewFeatures: s.previewFeatures,
  upgradeSendEmail: s.upgradeSendEmail,
  upgradeSendEmailLog: s.upgradeSendEmailLog,
  php: s.phpVersions.filter((v) => v.installed).map((v) => v.version),
  defaultPhp: s.defaultPhp,
  webmailAlias: s.mail.webmailAlias,
  relay: s.mail.relay,
  relayHost: s.mail.relayHost,
  relayPort: s.mail.relayPort,
  relayUser: s.mail.relayUser,
  relayPassword: '',
  pmaAlias: s.db.pmaAlias,
  pgaAlias: s.db.pgaAlias,
  pmaSso: s.db.pmaSso,
  pmaRestrict: s.db.pmaRestrict,
  mysqlPassword: '',
  backupLocal: s.backup.local,
  backupMode: s.backup.mode,
  backupLevel: s.backup.level,
  remote: s.backup.remote,
  backupPassword: '',
  b2Key: '',
  incremental: s.backup.incremental,
  restic: s.backup.restic,
  sslCrt: s.ssl.crt,
  sslKey: '',
  security: s.security,
  protection: s.protection,
  policies: s.policies,
  plugins: s.plugins,
})

/** The request for the sections this server has; a missing section sends none of its fields. */
function buildRequest(s: SettingsView, f: Form): SettingsRequest {
  const req: SettingsRequest = {
    hostname: f.hostname.trim(),
    timezone: f.timezone,
    theme: f.theme,
    themeForAll: f.themeForAll,
    language: f.language,
    ...(f.language !== s.language ? { languageForAll: f.languageForAll } : {}),
    debugMode: f.debugMode,
    previewFeatures: f.previewFeatures,
    upgradeSendEmail: f.upgradeSendEmail,
    upgradeSendEmailLog: f.upgradeSendEmailLog,
    backupLocal: f.backupLocal,
    backupMode: f.backupMode,
    backupLevel: f.backupLevel,
    backupRemote: f.remote,
    ...(f.backupPassword ? { backupPassword: f.backupPassword } : {}),
    ...(f.b2Key ? { b2Key: f.b2Key } : {}),
    incremental: f.incremental,
    restic: f.restic,
    sslCrt: f.sslCrt,
    ...(f.sslKey ? { sslKey: f.sslKey } : {}),
    apiSystem: f.security.apiSystem,
    legacyApi: f.security.legacyApi,
    apiAllowedIp: f.security.apiAllowedIp,
    loginStyle: f.security.loginStyle,
    passwordReset: f.security.passwordReset,
    sessionTimeout: f.security.sessionTimeout.trim(),
    csrfStrictness: f.security.csrfStrictness,
    // The suspended-user policy is only offered with preview features on; otherwise echo the stored value
    policies: { ...f.policies, viewSuspended: f.previewFeatures ? f.policies.viewSuspended : s.policies.viewSuspended },
    plugins: f.plugins,
  }
  if (s.webBackend === 'php-fpm') {
    req.phpVersions = [...f.php].sort((a, b) => a.localeCompare(b, undefined, { numeric: true }))
    if (f.defaultPhp) req.defaultPhp = f.defaultPhp
  }
  if (s.mail.system) {
    req.smtpRelay = f.relay
    req.smtpRelayHost = f.relayHost.trim()
    req.smtpRelayPort = f.relayPort.trim()
    req.smtpRelayUser = f.relayUser.trim()
    if (f.relayPassword) req.smtpRelayPassword = f.relayPassword
    if (s.mail.webmailSystem) req.webmailAlias = f.webmailAlias.trim()
  }
  if (s.db.mysql) {
    req.pmaAlias = f.pmaAlias.trim()
    req.pmaSso = f.pmaSso
    req.pmaRestrict = f.pmaRestrict
  }
  if (s.db.pgsql) req.pgaAlias = f.pgaAlias.trim()
  if (s.db.mysqlLocal && f.mysqlPassword) req.mysqlPassword = f.mysqlPassword
  if (s.canEditProtection) {
    req.protectedAdmin = f.protection.protectedAdmin
    req.hideAdmin = f.protection.hideAdmin
    req.hideServices = f.protection.hideServices
  }
  return req
}

function languageName(code: string): string {
  try {
    return new Intl.DisplayNames(['en'], { type: 'language' }).of(code) ?? code
  } catch {
    return code
  }
}

function CheckField({
  label,
  checked,
  onChange,
  hint,
}: {
  label: ReactNode
  checked: boolean
  onChange: (v: boolean) => void
  hint?: ReactNode
}) {
  return (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
      <Checkbox label={label} checked={checked} onCheckedChange={(v) => onChange(v === true)} />
      {hint}
    </div>
  )
}

function SubHeading({ children }: { children: ReactNode }) {
  return (
    <Text bold as="h3">
      {children}
    </Text>
  )
}

/** A fact naming a service (or saying it is there), with a link to its config editor. */
function ServiceFact({ label, service, children }: { label: string; service: string; children?: ReactNode }) {
  const t = useT()
  return (
    <Fact label={label}>
      <span className="inline-flex items-center gap-1.5">
        {children ?? service}
        <Link href={`/server/services/${service}`} aria-label={t('Configure %s', service)} variant="plain">
          <PencilSimpleIcon />
        </Link>
      </span>
    </Fact>
  )
}

type Props = { s: SettingsView; f: Form; set: (patch: Partial<Form>) => void }

function GeneralTab({ s, f, set }: Props) {
  const t = useT()
  return (
    <FormSection title={t('Basic options')} description={t('Server identity and the defaults new users start with.')}>
      <Input label={t('Hostname')} value={f.hostname} onChange={(e) => set({ hostname: e.target.value })} />
      <ComboboxField
        label={t('Time zone')}
        placeholder={t('Search time zones')}
        value={f.timezone}
        onChange={(timezone) => set({ timezone })}
        options={s.options.timezones.map((tz) => ({ value: tz, label: tz.replaceAll('_', ' ') }))}
      />
      <div className="grid gap-2">
        <SelectField
          label={t('Theme')}
          value={f.theme}
          onChange={(theme) => set({ theme })}
          options={s.options.themes.map((th) => ({ value: th, label: th.charAt(0).toUpperCase() + th.slice(1) }))}
        />
        <CheckField
          label={t('Set as selected theme for all users')}
          checked={f.themeForAll}
          onChange={(themeForAll) => set({ themeForAll })}
        />
      </div>
      <div className="grid gap-2">
        <ComboboxField
          label={t('Default language')}
          placeholder={t('Search languages')}
          value={f.language}
          onChange={(language) => set({ language })}
          options={s.options.languages.map((code) => ({ value: code, label: languageName(code) }))}
        />
        <CheckField
          label={t('Set as default language for all users')}
          checked={f.languageForAll}
          onChange={(languageForAll) => set({ languageForAll })}
        />
      </div>
      <CheckField label={t('Enable debug mode')} checked={f.debugMode} onChange={(debugMode) => set({ debugMode })} />
    </FormSection>
  )
}

function UpdatesTab({ s, f, set }: Props) {
  const t = useT()
  return (
    <FormSection title={t('Updates')} description={t('Release channel and how you hear about installed updates.')}>
      <Fact label={t('Version')}>{s.version}</Fact>
      {s.releaseBranch !== 'release' && <Fact label={t('Release')}>{s.releaseBranch}</Fact>}
      <div className="grid gap-2">
        <Text bold as="span">
          {t('Options')}
        </Text>
        <CheckField
          label={t('Enable preview features')}
          checked={f.previewFeatures}
          onChange={(previewFeatures) => set({ previewFeatures })}
          hint={
            <Link href="/server/preview" variant="inline">
              {t('View preview features')}
            </Link>
          }
        />
        <CheckField
          label={t('Send email notification when an update has been installed')}
          checked={f.upgradeSendEmail}
          onChange={(upgradeSendEmail) => set({ upgradeSendEmail })}
        />
        <CheckField
          label={t('Send update installation log by email')}
          checked={f.upgradeSendEmailLog}
          onChange={(upgradeSendEmailLog) => set({ upgradeSendEmailLog })}
        />
      </div>
    </FormSection>
  )
}

function WebTab({ s, f, set }: Props) {
  const php = s.webBackend === 'php-fpm'
  const installed = s.phpVersions.filter((v) => v.installed).map((v) => v.version)
  const t = useT()
  return (
    <>
      <FormSection title={t('Web server')} description={t('The web stack installed on this server.')}>
        {s.proxySystem && <ServiceFact label={t('Proxy server')} service={s.proxySystem} />}
        {s.webSystem && <ServiceFact label={t('Web server')} service={s.webSystem} />}
        {s.webBackend && (
          <Fact label={t('PHP interpreter')}>
            <span className="inline-flex items-center gap-1.5">
              {s.webBackend}
              {s.defaultPhp && (
                <Link
                  href={`/server/services/php${s.defaultPhp}-fpm`}
                  aria-label={t('Configure %s', 'PHP')}
                  variant="plain"
                >
                  <PencilSimpleIcon />
                </Link>
              )}
            </span>
          </Fact>
        )}
      </FormSection>
      {php && (
        <FormSection
          title={t('Enabled PHP versions')}
          description={t("Install or remove PHP-FPM versions. Versions in use by a web domain can't be removed.")}
        >
          <Banner variant="default">
            {t(
              'It may take a few minutes to save your changes. Please wait until the process has completed and do not refresh the page.',
            )}
          </Banner>
          <div className="grid gap-3 sm:grid-cols-2">
            {s.phpVersions.map((v) => (
              <div key={v.version} className="grid content-start gap-1">
                <Checkbox
                  label={`PHP ${v.version}`}
                  checked={f.php.includes(v.version)}
                  disabled={v.protected}
                  onCheckedChange={(on) =>
                    set({ php: on === true ? [...f.php, v.version] : f.php.filter((x) => x !== v.version) })
                  }
                />
                {v.usedBy.map((u) => (
                  <div key={`${u.user}/${u.domain}`} className="flex items-center gap-1.5 pl-6">
                    <span className="flex h-lh items-center">
                      <UserIcon className="shrink-0 text-kumo-subtle" />
                    </span>
                    <Text variant="secondary" size="sm" as="span" DANGEROUS_className="truncate">
                      {t('Used by %s (%s)', u.domain, u.user)}
                    </Text>
                  </div>
                ))}
                {v.protected && v.usedBy.length === 0 && (
                  <Text variant="secondary" size="sm" as="span" DANGEROUS_className="pl-6">
                    {t('System default version')}
                  </Text>
                )}
              </div>
            ))}
          </div>
          <SelectField
            label={t('System PHP version')}
            value={f.defaultPhp}
            onChange={(defaultPhp) => set({ defaultPhp })}
            options={installed.map((v) => ({ value: v, label: `PHP ${v}` }))}
            className="w-full sm:max-w-xs"
          />
        </FormSection>
      )}
    </>
  )
}

function DnsTab({ s }: Props) {
  const t = useT()
  return (
    <FormSection title={t('DNS server')} description={t('Name server software and cluster replication.')}>
      <ServiceFact label={t('DNS server')} service={s.dnsSystem} />
      <Fact label={t('DNS cluster')}>{s.dnsCluster.length > 0 ? t('Yes') : t('No')}</Fact>
      {s.dnsCluster.map((h, i) => (
        <Input key={h} label={t('Host #%s', i + 1)} value={h} disabled />
      ))}
    </FormSection>
  )
}

function MailTab({ s, f, set }: Props) {
  const t = useT()
  const touched =
    f.relayHost !== s.mail.relayHost || f.relayPort !== s.mail.relayPort || f.relayUser !== s.mail.relayUser
  return (
    <>
      <FormSection title={t('Mail server')} description={t('Mail software, webmail address and outbound relay.')}>
        <ServiceFact label={t('Mail server')} service={s.mail.system} />
        {s.mail.antivirus && <ServiceFact label={t('Anti-virus')} service={s.mail.antivirus} />}
        {s.mail.antispam && <ServiceFact label={t('Spam filter')} service={s.mail.antispam} />}
        {s.mail.webmailSystem && (
          <Input
            label={t('Webmail alias')}
            value={f.webmailAlias}
            onChange={(e) => set({ webmailAlias: e.target.value })}
            description={<span className="font-mono text-[0.9em]">{f.webmailAlias || 'webmail'}.example.com</span>}
          />
        )}
      </FormSection>
      <FormSection
        title={t('Global SMTP relay')}
        description={t('Send all outgoing mail from this server through another SMTP server.')}
        aside={<Switch label={t('Global SMTP relay')} checked={f.relay} onCheckedChange={(relay) => set({ relay })} />}
      >
        {f.relay ? (
          <div className="grid gap-4 sm:grid-cols-[1fr_8rem]">
            <Input label={t('Host')} value={f.relayHost} onChange={(e) => set({ relayHost: e.target.value })} />
            <Input
              label={t('Port')}
              value={f.relayPort}
              onChange={(e) => set({ relayPort: e.target.value })}
              inputMode="numeric"
              className="w-full min-w-0"
            />
            <Input
              label={t('Username')}
              value={f.relayUser}
              onChange={(e) => set({ relayUser: e.target.value })}
              autoComplete="off"
            />
            <div className="sm:col-span-2">
              <PasswordInput
                label={t('SMTP relay password')}
                isNew={false}
                value={f.relayPassword}
                onChange={(relayPassword) => set({ relayPassword })}
                autoComplete="off"
                description={
                  s.mail.relayUser || touched
                    ? t(
                        'Type the password again whenever you change the relay settings. Leave empty to keep the stored one.',
                      )
                    : undefined
                }
              />
            </div>
          </div>
        ) : (
          <Text variant="secondary">{t('Mail is delivered directly from this server.')}</Text>
        )}
      </FormSection>
    </>
  )
}

function DatabasesTab({ s, f, set }: Props) {
  const t = useT()
  const mysql = s.db.hosts.filter((h) => h.type === 'mysql')
  const pgsql = s.db.hosts.filter((h) => h.type === 'pgsql')
  return (
    <>
      {s.db.mysql && (
        <FormSection
          title="MySQL"
          description={t('phpMyAdmin access and the database hosts users can create databases on.')}
        >
          <ServiceFact label={t('MySQL support')} service="mysql">
            {t('Yes')}
          </ServiceFact>
          <Input label={t('phpMyAdmin alias')} value={f.pmaAlias} onChange={(e) => set({ pmaAlias: e.target.value })} />
          <ToggleField
            label={t('phpMyAdmin single sign-on')}
            checked={f.pmaSso}
            // Without sign-on there is nothing to restrict to, so turning it off lifts the restriction too
            onCheckedChange={(pmaSso) => set({ pmaSso, pmaRestrict: pmaSso ? f.pmaRestrict : false })}
          />
          <ToggleField
            label={t('Restrict phpMyAdmin to panel login')}
            description={t(
              "Blocks phpMyAdmin's own login form; only reachable via the phpMyAdmin link inside the panel (requires SSO above).",
            )}
            checked={f.pmaRestrict}
            disabled={!f.pmaSso}
            onCheckedChange={(pmaRestrict) => set({ pmaRestrict })}
          />
          {s.db.mysqlLocal && (
            <PasswordInput
              label={t('MySQL root password')}
              isNew={false}
              value={f.mysqlPassword}
              onChange={(mysqlPassword) => set({ mysqlPassword })}
              autoComplete="off"
              description={t('Leave empty to keep the current password.')}
            />
          )}
          {mysql.map((h, i) => (
            <div key={h.host} className="grid gap-4 border-t border-kumo-line pt-4 sm:grid-cols-3">
              <Input label={t('Host #%s', i + 1)} value={h.host} disabled />
              <Input label={t('Maximum number of databases')} value={h.maxDb} disabled />
              <Input label={t('Current number of databases')} value={h.current} disabled />
            </div>
          ))}
        </FormSection>
      )}
      {s.db.pgsql && (
        <FormSection title="PostgreSQL" description={t('phpPgAdmin access and PostgreSQL hosts.')}>
          <ServiceFact label={t('PostgreSQL support')} service="postgresql">
            {t('Yes')}
          </ServiceFact>
          <Input label={t('phpPgAdmin alias')} value={f.pgaAlias} onChange={(e) => set({ pgaAlias: e.target.value })} />
          {pgsql.map((h, i) => (
            <div key={h.host} className="grid gap-4 border-t border-kumo-line pt-4 sm:grid-cols-3">
              <Input label={t('Host #%s', i + 1)} value={h.host} disabled />
              <Input label={t('Maximum number of databases')} value={h.maxDb} disabled />
              <Input label={t('Current number of databases')} value={h.current} disabled />
            </div>
          ))}
        </FormSection>
      )}
    </>
  )
}

const REMOTE_TYPES = [
  { value: 'ftp', label: 'FTP' },
  { value: 'sftp', label: 'SFTP' },
  { value: 'b2', label: 'Backblaze' },
  { value: 'rclone', label: 'Rclone' },
]

function BackupsTab({ s, f, set }: Props) {
  const t = useT()
  const r = f.remote
  const setRemote = (patch: Partial<SettingsRemote>) => set({ remote: { ...r, ...patch } })
  const levels = Array.from({ length: f.backupMode === 'zstd' ? 19 : 9 }, (_, i) => String(i + 1))
  const remoteOn = r.type !== ''
  return (
    <>
      <FormSection title={t('Local backup')} description={t('Where and how user backups are written on this server.')}>
        <SelectField
          label={t('Local backup')}
          value={f.backupLocal ? 'yes' : 'no'}
          onChange={(v) => set({ backupLocal: v === 'yes' })}
          options={translateOptions(yesNo, t)}
        />
        <div className="grid gap-4 sm:grid-cols-2">
          <SelectField
            label={t('Compression')}
            value={f.backupMode}
            onChange={(backupMode) =>
              set({ backupMode, backupLevel: backupMode === 'gzip' && Number(f.backupLevel) > 9 ? '9' : f.backupLevel })
            }
            options={[
              { value: 'gzip', label: 'gzip' },
              { value: 'zstd', label: 'zstd' },
            ]}
          />
          <SelectField
            label={t('Compression level')}
            value={f.backupLevel}
            onChange={(backupLevel) => set({ backupLevel })}
            options={levels.map((l) => ({ value: l, label: Number(l) > 9 ? `${l} (${t('zstd only')})` : l }))}
          />
        </div>
        <Input label={t('Directory')} value={s.backup.directory} disabled />
      </FormSection>
      <FormSection
        title={t('Remote backup')}
        description={t("Copy every backup to a remote host after it's created.")}
        aside={
          <Switch
            label={t('Remote backup')}
            checked={remoteOn}
            onCheckedChange={(on) =>
              setRemote({ type: on ? s.backup.remote.type || 'sftp' : '', ...(on && !r.port ? { port: '22' } : {}) })
            }
          />
        }
      >
        {remoteOn ? (
          <>
            <SelectField
              label={t('Protocol')}
              value={r.type}
              onChange={(type) =>
                setRemote({
                  type,
                  ...(type === 'sftp' && !r.port ? { port: '22' } : type === 'ftp' && !r.port ? { port: '21' } : {}),
                })
              }
              options={REMOTE_TYPES}
            />
            {(r.type === 'ftp' || r.type === 'sftp') && (
              <div className="grid gap-4 sm:grid-cols-[1fr_8rem]">
                <Input label={t('Host')} value={r.host} onChange={(e) => setRemote({ host: e.target.value })} />
                <Input
                  label={t('Port')}
                  value={r.port}
                  onChange={(e) => setRemote({ port: e.target.value })}
                  inputMode="numeric"
                  className="w-full min-w-0"
                />
                <Input
                  label={t('Username')}
                  value={r.username}
                  onChange={(e) => setRemote({ username: e.target.value })}
                />
                <div className="grid gap-4 sm:col-span-2">
                  <PasswordInput
                    label={t('Remote backup password')}
                    isNew={false}
                    value={f.backupPassword}
                    onChange={(backupPassword) => set({ backupPassword })}
                    autoComplete="off"
                    description={[
                      t(
                        'Type the password again when you change the host, username, port or directory. Leave empty to keep the stored one.',
                      ),
                      // The core takes an SFTP key only as a key file on the server (D8)
                      r.type === 'sftp' &&
                        t(
                          "SFTP keys can't be pasted here. Copy the key file to the server over SSH and add the backup host with v-add-backup-host.",
                        ),
                    ]
                      .filter(Boolean)
                      .join(' ')}
                  />
                  <Input label={t('Directory')} value={r.path} onChange={(e) => setRemote({ path: e.target.value })} />
                </div>
              </div>
            )}
            {r.type === 'b2' && (
              <>
                <Input
                  label={t('Bucket')}
                  value={r.bucket}
                  onChange={(e) => setRemote({ bucket: e.target.value })}
                  placeholder="server1-backups"
                />
                <Input label={t('Key ID')} value={r.keyId} onChange={(e) => setRemote({ keyId: e.target.value })} />
                <PasswordInput
                  label={t('Application key')}
                  isNew={false}
                  value={f.b2Key}
                  onChange={(b2Key) => set({ b2Key })}
                  autoComplete="off"
                  description={t(
                    'Type the key again when you change the bucket or key ID. Leave empty to keep the stored one.',
                  )}
                />
              </>
            )}
            {r.type === 'rclone' && (
              <>
                <Input
                  label={t('Host')}
                  value={r.host}
                  onChange={(e) => setRemote({ host: e.target.value })}
                  description={t('The remote name from rclone config.')}
                  placeholder="r2"
                />
                <Input
                  label={t('Directory')}
                  value={r.path}
                  onChange={(e) => setRemote({ path: e.target.value })}
                  placeholder="/server1"
                />
              </>
            )}
          </>
        ) : (
          <Text variant="secondary">{t('Backups are kept on this server only.')}</Text>
        )}
      </FormSection>
      <FormSection
        title={t('Incremental backups')}
        description={t('Snapshot-based backups with restic, in addition to the full backups above.')}
        aside={
          <Switch
            label={t('Incremental backups')}
            checked={f.incremental}
            onCheckedChange={(incremental) => set({ incremental })}
          />
        }
      >
        {f.incremental ? (
          <>
            <Input
              label={t('Repository')}
              value={f.restic.repo}
              onChange={(e) => set({ restic: { ...f.restic, repo: e.target.value } })}
            />
            <Input
              label={t('Snapshots')}
              value={f.restic.snapshots}
              onChange={(e) => set({ restic: { ...f.restic, snapshots: e.target.value } })}
              inputMode="numeric"
              className="w-full min-w-0"
            />
            <div className="grid gap-4 sm:grid-cols-2">
              {(
                [
                  ['keepDaily', msgid('Keep last daily backups')],
                  ['keepWeekly', msgid('Keep last weekly backups')],
                  ['keepMonthly', msgid('Keep last monthly backups')],
                  ['keepYearly', msgid('Keep last yearly backups')],
                ] as const
              ).map(([key, label]) => (
                <Input
                  key={key}
                  label={t(label)}
                  value={f.restic[key]}
                  onChange={(e) => set({ restic: { ...f.restic, [key]: e.target.value } })}
                  inputMode="numeric"
                  className="w-full min-w-0"
                />
              ))}
            </div>
          </>
        ) : (
          <Text variant="secondary">{t('Incremental backups are turned off.')}</Text>
        )}
      </FormSection>
    </>
  )
}

function SslTab({ s, f, set }: Props) {
  const ssl = s.ssl
  const t = useT()
  return (
    <FormSection title={t('SSL')} description={t('The certificate used by the control panel, mail and FTP services.')}>
      <InputArea
        label={t('SSL certificate')}
        value={f.sslCrt}
        onValueChange={(sslCrt) => set({ sslCrt })}
        rows={7}
        spellCheck={false}
        className="font-mono text-[13px]"
      />
      <InputArea
        label={
          <>
            {t('SSL private key')} <Optional>{t('Leave empty to keep the current key')}</Optional>
          </>
        }
        value={f.sslKey}
        onValueChange={(sslKey) => set({ sslKey })}
        rows={5}
        spellCheck={false}
        className="font-mono text-[13px]"
      />
      <dl className="grid gap-2 border-t border-kumo-line pt-4">
        <Fact label={t('Issued to')}>{ssl.subject}</Fact>
        {ssl.aliases && <Fact label={t('Alternate')}>{ssl.aliases}</Fact>}
        <Fact label={t('Not before')}>{ssl.notBefore}</Fact>
        <Fact label={t('Not after')}>{ssl.notAfter}</Fact>
        <Fact label={t('Signature')}>{ssl.signature}</Fact>
        <Fact label={t('Key size')}>{ssl.pubKey}</Fact>
        <Fact label={t('Issued by')}>{ssl.issuer}</Fact>
      </dl>
    </FormSection>
  )
}

function SecurityTab({ s, f, set }: Props) {
  const sec = f.security
  const setSec = (patch: Partial<SettingsView['security']>) => set({ security: { ...sec, ...patch } })
  const prot = f.protection
  const setProt = (patch: Partial<SettingsView['protection']>) => set({ protection: { ...prot, ...patch } })
  const t = useT()
  return (
    <>
      <FormSection title={t('API')} description={t('Programmatic access to the panel.')}>
        <SelectField
          label={t('Enable API access')}
          value={sec.apiSystem}
          onChange={(apiSystem) => setSec({ apiSystem })}
          options={[
            { value: '0', label: t('Disabled') },
            { value: '1', label: t('Enabled for admin') },
            { value: '2', label: t('Enabled for all users') },
          ]}
        />
        <SelectField
          label={t('Enable legacy API access')}
          value={sec.legacyApi ? 'yes' : 'no'}
          onChange={(v) => setSec({ legacyApi: v === 'yes' })}
          options={translateOptions(yesNo, t)}
        />
        {(sec.legacyApi || Number(sec.apiSystem) > 0) && (
          <InputArea
            label={t('Allowed IP addresses for API')}
            description={t('One IP address per line.')}
            value={sec.apiAllowedIp}
            onValueChange={(apiAllowedIp) => setSec({ apiAllowedIp })}
            rows={4}
            className="font-mono text-[13px]"
          />
        )}
      </FormSection>
      <FormSection title={t('Login')} description={t('How people sign in to the panel.')}>
        <SelectField
          label={t('Login screen style')}
          value={sec.loginStyle}
          onChange={(loginStyle) => setSec({ loginStyle })}
          options={[
            { value: 'default', label: t('Default') },
            { value: 'old', label: t('Old style') },
          ]}
        />
        <SelectField
          label={t('Allow users to reset their passwords')}
          value={sec.passwordReset ? 'yes' : 'no'}
          onChange={(v) => setSec({ passwordReset: v === 'yes' })}
          options={translateOptions(yesNo, t)}
        />
        <Input
          // Hestia's label: "Inactive session timeout (Minutes)", two msgids
          label={`${t('Inactive session timeout')} (${t('minutes')})`}
          value={sec.sessionTimeout}
          onChange={(e) => setSec({ sessionTimeout: e.target.value })}
          inputMode="numeric"
          className="max-w-40"
        />
        <SelectField
          label={t('Prevent CSRF')}
          value={sec.csrfStrictness}
          onChange={(csrfStrictness) => setSec({ csrfStrictness })}
          options={[
            { value: '0', label: t('Disabled') },
            { value: '1', label: t('Enabled') },
            { value: '2', label: t('Strict') },
          ]}
        />
      </FormSection>
      {s.canEditProtection && (
        <FormSection
          title={t('System protection')}
          description={t(
            'Limits on what other administrators can see and change. Only the system administrator sees these.',
          )}
        >
          <SubHeading>{t('System administrator account')}</SubHeading>
          <ToggleField
            label={t('Restrict access to read-only for other administrators')}
            checked={prot.protectedAdmin}
            onCheckedChange={(protectedAdmin) => setProt({ protectedAdmin })}
          />
          <ToggleField
            label={t('Hide account from other administrators')}
            checked={prot.hideAdmin}
            onCheckedChange={(hideAdmin) => setProt({ hideAdmin })}
          />
          <ToggleField
            label={t('Do not allow other administrators to access server settings')}
            checked={prot.hideServices}
            onCheckedChange={(hideServices) => setProt({ hideServices })}
          />
        </FormSection>
      )}
    </>
  )
}

function PoliciesTab({ f, set }: Props) {
  const p = f.policies
  const setP = (patch: Partial<typeof p>) => set({ policies: { ...p, ...patch } })
  const t = useT()
  return (
    <>
      <FormSection title={t('Users')} description={t('What regular users are allowed to do.')}>
        <ToggleField
          label={t('Allow users to edit their account details')}
          checked={p.editDetails}
          onCheckedChange={(editDetails) => setP({ editDetails })}
        />
        <ToggleField
          label={t('Allow users to change templates when editing web domains')}
          checked={p.editWebTemplates}
          onCheckedChange={(editWebTemplates) => setP({ editWebTemplates })}
        />
        <ToggleField
          label={t('Allow users to change templates when editing DNS zones')}
          checked={p.editDnsTemplates}
          onCheckedChange={(editDnsTemplates) => setP({ editDnsTemplates })}
        />
        <ToggleField
          label={t('Allow users to view action and login history logs')}
          checked={p.viewLogs}
          onCheckedChange={(viewLogs) => setP({ viewLogs })}
        />
        <ToggleField
          label={t('Allow users to delete log history')}
          checked={p.deleteLogs}
          onCheckedChange={(deleteLogs) => setP({ deleteLogs })}
        />
        {f.previewFeatures && (
          <ToggleField
            label={t('Allow suspended users to log in with read-only access')}
            description={t('Preview feature.')}
            checked={p.viewSuspended}
            onCheckedChange={(viewSuspended) => setP({ viewSuspended })}
          />
        )}
        <ToggleField
          label={t('Allow suspended users to create new backups')}
          checked={p.backupSuspendedUsers}
          onCheckedChange={(backupSuspendedUsers) => setP({ backupSuspendedUsers })}
        />
        <ToggleField
          label={t('Sync error document templates on user rebuild')}
          checked={p.syncErrorDocuments}
          onCheckedChange={(syncErrorDocuments) => setP({ syncErrorDocuments })}
        />
        <ToggleField
          label={t('Sync skeleton templates')}
          checked={p.syncSkeleton}
          onCheckedChange={(syncSkeleton) => setP({ syncSkeleton })}
        />
      </FormSection>
      <FormSection title={t('Domains')} description={t('Rules for adding domains across accounts.')}>
        <ToggleField
          label={t('Enforce subdomain ownership')}
          description={t('Only the owner of a domain can add its subdomains.')}
          checked={p.enforceSubdomain}
          onCheckedChange={(enforceSubdomain) => setP({ enforceSubdomain })}
        />
      </FormSection>
    </>
  )
}

function PluginsTab({ f, set }: Props) {
  const p = f.plugins
  const setP = (patch: Partial<typeof p>) => set({ plugins: { ...p, ...patch } })
  const t = useT()
  return (
    <FormSection title={t('Plugins')} description={t('Optional panel features and system-wide limits.')}>
      <ToggleField
        label={t('Quick App Installer')}
        checked={p.appInstaller}
        onCheckedChange={(appInstaller) => setP({ appInstaller })}
      />
      <ToggleField
        label={t('File Manager')}
        checked={p.fileManager}
        onCheckedChange={(fileManager) => setP({ fileManager })}
      />
      <ToggleField
        label={t('Web Terminal')}
        checked={p.webTerminal}
        onCheckedChange={(webTerminal) => setP({ webTerminal })}
      />
      <ToggleField
        label={t('Limit system resources')}
        checked={p.resourcesLimit}
        onCheckedChange={(resourcesLimit) => setP({ resourcesLimit })}
      />
      <ToggleField
        label={t('File system disk quota')}
        checked={p.diskQuota}
        onCheckedChange={(diskQuota) => setP({ diskQuota })}
      />
      <ToggleField label={t('Firewall')} checked={p.firewall} onCheckedChange={(firewall) => setP({ firewall })} />
    </FormSection>
  )
}

/** What the finished save did: the steps applied, the core's notes, and the failure. */
function SaveResult({ outcome }: { outcome: JobStatus }) {
  const t = useT()
  const failed = outcome.state === 'failed'
  const result = (outcome.result ?? {}) as Partial<SettingsResult>
  // A failed job's last step is the one that failed, so only the ones before it were applied
  const applied = failed ? outcome.steps.slice(0, -1) : (result.applied ?? outcome.steps)
  const notes = result.notes ?? []
  return (
    <div className="grid gap-3">
      {failed ? (
        <div role="alert">
          <Banner
            variant="error"
            icon={<WarningCircleIcon weight="fill" />}
            title={apiErrorText(
              new ApiError(0, outcome.error?.code ?? '', outcome.error?.message ?? '', outcome.error?.args ?? []),
              t,
            )}
            className="whitespace-pre-line"
          />
        </div>
      ) : (
        <Banner variant="default" icon={<CheckCircleIcon weight="fill" />} title={t('Changes have been saved.')} />
      )}
      {notes.length > 0 && (
        <Banner
          variant="default"
          icon={<InfoIcon weight="fill" />}
          title={notes.map((n) => t(n)).join('\n')}
          className="whitespace-pre-line"
        />
      )}
      {applied.length > 0 && (
        <div className="grid gap-1.5">
          <Text bold as="h3">
            {failed ? t('Applied before the failure') : t('Applied changes')}
          </Text>
          <ul aria-label={t('Applied changes')} className="grid gap-1">
            {applied.map((step, i) => (
              <li key={`${i}-${step}`} className="flex items-start gap-2">
                <span className="flex h-lh items-center">
                  <CheckCircleIcon className="text-kumo-success" />
                </span>
                <Text as="span">{stepText(step, t)}</Text>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  )
}

export function ServerConfigPage() {
  const settings = useSettings()
  const t = useT()
  if (settings.error || !settings.data) {
    return (
      <FormPage title={t('Server configuration')} back={{ href: '/server/services', label: t('Services') }} readOnly>
        {settings.error ? <DnsError error={settings.error} /> : <DnsLoading />}
      </FormPage>
    )
  }
  return <ConfigForm s={settings.data} />
}

function ConfigForm({ s }: { s: SettingsView }) {
  const m = useServerMutation()
  const t = useT()
  const { refresh } = useSession()
  const [f, setF] = useState<Form>(() => seed(s))
  const [tab, setTab] = useState('general')
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)
  const [jobId, setJobId] = useState('')
  const [outcome, setOutcome] = useState<JobStatus | null>(null)
  const set = (patch: Partial<Form>) => setF((prev) => ({ ...prev, ...patch }))

  // The guard of the save in progress: a job that fails re-arms it.
  const saving = useRef<FormGuard | null>(null)
  const onSubmit = async (e: FormEvent, guard: FormGuard) => {
    void e
    saving.current = guard
    setError(null)
    setOutcome(null)
    setJobId('')
    setBusy(true)
    try {
      const ref = await m.saveSettings(buildRequest(s, f))
      setJobId(ref.jobId)
    } catch (err) {
      // Validation and the firewall lockout answer before any job starts; the message
      // shows above the tabs, so bring it into view from wherever the field was
      setError(err)
      setBusy(false)
      scrollContentToTop()
    }
  }
  const onDone = (status: JobStatus) => {
    setBusy(false)
    setOutcome(status)
    if (status.state !== 'done') saving.current?.rearm()
    // The sidebar and other pages follow API_SYSTEM and the POLICY_* values: reload the session
    // after every save, also a part-way failure, which may already have changed some of them
    void refresh()
    if (status.state === 'done') {
      set({ relayPassword: '', mysqlPassword: '', backupPassword: '', b2Key: '', sslKey: '' })
      if ((status.result as Partial<SettingsResult> | null)?.refresh) window.location.reload()
    }
  }

  const tabs: { value: string; label: string; render: (p: Props) => ReactNode }[] = [
    { value: 'general', label: msgid('General'), render: GeneralTab },
    { value: 'updates', label: msgid('Updates'), render: UpdatesTab },
    ...(s.webSystem || s.proxySystem || s.webBackend
      ? [{ value: 'web', label: msgid('Web server'), render: WebTab }]
      : []),
    ...(s.dnsSystem ? [{ value: 'dns', label: msgid('DNS server'), render: DnsTab }] : []),
    ...(s.mail.system ? [{ value: 'mail', label: msgid('Mail server'), render: MailTab }] : []),
    ...(s.db.system ? [{ value: 'databases', label: msgid('Databases'), render: DatabasesTab }] : []),
    { value: 'backups', label: msgid('Backups'), render: BackupsTab },
    { value: 'ssl', label: msgid('SSL'), render: SslTab },
    { value: 'security', label: msgid('Security'), render: SecurityTab },
    { value: 'policies', label: msgid('Policies'), render: PoliciesTab },
    { value: 'plugins', label: msgid('Plugins'), render: PluginsTab },
  ]
  const active = tabs.find((x) => x.value === tab) ?? tabs[0]

  return (
    <FormPage
      title={t('Server configuration')}
      description={t('Global settings for this server and every account on it.')}
      back={{ href: '/server/services', label: t('Services') }}
      onSubmit={onSubmit}
      submitDisabled={busy}
      actions={
        <>
          <LinkButton href="/server/ip" variant="secondary" icon={NetworkIcon}>
            {t('Network')}
          </LinkButton>
          <LinkButton href="/server/white-label" variant="secondary" icon={PaintBrushIcon}>
            {t('White label')}
          </LinkButton>
          <LinkButton href="/server/services/hestiaweb" variant="secondary" icon={ClockIcon}>
            {t('Panel cron jobs')}
          </LinkButton>
        </>
      }
    >
      <ErrorNote error={error} />
      {outcome ? <SaveResult outcome={outcome} /> : <JobPanel jobId={jobId} onDone={onDone} />}
      <div className="-mx-4 overflow-x-auto px-4 sm:mx-0 sm:px-0">
        <Tabs
          variant="underline"
          value={active.value}
          onValueChange={setTab}
          tabs={tabs.map(({ value, label }) => ({ value, label: t(label) }))}
          className="min-w-max"
        />
      </div>
      <div className="grid gap-8">{active.render({ s, f, set })}</div>
    </FormPage>
  )
}
