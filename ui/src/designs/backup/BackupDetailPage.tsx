import { Badge, Button, Empty, LayerCard, LinkButton, Text, useKumoToastManager } from '@cloudflare/kumo'
import { ArchiveIcon, ArrowCounterClockwiseIcon, DownloadSimpleIcon } from '@phosphor-icons/react'
import { useState, type ReactNode } from 'react'
import { useParams } from 'react-router'
import { useBackup, useBackupMutation } from '../../api/backup'
import { apiErrorText } from '../../api/errors'
import { ApiError } from '../../api/client'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { PageHeader } from '../../components/PageHeader'
import { useT } from '../../i18n/I18nProvider'
import { useSessionUser } from '../../session/SessionProvider'
import { DnsError, DnsLoading } from '../dns/shared'
import { formatBackupSize, formatRuntime, useBackupDownload } from './BackupTabs'
import { RestoreTable, restoreItems } from './RestoreTable'

export function Summary({ items }: { items: { label: string; value: ReactNode }[] }) {
  return (
    <LayerCard className="px-5 py-4">
      <dl className="grid grid-cols-2 gap-x-6 gap-y-3 sm:flex sm:flex-wrap sm:gap-x-10">
        {items.map((i) => (
          <div key={i.label} className="grid gap-0.5">
            <dt className="text-kumo-subtle">{i.label}</dt>
            <dd className="tabular-nums">{i.value}</dd>
          </div>
        ))}
      </dl>
    </LayerCard>
  )
}

export function BackupDetailPage() {
  const { backup = '' } = useParams()
  const toast = useKumoToastManager()
  const t = useT()
  const back = { href: '/backup', label: t('Backups') }
  const readOnly = useSessionUser()?.readOnly ?? false
  const [confirmOpen, setConfirmOpen] = useState(false)
  const q = useBackup(backup)
  const m = useBackupMutation()
  const download = useBackupDownload()
  const b = q.data?.backup

  if (q.isPending) return <DnsLoading />
  if (q.isError && !(q.error instanceof ApiError && q.error.status === 404)) {
    return (
      <div className="grid gap-6">
        <PageHeader title={t('Backup')} back={back} />
        <DnsError error={q.error} />
      </div>
    )
  }
  if (!b) {
    return (
      <div className="grid gap-6">
        <PageHeader title={t('Backup not found')} back={back} />
        <Empty
          icon={<ArchiveIcon size={40} className="text-kumo-inactive" />}
          title={t('Backup not found')}
          description={t('%s may have been rotated out or deleted.', backup)}
          contents={<LinkButton href="/backup">{t('Back to backups')}</LinkButton>}
          className="py-16"
        />
      </div>
    )
  }

  const restoreAll = () =>
    m.restore(backup, { all: true, web: [], dns: [], mail: [], db: [], udir: [], cron: false }).then(
      (r) => toast.add({ title: t('Restore scheduled'), description: t(r.message), type: 'success' }),
      (err) =>
        toast.add({
          title: t('Restore failed'),
          description: <span className="whitespace-pre-line">{apiErrorText(err, t)}</span>,
          type: 'error',
        }),
    )

  return (
    <div className="grid gap-6">
      <PageHeader
        title={t('Restore from backup')}
        description={<span className="font-mono text-[0.9em]">{backup}</span>}
        back={back}
        actions={
          <>
            <Button icon={DownloadSimpleIcon} onClick={() => download(backup)}>
              {t('Download')}
            </Button>
            {!readOnly && (
              <Button variant="primary" icon={ArrowCounterClockwiseIcon} onClick={() => setConfirmOpen(true)}>
                {t('Restore all')}
              </Button>
            )}
          </>
        }
      />

      <Summary
        items={[
          { label: t('Date'), value: `${b.DATE} ${b.TIME}` },
          { label: t('Size'), value: formatBackupSize(b.SIZE) },
          { label: t('Type'), value: <Badge variant={b.TYPE === 'local' ? 'neutral' : 'blue'}>{b.TYPE}</Badge> },
          { label: t('Runtime'), value: formatRuntime(b.RUNTIME, t) },
        ]}
      />

      <div className="grid gap-1">
        <Text variant="heading" as="h2">
          {t('Backup contents')}
        </Text>
        <Text variant="secondary">
          {t('Restoring an item overwrites its current files, records or data with the copy from this backup.')}
        </Text>
      </div>
      <RestoreTable items={restoreItems(b)} restore={(body) => m.restore(backup, body)} readOnly={readOnly} />

      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={t('Restore all')}
        description={t(
          'Are you sure you want to restore everything in %s? Existing web domains, mail, databases, cron jobs and user directories will be overwritten.',
          backup,
        )}
        confirmLabel={t('Restore all')}
        onConfirm={restoreAll}
      />
    </div>
  )
}
