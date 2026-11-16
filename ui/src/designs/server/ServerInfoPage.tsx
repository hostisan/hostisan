import { Banner, Button, LinkButton, Loader, Tabs, Text } from '@cloudflare/kumo'
import { ArrowClockwiseIcon, ChartLineIcon, WarningCircleIcon } from '@phosphor-icons/react'
import { useMemo, useState } from 'react'
import { apiErrorText } from '../../api/errors'
import { useSettings, useStatus } from '../../api/server'
import { PageHeader } from '../../components/PageHeader'
import { useT } from '../../i18n/I18nProvider'
import { interpolate } from '../../lib/interpolate'

const commands: Record<string, string> = {
  cpu: 'v-list-sys-cpu-status',
  mem: 'v-list-sys-memory-status',
  disk: 'v-list-sys-disk-status',
  net: 'v-list-sys-network-status',
  web: 'v-list-sys-web-status',
  dns: 'v-list-sys-dns-status',
  mail: 'v-list-sys-mail-status',
  db: 'v-list-sys-db-status',
}

/** A *_SYSTEM value names installed software unless it is empty or "no" (as the API's status check). */
const installed = (system: string | undefined) => !!system && system !== 'no'

export function ServerInfoPage() {
  const t = useT()
  const settings = useSettings()
  const [chosen, setChosen] = useState('cpu')

  // CPU, RAM, disk and network always exist; the others only when that system is installed.
  const tabs = useMemo(() => {
    const s = settings.data
    return [
      { value: 'cpu', label: t('CPU') },
      { value: 'mem', label: t('RAM') },
      { value: 'disk', label: t('Disk') },
      { value: 'net', label: t('Network') },
      ...(installed(s?.webSystem) ? [{ value: 'web', label: t('Web') }] : []),
      // dnsStatus is off on RHEL, where the status script rewrites named.conf (until Plan 6).
      ...(s?.dnsStatus ? [{ value: 'dns', label: t('DNS') }] : []),
      ...(installed(s?.mail.system) ? [{ value: 'mail', label: t('Mail') }] : []),
      ...(installed(s?.db.system) ? [{ value: 'db', label: t('DB') }] : []),
    ]
  }, [settings.data, t])
  const tab = tabs.some((x) => x.value === chosen) ? chosen : 'cpu'
  const status = useStatus(tab)

  return (
    <div className="grid gap-6">
      <PageHeader
        title={t('Server information')}
        description={t('Live status output from this server.')}
        actions={
          <>
            <LinkButton href="/server/graphs" variant="secondary" icon={ChartLineIcon}>
              {t('Graphs')}
            </LinkButton>
            <Button icon={ArrowClockwiseIcon} loading={status.isFetching} onClick={() => void status.refetch()}>
              {t('Refresh')}
            </Button>
          </>
        }
      />

      <div className="grid gap-3">
        {settings.error ? (
          <Text variant="secondary" size="sm">
            {t('Some tabs may be missing: the installed systems could not be read.')} {apiErrorText(settings.error, t)}
          </Text>
        ) : null}
        <div className="-mx-4 overflow-x-auto px-4 sm:mx-0 sm:px-0">
          <Tabs variant="underline" value={tab} onValueChange={setChosen} tabs={tabs} className="min-w-max" />
        </div>
        <Text variant="secondary" size="sm">
          {interpolate(t('Output of %s'), <span className="font-mono text-[0.9em]">{commands[tab]}</span>)}
        </Text>
        {status.error ? (
          <div role="alert">
            <Banner variant="error" icon={<WarningCircleIcon />} title={apiErrorText(status.error, t)} className="whitespace-pre-line" />
          </div>
        ) : !status.data ? (
          <Loader />
        ) : (
          // Plain text only: the API strips markup from the dumps, and React escapes the rest.
          <pre className="min-w-0 overflow-x-auto rounded-lg bg-kumo-tint px-4 py-3 font-mono text-[13px] leading-5 whitespace-pre ring ring-kumo-line">
            {status.data.text}
          </pre>
        )}
      </div>
    </div>
  )
}
