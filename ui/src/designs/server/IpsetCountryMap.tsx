import { ChartLegend, ChoroplethMap, LayerCard, Text } from '@cloudflare/kumo'
import type { MapGeoJson } from '@cloudflare/kumo/components/chart'
import * as echarts from 'echarts'
import { useEffect, useMemo, useState } from 'react'
import type { FirewallIpset } from '../../api/hestia.gen'
import type { FirewallRuleRow } from '../../api/types.gen'
import { useT } from '../../i18n/I18nProvider'
import { useIsDarkMode } from './useIsDarkMode'

// The data sources the add-list form offers (Hestia's ipListDataSource.js): the shipped
// blacklist script and one ipverse country list per country. Anything else the server refuses.
const ipsetCountries: Record<string, string> = {
  br: 'Brazil',
  ca: 'Canada',
  cn: 'China',
  fr: 'France',
  de: 'Germany',
  in: 'India',
  id: 'Indonesia',
  nl: 'Netherlands',
  ro: 'Romania',
  ru: 'Russia',
  es: 'Spain',
  ch: 'Switzerland',
  tr: 'Turkey',
  ua: 'Ukraine',
  gb: 'United Kingdom',
  us: 'United States',
}

export const ipsetSources: { name: string; source: string }[] = [
  {
    name: '[IPv4] Block Malicious IPs',
    source: 'script:/usr/local/hestia/install/common/firewall/ipset/blacklist.sh',
  },
  ...Object.entries(ipsetCountries)
    .map(([iso, name]) => ({
      name: `[IPv4] Country - ${name}`,
      source: `https://raw.githubusercontent.com/ipverse/rir-ip/master/country/${iso}/ipv4-aggregated.txt`,
    }))
    .sort((a, b) => a.name.localeCompare(b.name)),
]

// ipverse country lists, as offered by add_firewall_ipset.php.
const countrySource = /ipverse\/rir-ip\/master\/country\/([a-z]{2})\//

type CountryRow = { iso: string; country: string; lists: string[]; blocked: boolean; value: number }

function cssColor(name: string, fallback: string) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || fallback
}

/** Countries covered by country-based IP lists. Renders nothing when there are none. */
export function IpsetCountryMap({ lists, rules }: { lists: FirewallIpset[]; rules: FirewallRuleRow[] }) {
  const dark = useIsDarkMode()
  const t = useT()
  const [geoJson, setGeoJson] = useState<MapGeoJson | null>(null)

  const rows = useMemo<CountryRow[]>(() => {
    const byIso = new Map<string, CountryRow>()
    for (const list of lists) {
      const name = list.LISTNAME
      const iso = countrySource.exec(list.SOURCE)?.[1]
      if (!iso) continue
      const preset = ipsetSources.find((s) => s.source === list.SOURCE)
      const country = preset?.name.replace(/^\[IPv\d\] Country - /, '') ?? iso.toUpperCase()
      const blocked = rules.some((r) => r.action === 'DROP' && !r.suspended && r.ip === `ipset:${name}`)
      const row = byIso.get(iso) ?? { iso, country, lists: [], blocked: false, value: 1 }
      row.lists.push(name)
      row.blocked ||= blocked
      row.value = row.blocked ? 2 : 1
      byIso.set(iso, row)
    }
    return [...byIso.values()]
  }, [lists, rules])

  useEffect(() => {
    if (rows.length === 0 || geoJson) return
    // ~150 KB of country outlines, only fetched when there is something to show.
    import('./world-110m.json').then((m) => setGeoJson(m.default as unknown as MapGeoJson))
  }, [rows.length, geoJson])

  if (rows.length === 0) return null

  const blockedColor = cssColor('--color-cf-sequential-9', '#cb0849')
  const listedColor = cssColor('--color-cf-sequential-4', '#fcbad0')
  const blocked = rows.filter((r) => r.blocked)
  const unused = rows.filter((r) => !r.blocked)

  return (
    <LayerCard className="grid gap-4 px-5 py-4 md:grid-cols-[minmax(0,16rem)_1fr] md:items-center">
      <div className="grid gap-3">
        <div className="grid gap-1">
          <Text variant="heading" as="h2">
            {t('Country lists')}
          </Text>
          <Text variant="secondary">{t('Countries covered by country-based IP lists.')}</Text>
        </div>
        <div className="grid gap-1.5">
          <ChartLegend.SmallItem
            name={t('Blocked by a DROP rule')}
            color={blockedColor}
            value={blocked.map((r) => r.country).join(', ') || t('None')}
          />
          <ChartLegend.SmallItem
            name={t('Listed, not used in a rule')}
            color={listedColor}
            value={unused.map((r) => r.country).join(', ') || t('None')}
          />
        </div>
      </div>
      <div className="h-48 min-w-0">
        {geoJson && (
          <ChoroplethMap<CountryRow>
            echarts={echarts}
            geoJson={geoJson}
            mapName="hostisan-world-110m"
            data={rows}
            name="iso"
            nameProperty="iso_a2"
            value="value"
            colorRange={[listedColor, blockedColor]}
            height={192}
            zoom={1.15}
            isDarkMode={dark}
            tooltipFormatter={(r) =>
              `<strong>${r.country}</strong><br/>${r.blocked ? t('Blocked: %s', r.lists.join(', ')) : t('Listed only: %s', r.lists.join(', '))}`
            }
          />
        )}
      </div>
    </LayerCard>
  )
}
