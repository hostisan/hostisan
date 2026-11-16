import {
  Banner,
  ChartLegend,
  Empty,
  LayerCard,
  Loader,
  LinkButton,
  Tabs,
  Text,
  TimeseriesChart,
} from '@cloudflare/kumo'
import type { TimeseriesData } from '@cloudflare/kumo/components/chart'
import { ChartLineIcon, ListMagnifyingGlassIcon, WarningCircleIcon } from '@phosphor-icons/react'
import * as echarts from 'echarts'
import { useMemo, useState } from 'react'
import { apiErrorText } from '../../api/errors'
import { useGraph, useGraphs } from '../../api/server'
import type { GraphChart, GraphData } from '../../api/types.gen'
import { PageHeader } from '../../components/PageHeader'
import { useT, type Translate } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { axisTimeFormat, formatBytesPerSecond, sentenceCase } from './format'
import { useIsDarkMode } from './useIsDarkMode'

const periods = [
  { value: 'daily', label: msgid('Daily') },
  { value: 'weekly', label: msgid('Weekly') },
  { value: 'monthly', label: msgid('Monthly') },
  { value: 'yearly', label: msgid('Yearly') },
  { value: 'biennially', label: msgid('Biennially') },
  { value: 'triennially', label: msgid('Triennially') },
]

const webNames: Record<string, string> = { nginx: 'NGINX', apache2: 'Apache', httpd: 'Apache' }
const dbNames: Record<string, string> = { mysql: 'MySQL', pgsql: 'PostgreSQL' }

/** A translatable title built from the chart's type and RRD name; the core's own title is the fallback. */
function chartTitle(chart: GraphChart, t: Translate): string {
  const rrd = chart.id.replace(/^net_/, '')
  switch (chart.type) {
    case 'la':
      return t('Load average')
    case 'mem':
      return t('Memory usage')
    case 'net':
      return t('Bandwidth usage %s', rrd)
    case 'web':
      return t('%s usage', webNames[rrd] ?? rrd)
    case 'mail':
      return t('Exim usage')
    case 'ftp':
      return t('FTP usage')
    case 'ssh':
      return t('SSH usage')
    case 'db': {
      const [kind, ...host] = chart.id.split('_')
      return dbNames[kind] ? t('%s usage on %s', dbNames[kind], host.join('_')) : t(sentenceCase(chart.title))
    }
    default:
      return t(sentenceCase(chart.title))
  }
}

function formatterFor(chart: GraphChart): (v: number) => string {
  switch (chart.type) {
    case 'la':
      return (v) => v.toFixed(2)
    case 'mem':
      return (v) => (v >= 1024 ? `${(v / 1024).toFixed(1)} GB` : `${Math.round(v)} MB`)
    case 'net':
      return formatBytesPerSecond
    default:
      return (v) => String(Math.round(v))
  }
}

function cssColor(index: number): string {
  const value = getComputedStyle(document.documentElement)
    .getPropertyValue(`--color-cf-sequential-${index % 14}`)
    .trim()
  return value || '#005de0'
}

function toSeries(data: GraphData, colorOffset: number): TimeseriesData[] {
  return data.legend.map((name, i) => ({
    name,
    color: cssColor(colorOffset + i),
    // A null is a gap in the data (the still-open last step), so it is left out.
    data: data.series.flatMap((row, j) => {
      const v = row[i]
      return v == null ? [] : [[(data.start + j * data.step) * 1000, v] as [number, number]]
    }),
  }))
}

function ChartBody({
  chart,
  title,
  data,
  period,
  colorOffset,
  dark,
}: {
  chart: GraphChart
  title: string
  data: GraphData
  period: string
  colorOffset: number
  dark: boolean
}) {
  const format = formatterFor(chart)
  const series = useMemo(() => toSeries(data, colorOffset), [data, colorOffset])
  const axisTime = useMemo(() => axisTimeFormat(period), [period])
  return (
    <>
      <div className="flex flex-wrap gap-x-4 gap-y-1">
        {series.map((s) => (
          <ChartLegend.SmallItem
            key={s.name}
            name={s.name}
            color={s.color}
            value={s.data.length ? format(s.data[s.data.length - 1][1]) : '-'}
          />
        ))}
      </div>
      <TimeseriesChart
        echarts={echarts}
        isDarkMode={dark}
        data={series}
        height={220}
        gradient={chart.type !== 'la'}
        yAxisName={data.unit}
        yAxisTickFormat={format}
        tooltipValueFormat={format}
        xAxisTickFormat={axisTime}
        yAxisMinInterval={['la', 'net', 'mem'].includes(chart.type) ? undefined : 1}
        ariaDescription={`${title}, ${period}`}
      />
    </>
  )
}

/** One chart: it loads on its own, so a failure shows here and the other cards still draw. */
function ChartCard({
  chart,
  period,
  colorOffset,
  dark,
}: {
  chart: GraphChart
  period: string
  colorOffset: number
  dark: boolean
}) {
  const t = useT()
  const { data, error } = useGraph(chart.id, period)
  const title = chartTitle(chart, t)
  return (
    <LayerCard className="grid min-w-0 gap-3 px-5 py-4">
      <div className="grid gap-2">
        <Text variant="heading" as="h2">
          {title}
        </Text>
      </div>
      {error ? (
        <div role="alert">
          <Banner
            variant="error"
            icon={<WarningCircleIcon />}
            title={apiErrorText(error, t)}
            className="whitespace-pre-line"
          />
        </div>
      ) : !data ? (
        <Loader />
      ) : (
        <ChartBody chart={chart} title={title} data={data} period={period} colorOffset={colorOffset} dark={dark} />
      )}
    </LayerCard>
  )
}

export function GraphsPage() {
  const t = useT()
  const [period, setPeriod] = useState('daily')
  const dark = useIsDarkMode()
  const catalog = useGraphs()
  const charts = catalog.data?.charts ?? []

  return (
    <div className="grid gap-6">
      <PageHeader
        title={t('Graphs')}
        description={t('Resource usage collected by rrdtool every five minutes.')}
        actions={
          <LinkButton href="/server/info" variant="secondary" icon={ListMagnifyingGlassIcon}>
            {t('Advanced details')}
          </LinkButton>
        }
      />
      <div className="-mx-4 overflow-x-auto px-4 sm:mx-0 sm:px-0">
        <Tabs
          variant="segmented"
          value={period}
          onValueChange={setPeriod}
          tabs={periods.map((p) => ({ value: p.value, label: t(p.label) }))}
          className="w-fit"
        />
      </div>
      {catalog.error || catalog.data?.error ? (
        <div role="alert">
          <Banner
            variant="error"
            icon={<WarningCircleIcon />}
            title={catalog.error ? apiErrorText(catalog.error, t) : t(catalog.data?.error ?? '')}
            className="whitespace-pre-line"
          />
        </div>
      ) : !catalog.data ? (
        <Loader />
      ) : charts.length === 0 ? (
        <Empty
          icon={<ChartLineIcon size={40} className="text-kumo-inactive" />}
          title={t('No graphs yet')}
          description={t('Resource usage is collected every five minutes. Check back shortly.')}
          className="py-16"
        />
      ) : (
        <div className="grid gap-4 lg:grid-cols-2">
          {charts.map((c, i) => (
            <ChartCard key={c.id} chart={c} period={period} colorOffset={i * 3} dark={dark} />
          ))}
        </div>
      )}
    </div>
  )
}
