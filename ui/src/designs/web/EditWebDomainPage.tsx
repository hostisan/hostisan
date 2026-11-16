import { ValuesList } from '../../components/ValuesList'
import { isAliasHostname, isExtension } from '../../lib/validate'
import {
  Banner,
  Button,
  Checkbox,
  Collapsible,
  Input,
  InputArea,
  LayerCard,
  Link,
  LinkButton,
  Radio,
  Select,
  Switch,
  TableOfContents,
  Text,
  useKumoToastManager,
  useTableOfContentsActiveId,
} from '@cloudflare/kumo'
import { TagField } from '../../components/fields'
import {
  ArrowsClockwiseIcon,
  FileTextIcon,
  InfoIcon,
  MagicWandIcon,
  PauseCircleIcon,
  PlusIcon,
  TrashIcon,
  WarningIcon,
} from '@phosphor-icons/react'
import { scrollContentToTop } from '../../lib/scroll'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useNavigate } from 'react-router'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { DeleteResource } from '../../components/DeleteResource'
import { PasswordInput } from '../../components/PasswordInput'
import { FormPage } from '../../components/FormPage'
import { FormSection } from '../../components/FormSection'
import { StatusBadge } from '../../components/StatusBadge'
import { formatBackend } from '../../lib/format'
import { ApiError } from '../../api/client'
import { apiErrorText } from '../../api/errors'
import type {
  WebDomainView,
  WebEditRequest,
  WebFTPChange,
  WebOptions,
  WebRedirectView,
  WebSSLRequest,
} from '../../api/types.gen'
import { useWebDomains, useWebMutation, useWebOptions } from '../../api/web'
import { useT } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { useSessionUser } from '../../session/SessionProvider'
import { interpolate } from '../../lib/interpolate'
import { DnsError, DnsLoading } from '../dns/shared'
import { RequestFlow } from './RequestFlow'
import { DomainNotFound, useWebDomainParam } from './shared'

const SECTIONS = [
  { id: 'domain', label: msgid('Domain') },
  { id: 'request-flow', label: msgid('Request flow') },
  { id: 'statistics', label: msgid('Web statistics') },
  { id: 'redirection', label: msgid('Redirection') },
  { id: 'ssl', label: msgid('SSL') },
  { id: 'templates', label: msgid('Server templates'), advanced: true },
  { id: 'document-root', label: msgid('Custom document root'), advanced: true },
  { id: 'ftp', label: msgid('FTP accounts'), advanced: true },
]

function Anchor({ id, children }: { id: string; children: ReactNode }) {
  return (
    <div id={id} className="scroll-mt-6">
      {children}
    </div>
  )
}

function scrollParent(el: HTMLElement | null): HTMLElement | null {
  for (let node = el?.parentElement; node; node = node.parentElement) {
    if (/(auto|scroll)/.test(getComputedStyle(node).overflowY)) return node
  }
  return null
}

const toItems = (values: string[], format: (v: string) => string = (v) => v) =>
  Object.fromEntries(values.map((v) => [v, format(v)]))

const KEY_TYPES = {
  'ecdsa-256': msgid('ECDSA P-256 (recommended)'),
  'ecdsa-384': 'ECDSA P-384',
  rsa: 'RSA 4096',
}

type FtpRow = {
  id: number
  name: string
  password: string
  path: string
  prevPath: string
  email: string
  isNew: boolean
  /** An existing account the user removed with its Delete button. */
  removed: boolean
}

const blankFtp = (id: number): FtpRow => ({
  id,
  name: '',
  password: '',
  path: '',
  prevPath: '',
  email: '',
  isNew: true,
  removed: false,
})

const sameList = (a: string[], b: string[]) => [...a].sort().join('\n') === [...b].sort().join('\n')

const redirectModeValue = (mode: WebRedirectView['mode'], name: string) =>
  mode === 'www' ? `www.${name}` : mode === 'bare' ? name : 'custom'

export function EditWebDomainPage() {
  const t = useT()
  const { name, query } = useWebDomainParam()
  const options = useWebOptions()
  if (!query.data && query.error instanceof ApiError && query.error.status === 404) {
    return <DomainNotFound name={name} />
  }
  if (!query.data || !options.data) {
    const failed = query.error ?? options.error
    return (
      <FormPage title={t('Edit web domain')} back={{ href: '/web', label: t('Web domains') }} readOnly>
        {failed ? <DnsError error={failed} /> : <DnsLoading />}
      </FormPage>
    )
  }
  return <EditWebDomainForm key={name} name={name} view={query.data} options={options.data} />
}

