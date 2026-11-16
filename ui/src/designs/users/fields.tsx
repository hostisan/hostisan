import {
  Checkbox,
  
  Input,
  Label,
  Meter,
  
  
  
  Text,
  
  
} from '@cloudflare/kumo'
import { useId, useState, type ReactNode } from 'react'
import { useT, type Translate } from '../../i18n/I18nProvider'
import { formatMB, usagePercent } from '../../lib/format'

type LimitInputProps = {
  label: string
  name: string
  defaultValue: string
  /** Shown after the label, e.g. "in MB" or "per domain". */
  unit?: string
  description?: ReactNode
  /** Hestia only offers the "unlimited" toggle on some limits. */
  allowUnlimited?: boolean
}

/** Numeric package limit with Hestia's "Unlimited" toggle. */
export function LimitInput({ label, name, defaultValue, unit, description, allowUnlimited = true }: LimitInputProps) {
  const t = useT()
  const id = useId()
  const [unlimited, setUnlimited] = useState(defaultValue === 'unlimited')
  const [value, setValue] = useState(defaultValue === 'unlimited' ? '' : defaultValue)

  return (
    <div className="grid gap-1.5">
      <div className="flex min-h-5 items-center justify-between gap-3">
        <Label htmlFor={id}>
          {label}
          {unit && <span className="font-normal text-kumo-subtle"> ({unit})</span>}
        </Label>
        {allowUnlimited && (
          <Checkbox label={t('Unlimited')} checked={unlimited} onCheckedChange={(c) => setUnlimited(c === true)} />
        )}
      </div>
      <Input
        id={id}
        name={name}
        inputMode="numeric"
        aria-label={label}
        value={unlimited ? t('Unlimited') : value}
        disabled={unlimited}
        onChange={(e) => setValue(e.target.value)}
      />
      {description && (
        <Text variant="secondary" size="sm">
          {description}
        </Text>
      )}
    </div>
  )
}





/** Disk/bandwidth usage cell (one decimal, as Hestia's users list): a meter against the quota, or plain usage when unlimited. */
export function UsageMeter({ label, used, quota }: { label: string; used: string; quota: string }) {
  const t = useT()
  const pct = usagePercent(used, quota)
  if (pct === null) {
    return (
      <div className="grid w-32 gap-0.5">
        <Text size="sm" as="span" DANGEROUS_className="tabular-nums">
          {formatMB(used, 1)}
        </Text>
        <Text variant="secondary" size="sm" as="span">
          {t('Unlimited')}
        </Text>
      </div>
    )
  }
  return (
    <div className="w-32" title={t('%s: %s used', label, `${pct}%`)}>
      <Meter
        label={`${formatMB(used, 1)} / ${formatMB(quota, 1)}`}
        value={pct}
        showValue={false}
        indicatorClassName={pct >= 90 ? 'bg-kumo-danger' : pct >= 75 ? 'bg-kumo-warning' : undefined}
      />
    </div>
  )
}

/** "unlimited" -> "Unlimited" (translated), numbers pass through. */
export function formatLimit(value: string, t: Translate): string {
  return value === 'unlimited' ? t('Unlimited') : value
}
