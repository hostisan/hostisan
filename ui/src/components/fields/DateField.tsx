import { Button, DatePicker, Popover, Text } from '@cloudflare/kumo'
import { CalendarBlankIcon } from '@phosphor-icons/react'
import { useState, type ReactNode } from 'react'
import { useT } from '../../i18n/I18nProvider'
import { useFormGuard } from '../../unsaved/types'

const pad = (n: number) => String(n).padStart(2, '0')
const toIso = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
function fromIso(value: string): Date | undefined {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value)
  return m ? new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3])) : undefined
}

/** Date field stored as Hestia's YYYY-MM-DD string, picked from a Kumo calendar popover. */
export function DateField({
  label,
  description,
  value,
  onChange,
}: {
  label: ReactNode
  description?: ReactNode
  value: string
  onChange: (value: string) => void
}) {
  const t = useT()
  const guard = useFormGuard()
  const [open, setOpen] = useState(false)
  const selected = fromIso(value)
  return (
    <div className="grid gap-1.5">
      <Text bold as="span">
        {label}
      </Text>
      <Popover open={open} onOpenChange={setOpen}>
        <Popover.Trigger
          render={
            <Button className="w-fit min-w-48 justify-start tabular-nums" icon={<CalendarBlankIcon size={16} />}>
              {selected ? value : t('Pick a date')}
            </Button>
          }
        />
        <Popover.Content className="p-2">
          <DatePicker
            mode="single"
            selected={selected}
            defaultMonth={selected}
            onChange={(d: Date | undefined) => {
              if (d) {
                guard?.markDirty()
                onChange(toIso(d))
                setOpen(false)
              }
            }}
          />
        </Popover.Content>
      </Popover>
      {description && (
        <Text variant="secondary" size="sm">
          {description}
        </Text>
      )}
    </div>
  )
}