function EditWebDomainForm({ name, view, options }: { name: string; view: WebDomainView; options: WebOptions }) {
  const t = useT()
  const toast = useKumoToastManager()
  const navigate = useNavigate()
  const user = useSessionUser()
  const m = useWebMutation()
  const domainList = useWebDomains()
  const domain = view.domain
  const ssl = view.ssl
  const ftpPrefix = options.ftpPrefix
  const suspended = domain.SUSPENDED === 'yes'
  const canEditTemplates = options.canEditTemplates
  const isAdmin = user?.role === 'admin'
  const readOnly = user?.readOnly ?? false
  const hasProxy = options.proxySystem !== '' && domain.PROXY !== ''

  const [aliases, setAliases] = useState(view.aliases)
  const [ip, setIp] = useState(domain.IP)

  const [stats, setStats] = useState(domain.STATS || 'none')
  const [statsAuth, setStatsAuth] = useState(!!domain.STATS_USER)
  const [statsUser, setStatsUser] = useState(domain.STATS_USER)
  const [statsPassword, setStatsPassword] = useState('')

  const [redirect, setRedirect] = useState(view.redirect.enabled)
  const [redirectMode, setRedirectMode] = useState(redirectModeValue(view.redirect.mode, name))
  const [redirectCustom, setRedirectCustom] = useState(view.redirect.mode === 'custom' ? view.redirect.target : '')
  const [redirectCode, setRedirectCode] = useState(view.redirect.code || '301')

  const initialSsl = {
    on: domain.SSL === 'yes',
    le: domain.LETSENCRYPT === 'yes',
    keyAlgo: view.keyAlgo,
    force: domain.SSL_FORCE === 'yes',
    hsts: domain.SSL_HSTS === 'yes',
    crt: ssl?.CRT ?? '',
    key: ssl?.KEY ?? '',
    ca: ssl?.CA ?? '',
  }
  const [sslEnabled, setSslEnabled] = useState(initialSsl.on)
  const [letsEncrypt, setLetsEncrypt] = useState(initialSsl.le)
  const [keyAlgo, setKeyAlgo] = useState(initialSsl.keyAlgo)
  const [forceSsl, setForceSsl] = useState(initialSsl.force)
  const [hsts, setHsts] = useState(initialSsl.hsts)
  const [showCertificates, setShowCertificates] = useState(domain.LETSENCRYPT !== 'yes')
  const [crt, setCrt] = useState(initialSsl.crt)
  const [key, setKey] = useState(initialSsl.key)
  const [ca, setCa] = useState(initialSsl.ca)

  const [advanced, setAdvanced] = useState(false)
  const [template, setTemplate] = useState(domain.TPL)
  const [fastcgiCache, setFastcgiCache] = useState(domain.FASTCGI_CACHE === 'yes')
  const [fastcgiDuration, setFastcgiDuration] = useState(domain.FASTCGI_DURATION || '2m')
  const [backend, setBackend] = useState(domain.BACKEND || 'default')
  const [proxyTemplate, setProxyTemplate] = useState(domain.PROXY || 'default')
  // A domain with no extensions starts from the server's default list (owner ruling 6).
  const [proxyExt, setProxyExt] = useState(() => {
    const stored = domain.PROXY_EXT.split(',').filter(Boolean)
    return stored.length > 0 ? stored : options.defaultProxyExt
  })
  const [customDocroot, setCustomDocroot] = useState(view.docroot.enabled)
  const [docrootDomain, setDocrootDomain] = useState(view.docroot.domain || name)
  const [docrootFolder, setDocrootFolder] = useState(view.docroot.folder)

  const storedFtp = view.ftp.length > 0
  const [ftpEnabled, setFtpEnabled] = useState(storedFtp)
  const [ftpRows, setFtpRows] = useState<FtpRow[]>(
    storedFtp
      ? view.ftp.map((a, i) => ({ ...blankFtp(i), name: a.name, path: a.path, prevPath: a.path, isNew: false }))
      : [blankFtp(0)],
  )

  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const [confirm, setConfirm] = useState<'suspend' | 'unsuspend' | 'purge'>('suspend')
  const [deleteOpen, setDeleteOpen] = useState(false)

  const contentRef = useRef<HTMLDivElement>(null)
  const [scrollRoot, setScrollRoot] = useState<HTMLElement | null>(null)
  useEffect(() => setScrollRoot(scrollParent(contentRef.current)), [])
  const visibleSections = SECTIONS.filter(
    (section) => (section.id !== 'templates' || canEditTemplates) && (section.id !== 'ftp' || options.ftp),
  )
  const { activeId, selectSection } = useTableOfContentsActiveId({
    // Advanced sections only exist in the DOM while the panel is open.
    ids: visibleSections.filter((section) => advanced || !section.advanced).map((section) => section.id),
    root: scrollRoot,
    offset: 24,
    trackHash: false,
  })
  const jumpTo = (id: string, advancedSection?: boolean) => {
    if (advancedSection) setAdvanced(true)
    selectSection(id)
    requestAnimationFrame(() => {
      const el = document.getElementById(id)
      if (!el || !scrollRoot) return
      const top = scrollRoot.scrollTop + el.getBoundingClientRect().top - scrollRoot.getBoundingClientRect().top - 24
      scrollRoot.scrollTo({ top, behavior: 'smooth' })
    })
  }
  const [confirmOpen, setConfirmOpen] = useState(false)
  const ask = (kind: typeof confirm) => {
    setConfirm(kind)
    setConfirmOpen(true)
  }

  const webIsNginx = options.nginx
  const proxyIsNginx = options.proxySystem === 'nginx'
  const nginxCaching =
    options.purge && ((webIsNginx && fastcgiCache) || (hasProxy && proxyIsNginx && proxyTemplate.startsWith('caching')))
  // Hestia starts the FTP path hint at the stored custom document root's domain (owner ruling 8).
  const ftpHintDomain = (view.docroot.enabled && view.docroot.domain) || name
  const docrootPreview = `${options.webRoot}${docrootDomain}/public_html/${docrootFolder.replace(/^\/+/, '')}`
  const visibleFtp = ftpRows.filter((r) => !r.removed)
  const updateFtp = (id: number, patch: Partial<FtpRow>) =>
    setFtpRows((list) => list.map((a) => (a.id === id ? { ...a, ...patch } : a)))
  const docrootDomains = [...new Set([name, ...(domainList.data?.domains.map((d) => d.NAME) ?? [])])]

  // The aliases the SSL step last saw; it moves only once that step succeeded, so a retry still reissues
  const newStatsLogin = statsUser !== domain.STATS_USER
  const sslAliases = useRef(view.aliases)
  const aliasesChanged = !sameList(aliases, sslAliases.current)
  const sslChanged =
    sslEnabled !== initialSsl.on ||
    (sslEnabled &&
      (letsEncrypt !== initialSsl.le ||
        forceSsl !== initialSsl.force ||
        hsts !== initialSsl.hsts ||
        (letsEncrypt
          ? keyAlgo !== initialSsl.keyAlgo
          : crt !== initialSsl.crt || key !== initialSsl.key || ca !== initialSsl.ca) ||
        // changed aliases need a new Let's Encrypt certificate
        (aliasesChanged && letsEncrypt && initialSsl.le)))

  const settingsBody = (): WebEditRequest => {
    const ftp: WebFTPChange[] = ftpRows.flatMap((r): WebFTPChange[] => {
      const gone = !ftpEnabled || r.removed
      if (r.isNew) {
        if (gone || r.name === '') return []
        return [
          {
            name: r.name,
            password: r.password,
            path: r.path,
            prevPath: '',
            email: r.email,
            isNew: true,
            delete: false,
          },
        ]
      }
      return [
        {
          name: r.name,
          password: r.password,
          path: r.path,
          prevPath: r.prevPath,
          // An existing account is emailed only with a new password (owner ruling 7).
          email: r.password ? r.email : '',
          isNew: false,
          delete: gone,
        },
      ]
    })
    const useAuth = stats !== 'none' && statsAuth
    const target = !redirect ? '' : redirectMode === 'custom' ? redirectCustom : redirectMode
    return {
      ip,
      aliases: aliases.join('\n'),
      stats,
      statsAuth: useAuth,
      statsUser: useAuth ? statsUser : '',
      statsPassword: useAuth ? statsPassword : '',
      redirect,
      redirectTarget: target,
      redirectCode,
      ...(canEditTemplates
        ? {
            templates: {
              template,
              ...(options.webBackend ? { backend } : {}),
              ...(webIsNginx ? { fastcgi: { enabled: fastcgiCache, duration: fastcgiDuration } } : {}),
              ...(hasProxy ? { proxyTemplate, proxyExt: proxyExt.join('\n') } : {}),
            },
          }
        : {}),
      docroot: customDocroot,
      docrootDomain: customDocroot ? docrootDomain : '',
      docrootFolder: customDocroot ? docrootFolder : '',
      ftp: options.ftp ? ftp : [],
    }
  }

  const sslBody = (): WebSSLRequest => ({
    ssl: sslEnabled,
    letsencrypt: sslEnabled && letsEncrypt,
    keyAlgo,
    forceSsl,
    hsts,
    crt: sslEnabled && !letsEncrypt ? crt : '',
    key: sslEnabled && !letsEncrypt ? key : '',
    ca: sslEnabled && !letsEncrypt ? ca : '',
    reissue: aliasesChanged && sslEnabled && letsEncrypt && initialSsl.le,
  })

  const onSubmit = async () => {
    setError('')
    setBusy(true)
    try {
      await m.editDomain(name, settingsBody())
      // Rows the settings step applied are stored accounts now, so a retry doesn't add them again
      setFtpRows((rows) => {
        const next = rows.flatMap((r): FtpRow[] => {
          const gone = !ftpEnabled || r.removed
          if (!r.isNew) return gone ? [] : [r]
          if (gone || r.name === '') return gone ? [] : [r]
          return [{ ...r, isNew: false, prevPath: r.path, password: '', email: '' }]
        })
        return next.length ? next : [blankFtp(0)]
      })
      // SSL is its own endpoint; it runs only when its section changed, after the settings
      if (sslChanged) await m.saveSsl(name, sslBody())
      sslAliases.current = aliases
      toast.add({ title: t('Changes have been saved.'), type: 'success' })
      navigate('/web')
    } catch (err) {
      // Earlier steps stay applied and the queries refetch; the form keeps what was typed
      setError(apiErrorText(err, t))
      setBusy(false)
      scrollContentToTop()
    }
  }

  const runConfirmed = async () => {
    try {
      if (confirm === 'suspend') await m.suspendDomain(name)
      else if (confirm === 'unsuspend') await m.unsuspendDomain(name)
      else await m.purgeCache(name)
      toast.add({ title: confirmProps.done, type: 'success' })
    } catch (err) {
      toast.add({ title: apiErrorText(err, t), type: 'error' })
    }
  }

  const confirmProps = {
    suspend: {
      title: t('Suspend web domain'),
      description: `${t('Are you sure you want to suspend domain %s?', name)} ${t('Visitors will see the suspended page until it is unsuspended.')}`,
      label: t('Suspend'),
      destructive: true,
      done: t('%s suspended', name),
    },
    unsuspend: {
      title: t('Unsuspend web domain'),
      description: `${t('Are you sure you want to unsuspend domain %s?', name)} ${t('The website will be served again.')}`,
      label: t('Unsuspend'),
      destructive: false,
      done: t('%s unsuspended', name),
    },
    purge: {
      title: t('Purge NGINX cache'),
      description: t('Remove every cached page for %s? Visitors get fresh content from PHP on their next request.', name),
      label: t('Purge cache'),
      destructive: false,
      done: t('NGINX cache has been purged successfully.'),
    },
  }[confirm]

  return (
    <FormPage
      title={t('Edit web domain')}
      description={
        <span className="flex flex-wrap items-center gap-2">
          <span className="font-mono text-[0.9em]">{name}</span>
          <StatusBadge suspended={suspended} />
        </span>
      }
      back={{ href: '/web', label: t('Web domains') }}
      onSubmit={() => void onSubmit()}
      submitDisabled={busy}
      readOnly={readOnly}
      actions={
        <>
          {nginxCaching && (
            <Button type="button" icon={ArrowsClockwiseIcon} onClick={() => ask('purge')}>
              {t('Purge NGINX cache')}
            </Button>
          )}
          {options.appInstaller && (
            <LinkButton href={`/web/${name}/apps`} variant="secondary" icon={MagicWandIcon}>
              {t('Quick install app')}
            </LinkButton>
          )}
          <LinkButton href={`/web/${name}/logs`} variant="secondary" icon={FileTextIcon}>
            {t('View logs')}
          </LinkButton>
          {isAdmin && !suspended && (
            <Button type="button" icon={PauseCircleIcon} onClick={() => ask('suspend')}>
              {t('Suspend')}
            </Button>
          )}
          <Button type="button" variant="secondary-destructive" icon={TrashIcon} onClick={() => setDeleteOpen(true)}>
            {t('Delete')}
          </Button>
        </>
      }
    >
      {suspended && (
        <Banner
          variant="alert"
          icon={<WarningIcon weight="fill" />}
          title={t('This web domain is suspended')}
          description={t('Visitors see the suspended page. You can still change its settings; they take effect once it is unsuspended.')}
          action={
            isAdmin ? (
              <Banner.Action type="button" onClick={() => ask('unsuspend')}>
                {t('Unsuspend')}
              </Banner.Action>
            ) : undefined
          }
        />
      )}

      {error && (
        <Banner variant="error" icon={<WarningIcon weight="fill" />} title={error} className="whitespace-pre-line" />
      )}

      <div ref={contentRef} className="grid gap-8 xl:grid-cols-[minmax(0,1fr)_11rem]">
        <div className="grid min-w-0 gap-8">
          <Anchor id="domain">
            <FormSection title={t('Domain')} description={t('The name, extra hostnames and the IP address it listens on.')}>
              <Input label={t('Domain')} value={name} disabled description={t('The domain name cannot be changed.')} />
              <div className="grid gap-2">
                <TagField
                  label={t('Aliases')}
                  description={t('Press Enter or comma after each domain. Paste a list to add several.')}
                  placeholder={`shop.${name}`}
                  autoComplete="off"
                  value={aliases}
                  onValueChange={setAliases}
                  validateValue={(value, accepted) =>
                    isAliasHostname(value) && value !== name && !accepted.includes(value)
                  }
                />
                {letsEncrypt && sslEnabled && (
                  <Banner
                    size="sm"
                    variant="secondary"
                    icon={<InfoIcon weight="fill" />}
                    description={t("If the aliases change, Let's Encrypt will obtain a new SSL certificate.")}
                  />
                )}
              </div>
              <Select
                label={t('IP address')}
                value={ip}
                onValueChange={(v) => setIp(v ?? ip)}
                items={Object.fromEntries(options.ips.map((i) => [i.ip, i.display]))}
              />
            </FormSection>
          </Anchor>

          <Anchor id="request-flow">
            <section className="grid gap-4">
              <div className="flex flex-wrap items-end justify-between gap-x-6 gap-y-1">
                <div className="grid gap-1">
                  <Text variant="heading" as="h2">
                    {t('How requests are served')}
                  </Text>
                  <Text variant="secondary">
                    {t('The chain a request takes on this server. It follows your SSL and template choices before you save.')}
                  </Text>
                </div>
                {canEditTemplates && (
                  <button
                    type="button"
                    className="cursor-pointer text-kumo-link hover:underline"
                    onClick={() => jumpTo('templates', true)}
                  >
                    {t('Change templates')}
                  </button>
                )}
              </div>
              <LayerCard className="px-5 py-4">
                <RequestFlow
                  domain={name}
                  ssl={sslEnabled}
                  letsEncrypt={letsEncrypt}
                  proxy={hasProxy}
                  webSystem={options.webSystem}
                  webBackend={options.webBackend}
                  proxySystem={options.proxySystem}
                  proxyTemplate={proxyTemplate}
                  fastcgiCache={fastcgiCache}
                  template={template}
                  backend={backend}
                />
              </LayerCard>
            </section>
          </Anchor>

          <Anchor id="statistics">
            <FormSection title={t('Web statistics')} description={t('Traffic reports published at /vstats/ on this domain.')}>
              <Select
                label={t('Web statistics')}
                value={stats}
                onValueChange={(v) => setStats(v ?? stats)}
                items={toItems(options.statsTypes, (v) => (v === 'none' ? t('None') : v === 'awstats' ? 'AWStats' : v))}
              />
              {stats !== 'none' && (
                <>
                  <Checkbox label={t('Statistics authorization')} checked={statsAuth} onCheckedChange={setStatsAuth} />
                  {statsAuth && (
                    <div className="grid items-start gap-4 sm:grid-cols-2">
                      <Input label={t('Username')} value={statsUser} onChange={(e) => setStatsUser(e.target.value)} />
                      <PasswordInput
                        label={t('Password')}
                        value={statsPassword}
                        onChange={setStatsPassword}
                        required={newStatsLogin}
                        placeholder={newStatsLogin ? '' : t('Unchanged')}
                      />
                    </div>
                  )}
                </>
              )}
            </FormSection>
          </Anchor>

          <Anchor id="redirection">
            <FormSection
              title={t('Redirection')}
              description={t('Send every visitor to one canonical address.')}
              aside={<Switch label={t('Enable domain redirection')} checked={redirect} onCheckedChange={setRedirect} />}
            >
              {redirect ? (
                <>
                  <Radio.Group
                    legend={t('Redirect visitors to')}
                    value={redirectMode}
                    onValueChange={(v) => setRedirectMode(String(v))}
                  >
                    <Radio.Item label={t('Redirect visitors to %s', `www.${name}`)} value={`www.${name}`} />
                    <Radio.Item label={t('Redirect visitors to %s', name)} value={name} />
                    <Radio.Item label={t('Redirect visitors to a custom domain or web address')} value="custom" />
                  </Radio.Group>
                  {redirectMode === 'custom' && (
                    <Input
                      label={t('Target domain or URL')}
                      placeholder="https://acme-bakery.com/shop"
                      value={redirectCustom}
                      onChange={(e) => setRedirectCustom(e.target.value)}
                    />
                  )}
                  <Select
                    label={t('Status code')}
                    className="sm:w-48"
                    value={redirectCode}
                    onValueChange={(v) => setRedirectCode(v ?? redirectCode)}
                    items={{ '301': t('301 (permanent)'), '302': t('302 (temporary)') }}
                  />
                </>
              ) : (
                <Text variant="secondary">{t('Visitors reach the site on whichever alias they typed.')}</Text>
              )}
            </FormSection>
          </Anchor>

          <Anchor id="ssl">
            <FormSection
              title="SSL"
              description={t("Serve the site over HTTPS with a Let's Encrypt or your own certificate.")}
              aside={<Switch label={t('Enable SSL for this domain')} checked={sslEnabled} onCheckedChange={setSslEnabled} />}
            >
              {sslEnabled ? (
                <>
                  <Checkbox
                    label={t("Use Let's Encrypt to obtain SSL certificate")}
                    checked={letsEncrypt}
                    onCheckedChange={(v) => {
                      setLetsEncrypt(v)
                      setShowCertificates(!v)
                    }}
                  />
                  {letsEncrypt && (
                    <Select
                      label={t('Key type')}
                      className="sm:w-72"
                      value={keyAlgo}
                      onValueChange={(v) => setKeyAlgo(v ?? keyAlgo)}
                      items={{ ...KEY_TYPES, 'ecdsa-256': t(KEY_TYPES['ecdsa-256']) }}
                    />
                  )}
                  {letsEncrypt && Number(domain.LETSENCRYPT_FAIL_COUNT) > 0 && (
                    <Banner
                      size="sm"
                      variant="error"
                      description={t(
                        "Let's Encrypt has failed %s times for this domain. Check that every alias points to %s.",
                        domain.LETSENCRYPT_FAIL_COUNT,
                        domain.IP,
                      )}
                    />
                  )}
                  <Checkbox
                    label={t('Enable automatic HTTPS redirection')}
                    checked={forceSsl}
                    onCheckedChange={setForceSsl}
                  />
                  <Checkbox
                    label={
                      <span>
                        {t('Enable HTTP Strict Transport Security (HSTS)')}{' '}
                        <Link href="https://en.wikipedia.org/wiki/HTTP_Strict_Transport_Security" target="_blank">
                          {t('What is HSTS?')}
                        </Link>
                      </span>
                    }
                    checked={hsts}
                    onCheckedChange={setHsts}
                  />

                  {ssl && domain.SSL === 'yes' && (
                    <div className="grid gap-3 border-t border-kumo-line pt-4">
                      <div className="flex items-center justify-between gap-2">
                        <Text bold as="span">
                          {t('Current certificate')}
                        </Text>
                        <Link href={`/web/${name}/ssl`}>{t('Certificate details')}</Link>
                      </div>
                      <ValuesList
                        items={[
                          { label: t('Issued to'), value: ssl.SUBJECT },
                          ...(ssl.ALIASES ? [{ label: t('Alternate'), value: ssl.ALIASES.split(',').join(', ') }] : []),
                          { label: t('Not before'), value: ssl.NOT_BEFORE },
                          { label: t('Not after'), value: ssl.NOT_AFTER },
                          { label: t('Signature'), value: ssl.SIGNATURE },
                          { label: t('Key size'), value: ssl.PUB_KEY },
                          { label: t('Issued by'), value: ssl.ISSUER },
                        ]}
                      />
                      {letsEncrypt && (
                        <button
                          type="button"
                          className="w-fit cursor-pointer text-kumo-link hover:underline"
                          onClick={() => setShowCertificates((v) => !v)}
                        >
                          {showCertificates ? t('Hide certificate') : t('Show certificate')}
                        </button>
                      )}
                    </div>
                  )}

                  {showCertificates && (
                    <div className="grid gap-4 border-t border-kumo-line pt-4">
                      <InputArea
                        label={t('SSL certificate')}
                        description={
                          !letsEncrypt && (
                            <Link href={`/web/${name}/ssl/generate`}>{t('Generate self-signed SSL certificate')}</Link>
                          )
                        }
                        required={!letsEncrypt}
                        value={crt}
                        onValueChange={setCrt}
                        rows={5}
                        className="font-mono text-[13px]"
                        readOnly={letsEncrypt}
                      />
                      <InputArea
                        label={t('SSL private key')}
                        required={!letsEncrypt}
                        value={key}
                        onValueChange={setKey}
                        rows={5}
                        className="font-mono text-[13px]"
                        readOnly={letsEncrypt}
                      />
                      <InputArea
                        label={
                          <span>
                            {t('SSL certificate authority / intermediate')}{' '}
                            <span className="font-normal text-kumo-subtle">({t('optional')})</span>
                          </span>
                        }
                        value={ca}
                        onValueChange={setCa}
                        rows={5}
                        className="font-mono text-[13px]"
                        readOnly={letsEncrypt}
                      />
                    </div>
                  )}
                </>
              ) : (
                <Text variant="secondary">{t('The site is served over plain HTTP only.')}</Text>
              )}
            </FormSection>
          </Anchor>

          <Collapsible.Root open={advanced} onOpenChange={setAdvanced}>
            <Collapsible.DefaultTrigger>{t('Advanced options')}</Collapsible.DefaultTrigger>
            <Collapsible.Panel>
              <div className="grid gap-8 pt-4">
                {canEditTemplates && (
                  <Anchor id="templates">
                    <FormSection
                      title={t('Server templates')}
                      description={t('How the web server, PHP and the NGINX proxy handle this domain.')}
                    >
                      <Select
                        label={
                          <span>
                            {t('Web template')}{' '}
                            <span className="font-normal text-kumo-subtle">{options.webSystem.toUpperCase()}</span>
                          </span>
                        }
                        value={template}
                        onValueChange={(v) => setTemplate(v ?? template)}
                        items={toItems(options.templates)}
                      />
                      {webIsNginx && (
                        <div className="grid gap-3">
                          <Checkbox
                            label={
                              <span>
                                {t('Enable FastCGI cache')}{' '}
                                <Link
                                  href="https://hestiacp.com/docs/server-administration/web-templates.html#nginx-fastcgi-cache"
                                  target="_blank"
                                >
                                  {t('Learn more')}
                                </Link>
                              </span>
                            }
                            checked={fastcgiCache}
                            onCheckedChange={setFastcgiCache}
                          />
                          {fastcgiCache && (
                            <Input
                              label={t('Cache duration')}
                              description={t('For example: 30s, 10m or 1d')}
                              className="sm:w-48"
                              value={fastcgiDuration}
                              onChange={(e) => setFastcgiDuration(e.target.value)}
                            />
                          )}
                        </div>
                      )}
                      {options.webBackend && (
                        <Select
                          label={
                            <span>
                              {t('Backend template')}{' '}
                              <span className="font-normal text-kumo-subtle">{options.webBackend.toUpperCase()}</span>
                            </span>
                          }
                          description={t('Selects the PHP version for this domain.')}
                          value={backend}
                          onValueChange={(v) => setBackend(v ?? backend)}
                          items={toItems(options.backendTemplates, formatBackend)}
                        />
                      )}
                      {hasProxy && (
                        <div className="grid gap-4 border-t border-kumo-line pt-4">
                          <Text bold as="span">
                            {t('Proxy support')} <span className="text-kumo-subtle">{options.proxySystem.toUpperCase()}</span>
                          </Text>
                          <Select
                            label={t('Proxy template')}
                            value={proxyTemplate}
                            onValueChange={(v) => setProxyTemplate(v ?? proxyTemplate)}
                            items={toItems(options.proxyTemplates)}
                          />
                          <TagField
                            label={t('Proxy extensions')}
                            description={t('Static files with these extensions are served by NGINX directly.')}
                            placeholder="webp"
                            autoComplete="off"
                            value={proxyExt}
                            onValueChange={setProxyExt}
                            validateValue={(value, accepted) => isExtension(value) && !accepted.includes(value)}
                          />
                        </div>
                      )}
                    </FormSection>
                  </Anchor>
                )}

                <Anchor id="document-root">
                  <FormSection
                    title={t('Custom document root')}
                    description={t("Serve this domain from another domain's folder, or a sub-folder of its own.")}
                    aside={
                      <Switch label={t('Custom document root')} checked={customDocroot} onCheckedChange={setCustomDocroot} />
                    }
                  >
                    {customDocroot ? (
                      <>
                        <Select
                          label={t('Point to')}
                          value={docrootDomain}
                          onValueChange={(v) => setDocrootDomain(v ?? docrootDomain)}
                          items={toItems(docrootDomains)}
                        />
                        <Input
                          label={
                            <span>
                              {t('Directory')} <span className="font-normal text-kumo-subtle">({t('optional')})</span>
                            </span>
                          }
                          placeholder="staging"
                          value={docrootFolder}
                          onChange={(e) => setDocrootFolder(e.target.value)}
                          description={
                            <span>
                              {interpolate(
                                t('Document root: %s'),
                                <span className="font-mono text-[0.9em] break-all">{docrootPreview}</span>,
                              )}
                            </span>
                          }
                        />
                      </>
                    ) : (
                      <Text variant="secondary">
                        {interpolate(
                          t('Files are served from %s'),
                          <span className="font-mono text-[0.9em] break-all">{domain.DOCUMENT_ROOT}</span>,
                        )}
                      </Text>
                    )}
                  </FormSection>
                </Anchor>

                {options.ftp && (
                  <Anchor id="ftp">
                    <FormSection
                      title={t('FTP accounts')}
                      description={t('Extra FTP logins limited to a folder of this domain.')}
                      aside={
                        <Switch
                          label={t('Additional FTP account(s)')}
                          checked={ftpEnabled}
                          onCheckedChange={setFtpEnabled}
                        />
                      }
                    >
                      {ftpEnabled ? (
                        <>
                          {visibleFtp.map((account, i) => (
                            <fieldset
                              key={account.id}
                              aria-labelledby={`ftp-legend-${account.id}`}
                              className={`grid gap-4 ${i > 0 ? 'border-t border-kumo-line pt-4' : ''}`}
                            >
                              <div className="flex items-center justify-between">
                                <Text bold as="span" id={`ftp-legend-${account.id}`}>
                                  {t('FTP #%s', i + 1)}
                                </Text>
                                <Button
                                  type="button"
                                  size="sm"
                                  variant="ghost"
                                  icon={TrashIcon}
                                  className="text-kumo-danger"
                                  onClick={() =>
                                    setFtpRows((list) =>
                                      account.isNew
                                        ? list.filter((a) => a.id !== account.id)
                                        : list.map((a) => (a.id === account.id ? { ...a, removed: true } : a)),
                                    )
                                  }
                                >
                                  {t('Delete')}
                                </Button>
                              </div>
                              <div className="grid items-start gap-4 sm:grid-cols-2">
                                <Input
                                  label={t('Username')}
                                  value={account.name}
                                  disabled={!account.isNew}
                                  onChange={(e) =>
                                    updateFtp(account.id, { name: e.target.value.replace(/[^A-Za-z0-9_]/g, '') })
                                  }
                                  description={
                                    account.name
                                      ? `${t('Login')}: ${ftpPrefix}${account.name}`
                                      : t('Prefix %s will be added to username automatically', ftpPrefix)
                                  }
                                />
                                <PasswordInput
                                  label={t('Password')}
                                  value={account.password}
                                  required={account.isNew}
                                  placeholder={account.isNew ? '' : t('Unchanged')}
                                  onChange={(v) => updateFtp(account.id, { password: v })}
                                />
                              </div>
                              <Input
                                label={t('Path')}
                                placeholder="/"
                                value={account.path}
                                onChange={(e) => updateFtp(account.id, { path: e.target.value })}
                                description={
                                  <span className="font-mono text-[0.9em] break-all">
                                    {options.webRoot}
                                    {ftpHintDomain}
                                    {account.path ? `/${account.path.replace(/^\/+/, '')}` : ''}
                                  </span>
                                }
                              />
                              {(account.isNew || account.password !== '') && (
                                <Input
                                  label={t('Send FTP credentials to email')}
                                  type="email"
                                  placeholder="owner@acme-bakery.com"
                                  value={account.email}
                                  onChange={(e) => updateFtp(account.id, { email: e.target.value })}
                                />
                              )}
                            </fieldset>
                          ))}
                          <Button
                            type="button"
                            icon={PlusIcon}
                            className="w-fit"
                            onClick={() =>
                              setFtpRows((list) => [...list, blankFtp(Math.max(-1, ...list.map((a) => a.id)) + 1)])
                            }
                          >
                            {t('Add FTP account')}
                          </Button>
                        </>
                      ) : (
                        <Text variant="secondary">
                          {t('The account owner can always reach these files with their main FTP and SFTP login.')}
                        </Text>
                      )}
                    </FormSection>
                  </Anchor>
                )}
              </div>
            </Collapsible.Panel>
          </Collapsible.Root>
        </div>

        <nav aria-label={t('On this page')} className="hidden xl:block">
          <div className="sticky top-6">
            <TableOfContents>
              <TableOfContents.Title className="normal-case">{t('On this page')}</TableOfContents.Title>
              <TableOfContents.List>
                {visibleSections.map((section) => (
                  <TableOfContents.Item
                    key={section.id}
                    active={activeId === section.id}
                    className="cursor-pointer"
                    render={<a href={`#${section.id}`} />}
                    onClick={(e: React.MouseEvent) => {
                      e.preventDefault()
                      jumpTo(section.id, section.advanced)
                    }}
                  >
                    {t(section.label)}
                  </TableOfContents.Item>
                ))}
              </TableOfContents.List>
            </TableOfContents>
          </div>
        </nav>
      </div>

      <DeleteResource
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        resourceType={t('web domain')}
        resourceName={name}
        deleteButtonText={t('Delete web domain')}
        onDelete={async () => {
          try {
            await m.deleteDomain(name)
            setDeleteOpen(false)
            toast.add({ title: t('%s has been deleted', name), type: 'success' })
            navigate('/web')
          } catch (err) {
            setDeleteOpen(false)
            toast.add({ title: apiErrorText(err, t), type: 'error' })
          }
        }}
      />

      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={confirmProps.title}
        description={confirmProps.description}
        confirmLabel={confirmProps.label}
        destructive={confirmProps.destructive}
        onConfirm={runConfirmed}
      />
    </FormPage>
  )
}
