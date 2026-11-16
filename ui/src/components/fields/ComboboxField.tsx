import { Combobox } from '@cloudflare/kumo'
import { useState } from 'react'
import { useT } from '../../i18n/I18nProvider'
import { useFormGuard } from '../../unsaved/types'

export type ComboboxOption = { value: string; label: string }

/**
 * Searchable select for long lists (languages, time zones, users). Controlled with
 * value and onChange, or uncontrolled from defaultValue (submitted under name).
 */
export function ComboboxField({
  label,
  options,
  value: controlled,
  onChange,
  defaultValue = '',
  name,
  placeholder,
  emptyText,
}: {
  label: string
  options: ComboboxOption[]
  value?: string
  onChange?: (value: string) => void
  defaultValue?: string
  /** Submits the chosen value under this form field name. */
  name?: string
  placeholder?: string
  emptyText?: string
}) {
  const t = useT()
  const guard = useFormGuard()
  const [own, setOwn] = useState(defaultValue)
  const value = controlled ?? own
  const selected = options.find((o) => o.value === value) ?? null
  return (
    <>
      <Combobox
        label={label}
        items={options}
        value={selected}
        onValueChange={(v) => {
          const next = (v as ComboboxOption | null)?.value
          if (next === undefined) return
          if (next !== value) guard?.markDirty()
          if (controlled === undefined) setOwn(next)
          onChange?.(next)
        }}
        isItemEqualToValue={(a: ComboboxOption, b: ComboboxOption) => a.value === b.value}
      >
        <Combobox.TriggerInput placeholder={placeholder ?? t('Search')} className="w-full" />
        <Combobox.Content>
          <Combobox.Empty>{emptyText ?? t('No matches')}</Combobox.Empty>
          <Combobox.List>
            {(o: ComboboxOption) => (
              <Combobox.Item key={o.value} value={o}>
                {o.label}
              </Combobox.Item>
            )}
          </Combobox.List>
        </Combobox.Content>
      </Combobox>
      {name && <input type="hidden" name={name} value={value} />}
    </>
  )
}
