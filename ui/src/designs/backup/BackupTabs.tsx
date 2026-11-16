import { Tabs, useKumoToastManager } from '@cloudflare/kumo'
import { useEffect } from 'react'
import { useNavigate } from 'react-router'
import { apiErrorText } from '../../api/errors'
import { fileUrl, useBackupMutation } from '../../api/backup'
import { useT, type Translate } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { useSessionUser } from '../../session/SessionProvider'
import { BackupAside } from './BackupAside'

/**
 * Switches between Hestia's three backup lists. The incremental tab only shows when
 * the package has snapshots. Owns the summary rail so every tab shows the same one and
 * switching never shifts the layout.
 */
export function BackupTabs({ value }: { value: string }) {
  const navigate = useNavigate()
  const incremental = useSessionUser()?.features.incremental ?? false
  const t = useT()
  const tabs = [
    { value: '/backup', label: t('Backups') },
    ...(incremental ? [{ value: '/backup/incremental', label: t('Incremental backups') }] : []),
    { value: '/backup/exclusions', label: t('Backup exclusions') },
  ]
  return (
    <div className="overflow-x-auto">
      <BackupAside />
      <Tabs variant="underline" value={value} onValueChange={(v) => navigate(v)} tabs={tabs} />
    </div>
  )
}

/**
 * Jobs finish in the background and tell the user by email and in the notification
 * tray; there is no job status. Coming back to the tab is the cue to look again.
 */
export function useRefetchOnFocus(refetch: () => unknown) {
  useEffect(() => {
    const on = () => {
      if (document.visibilityState === 'visible') void refetch()
    }
    document.addEventListener('visibilitychange', on)
    return () => document.removeEventListener('visibilitychange', on)
  }, [refetch])
}

const SIZE_UNITS = ['mb', 'gb', 'tb', 'pb']

/** Megabytes the way Hestia's humanize_usage_size shows them: 312 -> "312.00 mb", 1243 -> "1.21 gb". */
export function formatBackupSize(mb: string) {
  let n = Number(mb)
  if (!Number.isFinite(n) || n < 1) n = 0
  let i = 0
  while (n > 999 && i < SIZE_UNITS.length - 1) {
    n /= 1024
    i++
  }
  return `${n.toFixed(2)} ${SIZE_UNITS[i]}`
}

// Hestia's "%d minute" msgids are the singular of plural entries; the catalogue has no
// plural forms (O8), so the plural is its own msgid.
const units = {
  minute: [msgid('%d minute'), msgid('%d minutes')],
  hour: [msgid('%d hour'), msgid('%d hours')],
  day: [msgid('%d day'), msgid('%d days')],
} as const

const plural = (t: Translate, n: number, unit: keyof typeof units) => t(units[unit][n === 1 ? 0 : 1], n)

/** Minutes the way Hestia's humanize_time shows them: "2 minutes", "1 hour", "2 days". */
export function formatRuntime(minutes: string, t: Translate) {
  const n = Number(minutes) || 0
  if (n < 60) return plural(t, n, 'minute')
  const hours = Math.round(n / 60)
  return hours >= 24 ? plural(t, Math.round(hours / 24), 'day') : plural(t, hours, 'hour')
}

/** A snapshot time in its own UTC offset, as restic recorded it: "2026-10-19 03:00:04". */
export function formatDateTime(iso: string) {
  return /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}/.test(iso) ? `${iso.slice(0, 10)} ${iso.slice(11, 19)}` : iso
}

/**
 * Starts a backup download. A local archive is fetched at once; a remote one is queued
 * and the user gets an email when it is ready (then clicks Download again).
 */
export function useBackupDownload() {
  const m = useBackupMutation()
  const toast = useKumoToastManager()
  const t = useT()
  return (name: string) =>
    m.prepareDownload(name).then(
      (r) => {
        if (r.status === 'ready') window.open(fileUrl(name), '_self')
        else toast.add({ title: t('Download scheduled'), description: t(r.message), type: 'success' })
      },
      (err) => toast.add({ title: t('Download failed'), description: apiErrorText(err, t), type: 'error' }),
    )
}
