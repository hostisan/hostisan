import { Badge, Button, Empty, LayerCard, LinkButton, Tabs, Text } from '@cloudflare/kumo'
import { MagicWandIcon } from '@phosphor-icons/react'
import { useMemo, useState } from 'react'
import { useParams } from 'react-router'
import { PageHeader } from '../../components/PageHeader'
import { SearchInput } from '../../components/SearchInput'
import { ApiError } from '../../api/client'
import type { WebAppInfo } from '../../api/types.gen'
import { useWebApps } from '../../api/web'
import { useT } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { interpolate } from '../../lib/interpolate'
import { DnsError, DnsLoading } from '../dns/shared'
import { DomainNotFound } from './shared'

export const appGroupLabels: Record<string, string> = {
  cms: msgid('CMS'),
  ecommerce: msgid('E-commerce'),
  framework: msgid('Framework'),
  wiki: msgid('Wiki'),
  forum: msgid('Forum'),
  cloud: msgid('Cloud'),
  crm: msgid('CRM'),
  helpdesk: msgid('Helpdesk'),
  podcasting: msgid('Podcasting'),
}

const TABS = [
  { value: 'all', label: msgid('All') },
  { value: 'cms', label: msgid('CMS') },
  { value: 'ecommerce', label: msgid('E-commerce') },
  { value: 'framework', label: msgid('Frameworks') },
  { value: 'community', label: msgid('Community') },
  { value: 'business', label: msgid('Business') },
]

const TAB_GROUPS: Record<string, string[]> = {
  community: ['wiki', 'forum', 'podcasting'],
  business: ['cloud', 'crm', 'helpdesk'],
}

export function AppMark({ name, size = 'md' }: { name: string; size?: 'md' | 'lg' }) {
  const dims = size === 'lg' ? 'size-12 text-lg rounded-xl' : 'size-10 text-base rounded-lg'
  return (
    <span
      aria-hidden
      className={`flex shrink-0 items-center justify-center bg-kumo-tint font-semibold text-kumo-strong ring ring-kumo-line ${dims}`}
    >
      {name.slice(0, 1)}
    </span>
  )
}

export function WebAppsPage() {
  const t = useT()
  const { domain: name = '' } = useParams()
  // The catalogue answers 404 for an unknown domain, so the edit view isn't needed
  const catalogue = useWebApps(name)
  const [query, setQuery] = useState('')
  const [tab, setTab] = useState('all')

  const all = useMemo<WebAppInfo[]>(() => catalogue.data?.apps ?? [], [catalogue.data])
  const apps = useMemo(() => {
    const q = query.trim().toLowerCase()
    return all.filter((app) => {
      const inTab = tab === 'all' || app.group === tab || TAB_GROUPS[tab]?.includes(app.group)
      return inTab && (!q || app.name.toLowerCase().includes(q))
    })
  }, [all, query, tab])

  const status = catalogue.error instanceof ApiError ? catalogue.error.status : 0
  if (status === 404) return <DomainNotFound name={name} />

  return (
    <div className="grid gap-6 pb-4">
      <PageHeader
        title={t('Quick install app')}
        description={
          <span>
            {interpolate(
              t('Install an application into %s. The installer creates the database and sets the right web template.'),
              <span className="font-mono text-[0.9em]">{name}</span>,
            )}
          </span>
        }
        back={{ href: `/web/${name}`, label: name }}
      />

      {status === 403 ? (
        <LayerCard className="p-0">
          <Empty
            icon={<MagicWandIcon size={40} className="text-kumo-inactive" />}
            title={t('Quick install is turned off')}
            description={t('The administrator has disabled the application installer on this server.')}
            className="py-10"
          />
        </LayerCard>
      ) : catalogue.isPending ? (
        <DnsLoading />
      ) : catalogue.isError ? (
        <DnsError error={catalogue.error} />
      ) : (
        <>
          <div className="flex flex-wrap items-center gap-3">
            <SearchInput label={t('Search apps')} placeholder={t('Search apps')} value={query} onChange={setQuery} />
            <div className="max-w-full overflow-x-auto">
              <Tabs variant="segmented" tabs={TABS.map((tb) => ({ ...tb, label: t(tb.label) }))} value={tab} onValueChange={setTab} />
            </div>
          </div>

          {apps.length === 0 ? (
            <LayerCard className="p-0">
              <Empty
                icon={<MagicWandIcon size={40} className="text-kumo-inactive" />}
                title={t('No apps match your search')}
                description={t('Try another name, or look in a different category.')}
                contents={
                  <Button
                    onClick={() => {
                      setQuery('')
                      setTab('all')
                    }}
                  >
                    {t('Show all apps')}
                  </Button>
                }
                className="py-10"
              />
            </LayerCard>
          ) : (
            <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
              {apps.map((app) => (
                <LayerCard key={app.id} className="flex flex-col gap-3 px-5 py-4">
                  <div className="flex items-start gap-3">
                    <AppMark name={app.name} />
                    <div className="grid min-w-0 gap-0.5">
                      <Text variant="heading" as="h2">
                        {app.name}
                      </Text>
                      <Text variant="secondary">
                        {t('Version')}: {app.version}
                      </Text>
                    </div>
                    <Badge variant="neutral" className="ml-auto">
                      {appGroupLabels[app.group] ? t(appGroupLabels[app.group]) : app.group}
                    </Badge>
                  </div>
                  <div className="mt-auto flex items-center justify-between gap-2 pt-1">
                    <Text variant="secondary">
                      {app.installable ? '' : t('No supported PHP version is installed')}
                    </Text>
                    {app.installable ? (
                      <LinkButton href={`/web/${name}/apps/${app.id}`} variant="secondary" size="sm">
                        {t('Setup')}
                      </LinkButton>
                    ) : (
                      <Button variant="secondary" size="sm" disabled>
                        {t('Setup')}
                      </Button>
                    )}
                  </div>
                </LayerCard>
              ))}
            </div>
          )}
        </>
      )}
    </div>
  )
}
