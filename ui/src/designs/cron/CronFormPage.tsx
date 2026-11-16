import { Banner, Button, Input, Select, Tabs, Text, useKumoToastManager } from '@cloudflare/kumo'
import { CalendarCheckIcon, ClockIcon, WarningIcon } from '@phosphor-icons/react'
import { useState, type FormEvent } from 'react'
import { useNavigate, useParams } from 'react-router'
import { useCronJob, useCronMutation, useCronOptions } from '../../api/cron'
import { ApiError } from '../../api/client'
import { apiErrorText } from '../../api/errors'
import type { CronJob } from '../../api/hestia.gen'
import { FormPage } from '../../components/FormPage'
import { FormSection } from '../../components/FormSection'
import { StatusBadge } from '../../components/StatusBadge'
import { useT } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { MONTHS } from '../../lib/date'
import { useSessionUser } from '../../session/SessionProvider'
import { DnsError, DnsLoading } from '../dns/shared'
import { WEEKDAYS, describeCron, formatServerTime } from './describeCron'

type Schedule = Pick<CronJob, 'MIN' | 'HOUR' | 'DAY' | 'MONTH' | 'WDAY'>
/** A select option. A label with letters is a msgid, filled with `arg` when set. */
type Option = { label: string; value: string; arg?: number }
type Tab = 'minutes' | 'hourly' | 'daily' | 'weekly' | 'monthly'

const back = { href: '/cron', label: msgid('Cron jobs') }

const pad = (n: number) => String(n).padStart(2, '0')
const range = (from: number, to: number): Option[] =>
  Array.from({ length: to - from + 1 }, (_, i) => ({ label: pad(from + i), value: String(from + i) }))

// Option lists copied from Hestia's add_cron.php generator.
const minuteSteps: Option[] = [
  { label: msgid('Every minute'), value: '*' },
  ...[2, 5, 10, 15, 30].map((n) => ({ label: msgid('Every %s minutes'), arg: n, value: `*/${n}` })),
]
const hourSteps: Option[] = [
  { label: msgid('Every hour'), value: '*' },
  ...[2, 6, 12].map((n) => ({ label: msgid('Every %s hours'), arg: n, value: `*/${n}` })),
]
const quarterMinutes: Option[] = ['0', '15', '30', '45'].map((v) => ({ label: pad(Number(v)), value: v }))
const minutes: Option[] = [0, 1, 2, 5, 10, 15, 20, 25, 30, 35, 40, 45, 50, 55].map((n) => ({
  label: pad(n),
  value: String(n),
}))
const hours = range(0, 23)
const days = range(1, 31)
const daySteps: Option[] = [
  { label: msgid('Every day'), value: '*' },
  { label: msgid('Every odd day'), value: '1-31/2' },
  { label: msgid('Every even day'), value: '*/2' },
  ...[3, 5, 10, 15].map((n) => ({ label: msgid('Every %s days'), arg: n, value: `*/${n}` })),
]
const weekdayOptions: Option[] = [
  { label: msgid('Every day'), value: '*' },
  { label: msgid('Weekdays (5 days)'), value: '1,2,3,4,5' },
  { label: msgid('Weekend (2 days)'), value: '0,6' },
  // Monday first, Sunday last, as Hestia lists them.
  ...[1, 2, 3, 4, 5, 6, 0].map((d) => ({ label: WEEKDAYS[d], value: String(d) })),
]
const monthOptions: Option[] = [
  { label: msgid('Every month'), value: '*' },
  { label: msgid('Every odd month'), value: '1-11/2' },
  { label: msgid('Every even month'), value: '*/2' },
  { label: msgid('Every %s months'), arg: 3, value: '*/3' },
  { label: msgid('Every %s months'), arg: 6, value: '*/6' },
  ...MONTHS.map((m, i) => ({ label: m, value: String(i + 1) })),
]

function Pick({
  label,
  value,
  onChange,
  items,
  className = 'w-full',
}: {
  label: string
  value: string
  onChange: (v: string) => void
  items: Option[]
  className?: string
}) {
  const t = useT()
  const shown = items.map((o) => ({
    value: o.value,
    label: !/[A-Za-z]/.test(o.label) ? o.label : o.arg === undefined ? t(o.label) : t(o.label, o.arg),
  }))
  return (
    <Select label={label} className={className} value={value} onValueChange={(v) => onChange((v as string) ?? '*')} items={shown} />
  )
}

