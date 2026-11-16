import { Button, buttonVariants, Empty, LayerCard, Tabs, Text } from '@cloudflare/kumo'
import { ArrowClockwiseIcon, DownloadSimpleIcon, FileTextIcon } from '@phosphor-icons/react'
import { useParams, useSearchParams } from 'react-router'
import { useWebDomains, useWebLog, webLogDownloadUrl, type WebLogType } from '../../api/web'
import { PageHeader } from '../../components/PageHeader'
import { useT } from '../../i18n/I18nProvider'
import { DnsError, DnsLoading } from '../dns/shared'
import { DomainNotFound } from './shared'

type LogType = WebLogType

function lineTone(type: LogType, line: string) {
  if (type === 'error') return /:error\]/.test(line) ? 'text-kumo-danger' : /:warn\]/.test(line) ? 'text-kumo-warning' : ''
  const status = /" (\d{3}) /.exec(line)?.[1]
  if (!status) return ''
  if (status.startsWith('5')) return 'text-kumo-danger'
  if (status.startsWith('4')) return 'text-kumo-warning'
  return ''
}

export function WebLogsPage() {
  const t = useT()
  const { domain: name = '' } = useParams()
  const [params, setParams] = useSearchParams()
  const type: LogType = params.get('type') === 'error' ? 'error' : 'access'
  // The logs API answers 200 with no lines for an unknown domain, so existence comes from
  // the (cached) list rather than the heavier edit view
  const list = useWebDomains()
  const log = useWebLog(name, type)

  if (list.data && !list.data.domains.some((d) => d.NAME === name)) return <DomainNotFound name={name} />

  const lines = log.data?.lines ?? []
  const filename = `${name}.${type}.log`
  // Hestia's msgid; the file name inside the translated sentence is set in mono
  const lastLines = t('Last 70 lines of %s.%s.log', name, type)
  const at = lastLines.indexOf(filename)

  return (
    <div className="grid gap-6 pb-4">
      <PageHeader
        title={t('Web logs')}
        description={<span className="font-mono text-[0.9em]">{name}</span>}
        back={{ href: '/web', label: t('Web domains') }}
        actions={
          <>
            <Button icon={ArrowClockwiseIcon} loading={log.isFetching && !log.isPending} onClick={() => void log.refetch()}>
              {t('Refresh')}
            </Button>
            <a href={webLogDownloadUrl(name, type)} download className={buttonVariants({ variant: 'secondary' })}>
              <DownloadSimpleIcon />
              {type === 'error' ? t('Download error log') : t('Download access log')}
            </a>
          </>
        }
      />

      <div className="grid gap-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <Tabs
            variant="segmented"
            tabs={[
              { value: 'access', label: t('Access log') },
              { value: 'error', label: t('Error log') },
            ]}
            value={type}
            onValueChange={(v) => setParams({ type: v }, { replace: true })}
          />
          <Text variant="secondary">
            {at < 0 ? (
              lastLines
            ) : (
              <>
                {lastLines.slice(0, at)}
                <span className="font-mono text-[0.9em]">{filename}</span>
                {lastLines.slice(at + filename.length)}
              </>
            )}
          </Text>
        </div>

        <LayerCard className="overflow-hidden p-0">
          {log.isPending ? (
            <DnsLoading />
          ) : log.isError ? (
            <DnsError error={log.error} />
          ) : lines.length === 0 ? (
            <Empty
              icon={<FileTextIcon size={40} className="text-kumo-inactive" />}
              title={type === 'error' ? t('The error log is empty') : t('The access log is empty')}
              description={t('Entries appear here as soon as the website receives requests.')}
              className="py-12"
            />
          ) : (
            <pre className="max-h-[70vh] overflow-auto px-5 py-4 font-mono text-[13px] leading-6">
              {lines.map((line, i) => (
                <div key={i} className={`whitespace-pre ${lineTone(type, line)}`}>
                  {line}
                </div>
              ))}
            </pre>
          )}
        </LayerCard>
      </div>
    </div>
  )
}
