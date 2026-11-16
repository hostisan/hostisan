import { Autocomplete, Badge } from '@cloudflare/kumo'
import type { ReactNode } from 'react'
import { useT } from '../../i18n/I18nProvider'

type BadgeVariant = 'blue' | 'teal' | 'purple' | 'orange' | 'green' | 'neutral' | 'outline'

const typeVariant: Record<string, BadgeVariant> = {
  A: 'blue',
  AAAA: 'blue',
  CNAME: 'teal',
  MX: 'purple',
  TXT: 'neutral',
  SPF: 'neutral',
  SRV: 'orange',
  CAA: 'green',
  NS: 'outline',
  PTR: 'teal',
  DNSKEY: 'green',
  DS: 'green',
  KEY: 'green',
  IPSECKEY: 'green',
  TLSA: 'green',
}

export function RecordTypeBadge({ type }: { type: string }) {
  return (
    <Badge variant={typeVariant[type] ?? 'neutral'} className="font-mono">
      {type}
    </Badge>
  )
}

/** Record types in the order of the add-record select. */
export const dnsRecordTypes = [
  'A',
  'AAAA',
  'CAA',
  'CNAME',
  'DNSKEY',
  'DS',
  'IPSECKEY',
  'KEY',
  'MX',
  'NS',
  'PTR',
  'SPF',
  'SRV',
  'TLSA',
  'TXT',
]

// The DNS pages' names for the shared request states (components/RequestState).
export { LoadingState as DnsLoading, RequestError as DnsError } from '../../components/RequestState'

/** Hestia's "list editor": pick a server IP or type any address. */
export function IpField({
  label,
  description,
  value,
  onChange,
  ips,
  required,
}: {
  label?: string
  description?: ReactNode
  value: string
  onChange: (value: string) => void
  ips: string[]
  required?: boolean
}) {
  const t = useT()
  return (
    <Autocomplete
      label={label ?? t('IP address')}
      required={required}
      description={description}
      items={ips}
      value={value}
      onValueChange={(v) => onChange(String(v ?? ''))}
    >
      <Autocomplete.InputGroup placeholder="203.0.113.24" />
      <Autocomplete.Content>
        <Autocomplete.List>
          {(item: string) => (
            <Autocomplete.Item key={item} value={item}>
              {item}
            </Autocomplete.Item>
          )}
        </Autocomplete.List>
      </Autocomplete.Content>
    </Autocomplete>
  )
}

export function suspendLabels(suspended: boolean) {
  return suspended ? { action: 'Unsuspend', verb: 'unsuspend' } : { action: 'Suspend', verb: 'suspend' }
}