/** Hestia's schedule generator: pick a preset per tab, then "Generate" writes the five raw fields. */
function ScheduleGenerator({ onGenerate }: { onGenerate: (s: Schedule) => void }) {
  const t = useT()
  const [tab, setTab] = useState<Tab>('minutes')
  const [g, setG] = useState({
    minutesMin: '*',
    hourlyHour: '*',
    hourlyMin: '0',
    dailyDay: '*',
    dailyHour: '12',
    dailyMin: '0',
    weeklyWday: '*',
    weeklyHour: '12',
    weeklyMin: '0',
    monthlyMonth: '*',
    monthlyDay: '1',
    monthlyHour: '12',
    monthlyMin: '0',
  })
  const set = (key: keyof typeof g) => (v: string) => setG((prev) => ({ ...prev, [key]: v }))

  const generated: Record<Tab, Schedule> = {
    minutes: { MIN: g.minutesMin, HOUR: '*', DAY: '*', MONTH: '*', WDAY: '*' },
    hourly: { MIN: g.hourlyMin, HOUR: g.hourlyHour, DAY: '*', MONTH: '*', WDAY: '*' },
    daily: { MIN: g.dailyMin, HOUR: g.dailyHour, DAY: g.dailyDay, MONTH: '*', WDAY: '*' },
    weekly: { MIN: g.weeklyMin, HOUR: g.weeklyHour, DAY: '*', MONTH: '*', WDAY: g.weeklyWday },
    monthly: { MIN: g.monthlyMin, HOUR: g.monthlyHour, DAY: g.monthlyDay, MONTH: g.monthlyMonth, WDAY: '*' },
  }
  const preview = generated[tab]

  return (
    <div className="grid h-fit gap-4 rounded-lg px-4 py-3 ring ring-kumo-line">
      <div className="overflow-x-auto">
        <Tabs
          variant="segmented"
          value={tab}
          onValueChange={(v) => setTab(v as Tab)}
          tabs={[
            { value: 'minutes', label: t('Minutes') },
            { value: 'hourly', label: t('Hourly') },
            { value: 'daily', label: t('Daily') },
            { value: 'weekly', label: t('Weekly') },
            { value: 'monthly', label: t('Monthly') },
          ]}
        />
      </div>

      {tab === 'minutes' && <Pick label={t('Run command')} value={g.minutesMin} onChange={set('minutesMin')} items={minuteSteps} />}
      {tab === 'hourly' && (
        <div className="grid gap-4 sm:grid-cols-[1fr_7rem]">
          <Pick label={t('Run command')} value={g.hourlyHour} onChange={set('hourlyHour')} items={hourSteps} />
          <Pick label={t('Minute')} value={g.hourlyMin} onChange={set('hourlyMin')} items={quarterMinutes} />
        </div>
      )}
      {tab === 'daily' && (
        <div className="grid gap-4 sm:grid-cols-[1fr_6rem_6rem]">
          <Pick label={t('Run command')} value={g.dailyDay} onChange={set('dailyDay')} items={daySteps} />
          <Pick label={t('Hour')} value={g.dailyHour} onChange={set('dailyHour')} items={hours} />
          <Pick label={t('Minute')} value={g.dailyMin} onChange={set('dailyMin')} items={minutes} />
        </div>
      )}
      {tab === 'weekly' && (
        <div className="grid gap-4 sm:grid-cols-[1fr_6rem_6rem]">
          <Pick label={t('Run command')} value={g.weeklyWday} onChange={set('weeklyWday')} items={weekdayOptions} />
          <Pick label={t('Hour')} value={g.weeklyHour} onChange={set('weeklyHour')} items={hours} />
          <Pick label={t('Minute')} value={g.weeklyMin} onChange={set('weeklyMin')} items={minutes} />
        </div>
      )}
      {tab === 'monthly' && (
        <div className="grid gap-4 sm:grid-cols-[1fr_5.5rem_5.5rem_5.5rem]">
          <Pick label={t('Run command')} value={g.monthlyMonth} onChange={set('monthlyMonth')} items={monthOptions} />
          <Pick label={t('Day')} value={g.monthlyDay} onChange={set('monthlyDay')} items={days} />
          <Pick label={t('Hour')} value={g.monthlyHour} onChange={set('monthlyHour')} items={hours} />
          <Pick label={t('Minute')} value={g.monthlyMin} onChange={set('monthlyMin')} items={minutes} />
        </div>
      )}

      <div className="flex flex-wrap items-center justify-between gap-3">
        <Text variant="secondary">
          {describeCron(preview.MIN, preview.HOUR, preview.DAY, preview.MONTH, preview.WDAY, t)}
        </Text>
        <Button icon={CalendarCheckIcon} onClick={() => onGenerate(preview)}>
          {t('Generate')}
        </Button>
      </div>
    </div>
  )
}

const rawFields: { key: keyof Schedule; label: string; wire: string }[] = [
  { key: 'MIN', label: msgid('Minute'), wire: 'min' },
  { key: 'HOUR', label: msgid('Hour'), wire: 'hour' },
  { key: 'DAY', label: msgid('Day'), wire: 'day' },
  { key: 'MONTH', label: msgid('Month'), wire: 'month' },
  { key: 'WDAY', label: msgid('Day of week'), wire: 'wday' },
]

/**
 * What a failed save shows. A bad schedule field ("invalid %s format :: %s", args
 * [name, value]) belongs on that field; everything else is one banner.
 */
