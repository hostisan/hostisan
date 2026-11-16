import { Badge, Banner, Button, Checkbox, Collapsible, Input, Switch, Text, useKumoToastManager } from '@cloudflare/kumo'
import { InfoIcon, ShieldCheckIcon, WarningIcon } from '@phosphor-icons/react'
import { useEffect, useMemo, useState, type FormEvent, type ReactNode } from 'react'
import { useNavigate } from 'react-router'
import { useAccountMutation } from '../../api/account'
import { apiErrorText } from '../../api/errors'
import { braceMessage } from '../../components/BraceMessage'
import type { Package } from '../../api/hestia.gen'
import type { AccountOptions, AccountView, UserEditRequest } from '../../api/types.gen'
import { usePackage, useUsers } from '../../api/users'
import { FormPage } from '../../components/FormPage'
import type { FormGuard } from '../../unsaved/types'
import { FormSection } from '../../components/FormSection'
import { FormWithToc, TocAnchor, type TocSection } from '../../components/FormWithToc'
import { NameServersField, SelectField, TagField } from '../../components/fields'
import { PasswordInput } from '../../components/PasswordInput'
import { useT } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { formatBackend, formatMB } from '../../lib/format'
import { scrollContentToTop } from '../../lib/scroll'
import { isIpv4 } from '../../lib/validate'
import { useSession, useSessionUser } from '../../session/SessionProvider'
import { useLoginAs } from './useLoginAs'
import { formatLimit } from './fields'
import { interpolate } from '../../lib/interpolate'
import { TwoFactorDialog, TwoFactorOffDialog } from './TwoFactorDialog'

export type UserFormMode = 'add' | 'edit' | 'account'

type UserFormProps = {
  mode: UserFormMode
  options: AccountOptions
  /** The account being edited; absent when adding. */
  view?: AccountView
  /** Page header and the pieces around the form. */
  title: string
  description?: ReactNode
  back: { href: string; label: string }
  actions?: ReactNode
  /** Rendered above the sections, e.g. the suspended banner. */
  banner?: ReactNode
}

const ROLE_LABELS: Record<string, string> = {
  user: msgid('User'),
  admin: msgid('Administrator'),
  'dns-cluster': msgid('DNS Sync User'),
}

/** "German (Deutsch)" for a language code; the code itself when the browser doesn't know it. */
function languageLabel(code: string): string {
  try {
    const english = new Intl.DisplayNames(['en'], { type: 'language' }).of(code)
    const native = new Intl.DisplayNames([code], { type: 'language' }).of(code)
    if (!english || english === code) return code
    return native && native !== english ? `${english} (${native})` : english
  } catch {
    return code
  }
}

type Limits = Pick<
  Package,
  'DISK_QUOTA' | 'BANDWIDTH' | 'WEB_DOMAINS' | 'DNS_DOMAINS' | 'MAIL_DOMAINS' | 'DATABASES' | 'CRON_JOBS' | 'BACKUPS'
>

/** The main package limits, from a package or from the limits copied into a user's record. */
function LimitsSummary({ limits: p }: { limits: Limits }) {
  const t = useT()
  const items = [
    [t('Quota'), formatMB(p.DISK_QUOTA)],
    [t('Bandwidth'), formatMB(p.BANDWIDTH)],
    [t('Web domains'), formatLimit(p.WEB_DOMAINS, t)],
    [t('DNS zones'), formatLimit(p.DNS_DOMAINS, t)],
    [t('Mail domains'), formatLimit(p.MAIL_DOMAINS, t)],
    [t('Databases'), formatLimit(p.DATABASES, t)],
    [t('Cron jobs'), formatLimit(p.CRON_JOBS, t)],
    [t('Backups'), p.BACKUPS],
  ]
  return (
    <dl className="grid grid-cols-2 gap-x-6 gap-y-2 rounded-lg bg-kumo-recessed px-4 py-3 sm:grid-cols-4">
      {items.map(([k, v]) => (
        <div key={k} className="grid gap-0.5">
          <dt className="text-sm text-kumo-subtle">{k}</dt>
          <dd className="tabular-nums">{v}</dd>
        </div>
      ))}
    </dl>
  )
}

