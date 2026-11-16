import type { DateRange } from '../../components/fields'
import { ALL_DATES, DateRangeFilter, monthInRange } from '../../components/fields'
import { LayerCard, LinkButton, Select, Table, Text, TimeseriesChart } from '@cloudflare/kumo'
import { ChartBarIcon } from '@phosphor-icons/react'
import * as echarts from 'echarts'
import { useEffect, useMemo, useState, type ComponentProps } from 'react'
import { useSearchParams } from 'react-router'
import { useStats } from '../../api/account'
import type { StatsMonth } from '../../api/types.gen'
import { PageHeader } from '../../components/PageHeader'
import { TableCard } from '../../components/table'
import type { Translate } from '../../i18n/I18nProvider'
import { useT } from '../../i18n/I18nProvider'
import { MONTHS } from '../../lib/date'
import { formatMB, usagePercent } from '../../lib/format'
import { useSessionUser } from '../../session/SessionProvider'
import { DnsError, DnsLoading } from '../dns/shared'

const NONE = '__none'

/** Megabytes humanized; Hestia's "unlimited" is shown as ∞. */
const mb = (v: string) => (v === 'unlimited' ? '∞' : formatMB(v))

type TimeseriesData = ComponentProps<typeof TimeseriesChart>['data'][number]

/** A UTC timestamp's month as Hestia's translated short name, e.g. "Oct". */
const monthName = (v: number, t: Translate) => t(MONTHS[new Date(v).getUTCMonth()])

/** "2026-10-31" as "Oct 2026", the month translated. */
const monthYear = (ymd: string, t: Translate) => {
  const d = Date.parse(ymd)
  return Number.isNaN(d) ? ymd : `${monthName(d, t)} ${new Date(d).getUTCFullYear()}`
}

function useDarkMode() {
  const read = () => document.documentElement.dataset.mode === 'dark'
  const [dark, setDark] = useState(read)
  useEffect(() => {
    const observer = new MutationObserver(() => setDark(read()))
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['data-mode'] })
    return () => observer.disconnect()
  }, [])
  return dark
}

// Literal class names so Tailwind emits the --color-cf-sequential-* variables we read below.
const SWATCHES = [
  'bg-cf-sequential-0',
  'bg-cf-sequential-1',
  'bg-cf-sequential-2',
  'bg-cf-sequential-3',
  'bg-cf-sequential-4',
  'bg-cf-sequential-5',
]

function Legend({ series }: { series: TimeseriesData[] }) {
  return (
    <ul className="flex flex-wrap gap-x-4 gap-y-1">
      {series.map((s) => (
        <li key={s.name} className="flex items-center gap-1.5 text-sm text-kumo-subtle">
          <span className="size-2.5 rounded-full" style={{ backgroundColor: s.color }} />
          {s.name}
        </li>
      ))}
    </ul>
  )
}