function cronError(err: unknown, t: (s: string) => string): { field?: keyof Schedule; text: string } {
  const field =
    err instanceof ApiError && err.message === 'invalid %s format :: %s'
      ? rawFields.find((f) => f.wire === err.args[0])?.key
      : undefined
  return { field, text: apiErrorText(err, t) }
}

function ServerTime({ iso, zone }: { iso: string; zone: string }) {
  const t = useT()
  return (
    <span>
      {t('System time')}:{' '}
      <time dateTime={iso} className="font-medium text-kumo-default">
        {formatServerTime(iso, zone, t)}
      </time>{' '}
      <span className="text-kumo-subtle">{zone}</span>
    </span>
  )
}

function CronForm({ job, mode }: { job?: CronJob; mode: 'add' | 'edit' }) {
  const t = useT()
  const user = useSessionUser()
  const m = useCronMutation()
  const toast = useKumoToastManager()
  const navigate = useNavigate()
  const options = useCronOptions().data
  const [cmd, setCmd] = useState(job?.CMD ?? '')
  const [schedule, setSchedule] = useState<Schedule>(
    job
      ? { MIN: job.MIN, HOUR: job.HOUR, DAY: job.DAY, MONTH: job.MONTH, WDAY: job.WDAY }
      : { MIN: '*', HOUR: '*', DAY: '*', MONTH: '*', WDAY: '*' },
  )
  const [error, setError] = useState<{ field?: keyof Schedule; text: string } | null>(null)
  const [busy, setBusy] = useState(false)
  const suspended = job?.SUSPENDED === 'yes'

  const onSubmit = async (e: FormEvent) => {
    void e
    setError(null)
    setBusy(true)
    const body = {
      min: schedule.MIN,
      hour: schedule.HOUR,
      day: schedule.DAY,
      month: schedule.MONTH,
      wday: schedule.WDAY,
      cmd,
    }
    try {
      if (job) await m.edit(job.JOB, body)
      else await m.add(body)
      toast.add({
        title: mode === 'add' ? t('Cron job has been created successfully.') : t('Changes have been saved.'),
        type: 'success',
      })
      navigate('/cron')
    } catch (err) {
      setError(cronError(err, t))
      setBusy(false)
    }
  }

  return (
    <FormPage
      title={mode === 'add' ? t('Add cron job') : t('Edit cron job')}
      description={
        <span className="flex flex-wrap items-center gap-2">
          {options && <ServerTime iso={options.now} zone={options.timezone} />}
          {job && <StatusBadge suspended={suspended} />}
        </span>
      }
      back={{ ...back, label: t(back.label) }}
      submitLabel={mode === 'add' ? t('Add cron job') : t('Save')}
      onSubmit={onSubmit}
      submitDisabled={busy}
      readOnly={suspended || user?.readOnly}
    >
      {suspended && (
        <Banner
          variant="alert"
          icon={<WarningIcon weight="fill" />}
          title={t('This cron job is suspended')}
          description={t('Unsuspend it from the cron jobs list before editing.')}
        />
      )}
      {error && !error.field && (
        <Banner variant="error" icon={<WarningIcon weight="fill" />} title={error.text} className="whitespace-pre-line" />
      )}

      <FormSection title={t('Command')} description={t("Runs as this account's system user, from its home directory.")}>
        <Input
          label={t('Command')}
          value={cmd}
          onChange={(e) => setCmd(e.target.value)}
          className="font-mono leading-normal"
          placeholder="php /home/admin/web/northwind-shop.io/private/artisan schedule:run"
          disabled={suspended}
        />
      </FormSection>

      <FormSection
        title={t('Schedule')}
        description={t('Type the cron fields directly, or use the generator to fill them in from a preset.')}
      >
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-5">
          {rawFields.map((f) => (
            <div key={f.key} className="min-w-0">
              <Input
                label={t(f.label)}
                value={schedule[f.key]}
                onChange={(e) => setSchedule((s) => ({ ...s, [f.key]: e.target.value }))}
                className="w-full font-mono"
                disabled={suspended}
                error={error?.field === f.key ? error.text : undefined}
              />
            </div>
          ))}
        </div>
        <div className="flex items-start gap-2">
          <span className="flex h-lh items-center text-kumo-subtle">
            <ClockIcon />
          </span>
          <Text>{describeCron(schedule.MIN, schedule.HOUR, schedule.DAY, schedule.MONTH, schedule.WDAY, t)}</Text>
        </div>
        {!suspended && <ScheduleGenerator onGenerate={setSchedule} />}
      </FormSection>
    </FormPage>
  )
}

export function AddCronJobPage() {
  return <CronForm mode="add" />
}

export function EditCronJobPage() {
  const t = useT()
  const { id = '' } = useParams()
  const job = useCronJob(id)
  if (job.error || !job.data) {
    return (
      <FormPage title={t('Edit cron job')} back={{ ...back, label: t(back.label) }} readOnly>
        {job.error ? <DnsError error={job.error} /> : <DnsLoading />}
      </FormPage>
    )
  }
  return <CronForm key={id} job={job.data} mode="edit" />
}