/** The server never opens the "system" package, so its limits come from the user's record. */
function PackageSummary({ name, record }: { name: string; record?: Limits }) {
  const system = name === 'system'
  const { data } = usePackage(system ? '' : name)
  const limits = system ? record : data
  return limits ? <LimitsSummary limits={limits} /> : null
}

/** Looks up the root administrator, so its role is not offered; only mounted when it can matter. */
function RootProbe({ username, onRoot }: { username: string; onRoot: (root: boolean) => void }) {
  const root = useUsers().data?.rootUser
  useEffect(() => onRoot(root === username), [root, username, onRoot])
  return null
}

const splitList = (s: string) => s.split(',').filter(Boolean)

/**
 * The add user, edit user and "My account" form (Plan 3): the server decides what each
 * session may change (`view.editable`), the form only hides what it would refuse.
 */
export function UserForm({ mode, options, view, title, description, back, actions, banner }: UserFormProps) {
  const t = useT()
  const m = useAccountMutation()
  const toast = useKumoToastManager()
  const navigate = useNavigate()
  const session = useSession()
  const loginAs = useLoginAs()
  const sessionUser = useSessionUser()
  const user = view?.user
  const isAdd = mode === 'add'
  const admin = isAdd || Boolean(view?.editable.admin)
  const canEditDetails = isAdd || Boolean(view?.editable.details)
  const canTheme = !isAdd && Boolean(view?.editable.theme) && options.themes.length > 0
  const self = Boolean(view?.self)
  const username = user?.USER ?? ''

  const [newUsername, setNewUsername] = useState('')
  const [name, setName] = useState(user?.NAME ?? '')
  const [email, setEmail] = useState(user?.CONTACT ?? '')
  const [password, setPassword] = useState('')
  const [currentPassword, setCurrentPassword] = useState('')
  const [loginDisabled, setLoginDisabled] = useState(user?.LOGIN_DISABLED === 'yes')
  const [welcomeEmail, setWelcomeEmail] = useState(false)
  const [notify, setNotify] = useState('')
  const [useIpList, setUseIpList] = useState(user?.LOGIN_USE_IPLIST === 'yes')
  const [allowedIps, setAllowedIps] = useState(() => splitList(user?.LOGIN_ALLOW_IPS ?? ''))
  // A new user starts at the system language, as Hestia's add form (owner ruling 13).
  const [language, setLanguage] = useState(user?.LANGUAGE || options.systemLanguage || 'en')
  const [theme, setTheme] = useState(user?.THEME ?? '')
  const [sortOrder, setSortOrder] = useState(user?.PREF_UI_SORT || 'name')
  const [role, setRole] = useState(user?.ROLE || 'user')
  const [pkg, setPkg] = useState(user?.PACKAGE || (options.packages.includes('default') ? 'default' : (options.packages[0] ?? '')))
  const [shell, setShell] = useState(user?.SHELL ?? '')
  const [phpCli, setPhpCli] = useState(user?.PHPCLI || options.defaultPhp)
  const [nameservers, setNameservers] = useState(() => splitList(user?.NS ?? ''))
  // An empty list is sent as "not offered", so removing every name server is caught here.
  const [nsEdited, setNsEdited] = useState(false)
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const [setupOpen, setSetupOpen] = useState(false)
  const [offOpen, setOffOpen] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [isRoot, setIsRoot] = useState(false)
  // Not while logged in as another user: the users list is refused then (owner ruling 12).
  const probeRoot = admin && !isAdd && !self && !sessionUser?.look

  // The page may mount before the account arrives from a refetch; keep the 2FA state live.
  const [twofa, setTwofa] = useState(view?.twofa ?? false)
  useEffect(() => setTwofa(view?.twofa ?? false), [view?.twofa])

  const languages = useMemo(
    () => options.languages.map((c) => ({ value: c, label: languageLabel(c) })).sort((a, b) => a.label.localeCompare(b.label)),
    [options.languages],
  )
  const themeItems = [{ value: '', label: t('System default') }, ...options.themes.map((x) => ({ value: x, label: x[0].toUpperCase() + x.slice(1) }))]
  const newPassword = password !== ''
  const needsCurrent = newPassword && self && !isAdd

  const sections: TocSection[] = [
    { id: 'account', label: t('Account') },
    { id: 'login', label: t('Password and login') },
    ...(!isAdd ? [{ id: 'twofa', label: t('Two-factor authentication') }] : []),
    { id: 'preferences', label: t('Preferences') },
    { id: 'package', label: admin ? t('Role and package') : t('Package') },
    ...(admin && !isAdd ? [{ id: 'advanced', label: t('Advanced options') }] : []),
  ]

  const onSubmit = async (e: FormEvent, guard: FormGuard) => {
    void e
    if (admin && !isAdd && options.dnsSystem && nsEdited && nameservers.length === 0) {
      guard.rearm()
      setError(t('Field "%s" can not be blank.', t('Nameserver 1')))
      return
    }
    setError('')
    setBusy(true)
    try {
      if (isAdd) {
        await m.addUser({
          username: newUsername,
          password,
          email,
          name,
          language,
          role,
          package: pkg,
          loginDisabled,
          notify: welcomeEmail ? notify || email : '',
        })
        const created = newUsername
        toast.add({
          title: braceMessage(t('User {%s} has been created successfully.'), created, (n) => n),
          description: (
            <Button size="sm" onClick={() => void logInAs(created)}>
              {t('Log in as %s', created)}
            </Button>
          ),
          type: 'success',
        })
        navigate(back.href)
        return
      }
      const body: UserEditRequest = {
        name,
        email,
        password,
        currentPassword: needsCurrent ? currentPassword : '',
        language,
        theme: canTheme ? theme : '',
        sortOrder,
        useIpList,
        allowedIps: useIpList ? allowedIps : [],
        // Empty values mean "not offered": the advanced section may never have been filled.
        ...(admin
          ? {
              loginDisabled,
              package: pkg,
              ...(shell ? { shell } : {}),
              ...(phpCli ? { phpCli } : {}),
              ...(role !== user?.ROLE ? { role } : {}),
              ...(options.dnsSystem && nameservers.length > 0 ? { nameservers } : {}),
            }
          : {}),
      }
      const saved = mode === 'account' ? await m.save(body) : await m.editUser(username, body)
      toast.add({ title: t('Changes have been saved.'), type: 'success' })
      setPassword('')
      setCurrentPassword('')
      if (saved.languageChanged) await session.refresh()
    } catch (err) {
      // The banner sits above the form: bring it into view from the field that was refused
      setError(apiErrorText(err, t))
      scrollContentToTop()
    } finally {
      setBusy(false)
    }
  }

  const logInAs = async (user: string) => {
    try {
      await loginAs(user)
    } catch (err) {
      toast.add({ title: apiErrorText(err, t), type: 'error' })
    }
  }

  return (
    <FormPage
      title={title}
      description={description}
      back={back}
      actions={actions}
      submitLabel={isAdd ? t('Add user') : t('Save')}
      onSubmit={onSubmit}
      submitDisabled={busy || (useIpList && allowedIps.length === 0)}
      readOnly={sessionUser?.readOnly}
    >
      {banner}
      {error && (
        <Banner variant="error" icon={<WarningIcon weight="fill" />} title={error} className="whitespace-pre-line" />
      )}
      <FormWithToc sections={sections} onBeforeJump={(id) => id === 'advanced' && setAdvancedOpen(true)}>
        <TocAnchor id="account">
          <FormSection title={t('Account')} description={t('Who this account belongs to and where notices are sent.')}>
            {isAdd ? (
              <Input
                label={t('Username')}
                required
                autoComplete="off"
                placeholder="acme"
                value={newUsername}
                onChange={(e) => setNewUsername(e.target.value)}
              />
            ) : (
              <Input
                label={t('Username')}
                value={username}
                disabled
                description={t("Usernames can't be changed after the account is created.")}
              />
            )}
            <Input
              label={t('Contact name')}
              required
              disabled={!canEditDetails}
              value={name}
              placeholder="Acme Bakery"
              onChange={(e) => setName(e.target.value)}
            />
            <Input
              label={t('Email')}
              type="email"
              required
              disabled={!canEditDetails}
              value={email}
              placeholder="owner@acme-bakery.com"
              onChange={(e) => setEmail(e.target.value)}
            />
            {!canEditDetails && (
              <Text variant="secondary" size="sm">
                {t('Your administrator has turned off changing your name and email.')}
              </Text>
            )}
          </FormSection>
        </TocAnchor>

        <TocAnchor id="login">
          <FormSection
            title={t('Password and login')}
            description={
              mode === 'account' ? t('How you sign in to the control panel.') : t('How this user signs in to the control panel.')
            }
          >
            <PasswordInput
              label={isAdd ? t('Password') : t('New password')}
              value={password}
              onChange={setPassword}
              required={isAdd}
              description={isAdd ? undefined : t('Leave blank to keep the current password.')}
            />
            {needsCurrent && (
              <PasswordInput
                label={t('Current password')}
                isNew={false}
                value={currentPassword}
                onChange={setCurrentPassword}
                required
                description={t("Needed to confirm that it's you changing the password.")}
              />
            )}

            {admin && (
              <Switch
                label={t('Do not allow user to log in to Control Panel')}
                checked={loginDisabled}
                onCheckedChange={setLoginDisabled}
              />
            )}

            {isAdd && !loginDisabled && (
              <div className="grid gap-3">
                <Checkbox
                  label={t('Send welcome email')}
                  checked={welcomeEmail}
                  onCheckedChange={(c) => {
                    setWelcomeEmail(c === true)
                    if (c === true && notify === '') setNotify(email)
                  }}
                />
                {welcomeEmail && (
                  <Input
                    label={t('Email login credentials to:')}
                    type="email"
                    value={notify}
                    onChange={(e) => setNotify(e.target.value)}
                  />
                )}
              </div>
            )}

            {!isAdd && (
              <div className="grid gap-3">
                <Switch
                  label={t('Use IP address allow list for login attempts')}
                  checked={useIpList}
                  onCheckedChange={setUseIpList}
                />
                {useIpList && (
                  <TagField
                    label={t('Allowed IP addresses')}
                    value={allowedIps}
                    onValueChange={setAllowedIps}
                    placeholder={t('For example: %s', '127.0.0.1')}
                    autoComplete="off"
                    description={t('Press Enter or comma after each address. Logins from any other address are rejected.')}
                    validateValue={(v, accepted) => isIpv4(v) && !accepted.includes(v)}
                    labels={{ invalidValue: (v) => t("“%s” isn't a valid IP address, or it's already in the list.", v) }}
                    error={allowedIps.length === 0 ? t('Add at least one address, or turn the allow list off.') : undefined}
                  />
                )}
              </div>
            )}

            {loginDisabled && (
              <Banner
                variant="secondary"
                icon={<InfoIcon weight="fill" />}
                description={t("This user can still use SSH, FTP and mail, but can't sign in to the control panel.")}
              />
            )}
          </FormSection>
        </TocAnchor>

        {!isAdd && (
          <TocAnchor id="twofa">
            <FormSection
              title={t('Two-factor authentication')}
              description={t('A code from an authenticator app, asked for after the password.')}
            >
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div className="flex items-center gap-2">
                  <Badge variant={twofa ? 'green' : 'neutral'}>{twofa ? t('Enabled') : t('Disabled')}</Badge>
                  <Text variant="secondary">
                    {twofa ? t('Signing in needs a code from your app.') : t('Signing in needs only the password.')}
                  </Text>
                </div>
                {twofa ? (
                  <Button type="button" variant="secondary" onClick={() => setOffOpen(true)}>
                    {t('Turn off')}
                  </Button>
                ) : (
                  self && (
                    <Button type="button" variant="secondary" onClick={() => setSetupOpen(true)}>
                      {t('Set up two-factor authentication')}
                    </Button>
                  )
                )}
              </div>
              {!twofa && !self && (
                <Text variant="secondary" size="sm">
                  {t("Two-factor authentication can only be set up by the account's own user.")}
                </Text>
              )}
            </FormSection>
          </TocAnchor>
        )}

        <TocAnchor id="preferences">
          <FormSection title={t('Preferences')} description={t('Language and how lists are shown in the panel.')}>
            <SelectField label={t('Language')} value={language} onChange={setLanguage} options={languages} />
            {!isAdd && canTheme && <SelectField label={t('Theme')} value={theme} onChange={setTheme} options={themeItems} />}
            {!isAdd && (
              <SelectField
                label={t('Default list sort order')}
                value={sortOrder}
                onChange={setSortOrder}
                options={[
                  { value: 'date', label: t('Date') },
                  { value: 'name', label: t('Name') },
                ]}
              />
            )}
          </FormSection>
        </TocAnchor>

        <TocAnchor id="package">
          {admin ? (
            <FormSection
              title={t('Role and package')}
              description={t('Permissions in the panel and the resource limits that apply.')}
            >
              {isAdd || (!self && !isRoot) ? (
                <SelectField
                  label={t('Role')}
                  value={role}
                  onChange={setRole}
                  options={Object.entries(ROLE_LABELS).map(([value, label]) => ({ value, label: t(label) }))}
                />
              ) : (
                <div className="grid gap-1.5">
                  <Text bold>{t('Role')}</Text>
                  <div className="flex items-center gap-2">
                    <Badge variant="purple">{t(ROLE_LABELS[user?.ROLE ?? 'admin'] ?? user?.ROLE ?? '')}</Badge>
                    <Text variant="secondary" size="sm">
                      {self ? t("You can't change your own role.") : t("This account's role can't be changed.")}
                    </Text>
                  </div>
                </div>
              )}
              <SelectField
                label={t('Package')}
                value={pkg}
                onChange={setPkg}
                options={options.packages.map((p) => ({ value: p, label: p }))}
              />
              <PackageSummary name={pkg} record={pkg === user?.PACKAGE ? user : undefined} />
            </FormSection>
          ) : (
            user && (
              <FormSection title={t('Package')} description={t('Set by your administrator.')}>
                <div className="flex items-center gap-2">
                  <span className="flex h-lh items-center">
                    <ShieldCheckIcon className="text-kumo-subtle" />
                  </span>
                  <Text>
                    {interpolate(
                      t("You're on the %s package."),
                      <Text as="span" bold>
                        {user.PACKAGE}
                      </Text>,
                    )}
                  </Text>
                </div>
                <LimitsSummary limits={user} />
              </FormSection>
            )
          )}
        </TocAnchor>

        {admin && !isAdd && (
          <TocAnchor id="advanced">
            <Collapsible.Root open={advancedOpen} onOpenChange={setAdvancedOpen} className="grid gap-6">
              <Collapsible.DefaultTrigger>{t('Advanced options')}</Collapsible.DefaultTrigger>
              <Collapsible.Panel>
                <FormSection
                  title={t('Advanced options')}
                  description={t('Shell access, the PHP version used on the command line, and default name servers for new DNS zones.')}
                >
                  <SelectField
                    label={t('SSH access')}
                    value={shell}
                    onChange={setShell}
                    options={options.shells.map((s) => ({ value: s, label: s }))}
                  />
                  <SelectField
                    label={t('PHP CLI version')}
                    value={phpCli}
                    onChange={setPhpCli}
                    options={options.phpVersions.map((v) => ({ value: v, label: formatBackend(`PHP-${v.replace('.', '_')}`) }))}
                  />
                  {options.dnsSystem && <NameServersField
                      defaultValue={nameservers}
                      onChange={(v) => {
                        setNsEdited(true)
                        setNameservers(v)
                      }}
                    />}
                </FormSection>
              </Collapsible.Panel>
            </Collapsible.Root>
          </TocAnchor>
        )}
      </FormWithToc>
      {probeRoot && <RootProbe username={username} onRoot={setIsRoot} />}
      <TwoFactorDialog open={setupOpen} onOpenChange={setSetupOpen} />
      <TwoFactorOffDialog open={offOpen} onOpenChange={setOffOpen} username={username} needsPassword={self} />
    </FormPage>
  )
}