// TimeseriesChart wants hex strings, so resolve the theme's sequential tokens.
function useSequentialColors(count: number, dark: boolean) {
  return useMemo(() => {
    const styles = getComputedStyle(document.documentElement)
    return Array.from({ length: count }, (_, i) => styles.getPropertyValue(`--color-cf-sequential-${i}`).trim() || '#005de0')
    // dark: the sequential tokens differ per theme, so read them again when it changes
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [count, dark])
}

function ChartCard({ title, description, children }: { title: string; description: string; children: React.ReactNode }) {
  return (
    <LayerCard className="grid min-w-0 content-start gap-4 px-5 py-4">
      <div className="grid gap-1">
        <Text variant="heading" as="h2">
          {title}
        </Text>
        <Text variant="secondary">{description}</Text>
      </div>
      {children}
      <span className={`hidden ${SWATCHES.join(' ')}`} />
    </LayerCard>
  )
}

function Usage({ used, quota }: { used: string; quota: string }) {
  const pct = usagePercent(used, quota)
  return (
    <span className="tabular-nums">
      {mb(used)}
      <span className="text-kumo-subtle">
        {' '}
        / {mb(quota)}
        {pct !== null && ` (${pct}%)`}
      </span>
    </span>
  )
}

export function StatsPage() {
  const t = useT()
  const user = useSessionUser()
  // An admin looking at another account sees that account, like a user, and gets no ?user=.
  const isAdmin = user?.role === 'admin' && !user.look
  const [params, setParams] = useSearchParams()
  const selected = isAdmin ? (params.get('user') ?? '') : ''
  const stats = useStats(selected || undefined)
  const [range, setRange] = useState<DateRange>(ALL_DATES)
  const dark = useDarkMode()
  const colors = useSequentialColors(6, dark)

  const overall = stats.data?.overall ?? false
  // The API sends the newest month first.
  const newestFirst = useMemo(() => stats.data?.months ?? [], [stats.data])
  const inRangeMonths = useMemo(() => newestFirst.filter((m) => monthInRange(m.date, range)), [newestFirst, range])
  const months = inRangeMonths
  const latest: StatsMonth | undefined = months[0]
  const oldestFirst = useMemo(() => [...months].reverse(), [months])

  const series = useMemo(() => {
    const line = (name: string, key: keyof StatsMonth, color: string): TimeseriesData => ({
      name,
      color,
      data: oldestFirst.map((m) => [Date.parse(m.date), Number(m[key]) || 0]),
    })
    return {
      disk: [
        line(t('Web'), 'uDiskWeb', colors[5]),
        line(t('Mail'), 'uDiskMail', colors[1]),
        line(t('Databases'), 'uDiskDb', colors[3]),
        line(t('User directory'), 'uDiskDirs', colors[2]),
      ],
      bandwidth: [line(t('Bandwidth'), 'uBandwidth', colors[5])],
      objects: [
        line(t('Web domains'), 'uWebDomains', colors[5]),
        line(t('DNS zones'), 'uDnsDomains', colors[1]),
        line(t('Mail domains'), 'uMailDomains', colors[3]),
        line(t('Databases'), 'uDatabases', colors[2]),
      ],
    }
  }, [oldestFirst, colors, t])

  const tick = (v: number) => monthName(v, t)
  const axisMb = (v: number) => formatMB(String(Math.round(v)))

  const perUser = {
    [NONE]: t('Show Per User'),
    ...Object.fromEntries((stats.data?.users ?? []).map((u) => [u, t('User: %s', u)])),
  }

  return (
    <div className="grid gap-6">
      <PageHeader
        title={t('Statistics')}
        description={
          !stats.data
            ? t('Usage recorded on the first day of each month.')
            : overall
              ? t('Server-wide usage, recorded on the first day of each month.')
              : t('Usage for %s, recorded on the first day of each month.', stats.data.user)
        }
        actions={
          <>
            <DateRangeFilter value={range} onChange={setRange} granularity="month" toolbar={false} />
            {isAdmin && (
              <>
                <LinkButton href="/stats" variant={selected === '' ? 'primary' : 'secondary'}>
                  {t('Overall Statistics')}
                </LinkButton>
                <Select
                  aria-label={t('Show Per User')}
                  className="w-52"
                  value={selected || NONE}
                  onValueChange={(v) => setParams(v === NONE ? {} : { user: String(v) })}
                  items={perUser}
                />
              </>
            )}
          </>
        }
      />

      {stats.isPending ? (
        <DnsLoading />
      ) : stats.isError ? (
        <DnsError error={stats.error} />
      ) : !latest ? (
        <TableCard
          noun={t('months')}
          filteredCount={0}
          totalCount={0}
          emptyState={{
            icon: <ChartBarIcon size={40} className="text-kumo-inactive" />,
            title: newestFirst.length ? t('No statistics in this range') : t('No statistics yet'),
            description: newestFirst.length
              ? t('Pick a wider date range to see more months.')
              : t('Usage is recorded on the first day of each month.'),
          }}
        >
          {null}
        </TableCard>
      ) : (
        <>
          <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
            {[
              [t('Disk'), mb(latest.uDisk), overall ? '' : t('of %s', mb(latest.diskQuota))],
              [t('Bandwidth'), mb(latest.uBandwidth), overall ? '' : t('of %s', mb(latest.bandwidth))],
              [t('Web domains'), latest.uWebDomains, t('%s with SSL · %s aliases', latest.uWebSsl, latest.uWebAliases)],
              [t('Mail accounts'), latest.uMailAccounts, t('on %s mail domains', latest.uMailDomains)],
            ].map(([label, value, hint]) => (
              <LayerCard key={label} className="grid gap-1 px-5 py-4">
                <Text variant="secondary" size="sm">
                  {label}
                </Text>
                <Text variant="heading" size="lg" as="p" DANGEROUS_className="tabular-nums">
                  {value}
                </Text>
                <Text variant="secondary" size="sm">
                  {hint}
                </Text>
              </LayerCard>
            ))}
          </div>

          <div className="grid gap-4 lg:grid-cols-2">
            <ChartCard
              title={t('Disk usage')}
              description={t('Space used by web files, mail, databases and the home directory.')}
            >
              <TimeseriesChart
                echarts={echarts}
                isDarkMode={dark}
                data={series.disk}
                height={260}
                xAxisTickFormat={tick}
                yAxisTickFormat={axisMb}
                tooltipValueFormat={axisMb}
                ariaDescription={t('Monthly disk usage by type')}
              />
              <Legend series={series.disk} />
            </ChartCard>
            <ChartCard title={t('Bandwidth')} description={t('Traffic served each month.')}>
              <TimeseriesChart
                echarts={echarts}
                isDarkMode={dark}
                type="bar"
                data={series.bandwidth}
                height={260}
                xAxisTickFormat={tick}
                yAxisTickFormat={axisMb}
                tooltipValueFormat={axisMb}
                ariaDescription={t('Monthly bandwidth')}
              />
            </ChartCard>
          </div>

          <ChartCard
            title={t('Hosted objects')}
            description={t('Web domains, DNS zones, mail domains and databases over time.')}
          >
            <TimeseriesChart
              echarts={echarts}
              isDarkMode={dark}
              data={series.objects}
              height={240}
              xAxisTickFormat={tick}
              yAxisMinInterval={1}
              ariaDescription={t('Monthly counts of web domains, DNS zones, mail domains and databases')}
            />
            <Legend series={series.objects} />
          </ChartCard>

          <div className="grid gap-3">
            <Text variant="heading" as="h2">
              {t('Monthly breakdown')}
            </Text>
            <TableCard
              noun={t('months')}
              filteredCount={months.length}
              totalCount={months.length}
              emptyState={{
                icon: <ChartBarIcon size={40} className="text-kumo-inactive" />,
                title: t('No statistics yet'),
                description: t('Usage is recorded on the first day of each month.'),
              }}
            >
              <Table className="whitespace-nowrap">
                <Table.Header>
                  <Table.Row>
                    <Table.Head sticky="left">{t('Month')}</Table.Head>
                    {!overall && <Table.Head>{t('Package')}</Table.Head>}
                    {overall && <Table.Head className="text-right">{t('Users')}</Table.Head>}
                    <Table.Head>{t('Disk')}</Table.Head>
                    <Table.Head className="text-right">{t('Web')}</Table.Head>
                    <Table.Head className="text-right">{t('Mail')}</Table.Head>
                    <Table.Head className="text-right">{t('Databases')}</Table.Head>
                    <Table.Head className="text-right">{t('User directory')}</Table.Head>
                    <Table.Head>{t('Bandwidth')}</Table.Head>
                    <Table.Head className="text-right">{t('Web domains')}</Table.Head>
                    <Table.Head className="text-right">{t('SSL domains')}</Table.Head>
                    <Table.Head className="text-right">{t('Web aliases')}</Table.Head>
                    <Table.Head className="text-right">{t('DNS zones')}</Table.Head>
                    <Table.Head className="text-right">{t('DNS records')}</Table.Head>
                    <Table.Head className="text-right">{t('Mail domains')}</Table.Head>
                    <Table.Head className="text-right">{t('Mail accounts')}</Table.Head>
                    <Table.Head className="text-right">{t('Databases')}</Table.Head>
                    <Table.Head className="text-right">{t('Cron jobs')}</Table.Head>
                    <Table.Head className="text-right">{t('Backups')}</Table.Head>
                    <Table.Head className="text-right">{t('IP addresses')}</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {months.map((s) => (
                    <Table.Row key={s.date}>
                      <Table.Cell sticky="left">
                        <Text bold as="span">
                          {monthYear(s.date, t)}
                        </Text>
                      </Table.Cell>
                      {!overall && <Table.Cell>{s.package}</Table.Cell>}
                      {overall && <Table.Cell className="text-right tabular-nums">{s.uUsers}</Table.Cell>}
                      <Table.Cell>
                        <Usage used={s.uDisk} quota={s.diskQuota} />
                      </Table.Cell>
                      <Table.Cell className="text-right tabular-nums">{mb(s.uDiskWeb)}</Table.Cell>
                      <Table.Cell className="text-right tabular-nums">{mb(s.uDiskMail)}</Table.Cell>
                      <Table.Cell className="text-right tabular-nums">{mb(s.uDiskDb)}</Table.Cell>
                      <Table.Cell className="text-right tabular-nums">{mb(s.uDiskDirs)}</Table.Cell>
                      <Table.Cell>
                        <Usage used={s.uBandwidth} quota={s.bandwidth} />
                      </Table.Cell>
                      {(
                        [
                          'uWebDomains',
                          'uWebSsl',
                          'uWebAliases',
                          'uDnsDomains',
                          'uDnsRecords',
                          'uMailDomains',
                          'uMailAccounts',
                          'uDatabases',
                          'uCronJobs',
                          'uBackups',
                          'ipOwned',
                        ] as const
                      ).map((k) => (
                        <Table.Cell key={k} className="text-right tabular-nums">
                          {s[k]}
                        </Table.Cell>
                      ))}
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table>
            </TableCard>
          </div>
        </>
      )}
    </div>
  )
}
