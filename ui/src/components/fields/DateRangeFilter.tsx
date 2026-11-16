import { Button, DateRangePicker, Popover, Toolbar } from '@cloudflare/kumo'
import { CalendarBlankIcon } from '@phosphor-icons/react'
import { useState, type Dispatch, type SetStateAction } from 'react'
import { useT, type Translate } from '../../i18n/I18nProvider'
import { MONTHS } from '../../lib/date'

export type DateRange = { start: Date | null; end: Date | null }

export const ALL_DATES: DateRange = { start: null, end: null }

function toKey(d: Date) {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

/** True when a Hestia "YYYY-MM-DD" date falls inside the range (inclusive). */
export function inRange(date: string, range: DateRange) {
  if (range.start && date < toKey(range.start)) return false
  if (range.end && date > toKey(range.end)) return false
  return true
}

/** True when a Hestia month key ("YYYY-MM-01") overlaps the range. */
export function monthInRange(month: string, range: DateRange) {
  const m = month.slice(0, 7)
  if (range.start && m < toKey(range.start).slice(0, 7)) return false
  if (range.end && m > toKey(range.end).slice(0, 7)) return false
  return true
}

/** The range as the filter button shows it, e.g. "Jan 5 – Mar 1" or "From Jan 2026". */
export function rangeLabel(range: DateRange, granularity: 'day' | 'month' = 'day', t: Translate = (s) => s) {
  const fmt = (d: Date) => `${t(MONTHS[d.getMonth()])} ${granularity === 'month' ? d.getFullYear() : d.getDate()}`
  if (!range.start && !range.end) return t('All dates')
  if (range.start && !range.end) return t('From %s', fmt(range.start))
  if (!range.start && range.end) return t('Until %s', fmt(range.end))
  const a = fmt(range.start!)
  const b = fmt(range.end!)
  return a === b ? a : `${a} – ${b}`
}

/**
 * Toolbar button that opens Kumo's DateRangePicker. The picker is uncontrolled, so this
 * keeps the chosen range and shows it on the button.
 */
export function DateRangeFilter({
  value,
  onChange,
  granularity = 'day',
  toolbar = true,
}: {
  value: DateRange
  onChange: Dispatch<SetStateAction<DateRange>>
  granularity?: 'day' | 'month'
  /** Render as a joined toolbar button (default) or a standalone secondary button. */
  toolbar?: boolean
}) {
  const t = useT()
  const [open, setOpen] = useState(false)
  const label = rangeLabel(value, granularity, t)
  const trigger = toolbar ? (
    <Toolbar.Button icon={CalendarBlankIcon} aria-label={t('Date range: %s', label)} />
  ) : (
    <Button variant="secondary" icon={CalendarBlankIcon} aria-label={t('Date range: %s', label)} />
  )

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <Popover.Trigger render={trigger}>
        <span className={toolbar ? 'max-md:sr-only' : undefined}>{label}</span>
      </Popover.Trigger>
      <Popover.Content align="end" className="grid gap-3 p-3">
        <DateRangePicker
          size="sm"
          variant="subtle"
          timezone="UTC"
          onStartDateChange={(start) => onChange((prev) => ({ ...prev, start }))}
          onEndDateChange={(end) => onChange((prev) => ({ ...prev, end }))}
        />
        <div className="flex justify-end">
          <Button size="sm" variant="primary" onClick={() => setOpen(false)}>
            {t('Done')}
          </Button>
        </div>
      </Popover.Content>
    </Popover>
  )
}
