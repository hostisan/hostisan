import { Select } from '@cloudflare/kumo'
import type { ReactNode } from 'react'
import { useFormGuard } from '../../unsaved/types'

type Option = { value: string; label: ReactNode }

/** Kumo Select fed from a plain option list. */
export function SelectField({
  label,
  ariaLabel,
  description,
  value,
  onChange,
  options,
  className,
}: {
  /** Visible label. Pass `ariaLabel` instead for toolbar selects. */
  label?: ReactNode
  ariaLabel?: string
  description?: ReactNode
  value: string
  onChange: (value: string) => void
  options: Option[]
  className?: string
}) {
  const guard = useFormGuard()
  return (
    <Select
      label={label}
      aria-label={ariaLabel}
      description={description}
      value={value}
      onValueChange={(v) => {
        guard?.markDirty()
        onChange((v as string) ?? '')
      }}
      items={Object.fromEntries(options.map((o) => [o.value, o.label]))}
      className={className ?? 'w-full'}
    >
      {options.map((o) => (
        <Select.Option key={o.value} value={o.value}>
          {o.label}
        </Select.Option>
      ))}
    </Select>
  )
}
