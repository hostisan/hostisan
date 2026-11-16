import { Button, Text, useKumoToastManager } from '@cloudflare/kumo'
import { CopyIcon, DownloadSimpleIcon } from '@phosphor-icons/react'
import type { ReactNode } from 'react'
import { useT } from '../i18n/I18nProvider'
import { downloadText } from '../lib/download'

type CopyBlockProps = {
  label: string
  value: string
  description?: ReactNode
  /** Adds a download button that saves the value under this file name. */
  filename?: string
  /**
   * `wrap` (default) breaks long single-line values like DKIM keys or DS records;
   * `scroll` keeps line structure (certificates, keys) in a scrolling box.
   */
  layout?: 'wrap' | 'scroll'
}

/** A labelled monospace value with copy (and optional download) actions. */
export function CopyBlock({ label, value, description, filename, layout = 'wrap' }: CopyBlockProps) {
  const toast = useKumoToastManager()
  const t = useT()
  const copy = () => {
    void navigator.clipboard?.writeText(value).catch(() => {})
    toast.add({ title: t('%s copied', label), type: 'success' })
  }
  return (
    <div className="grid gap-1.5">
      <div className="flex items-center justify-between gap-3">
        <Text bold as="span">
          {label}
        </Text>
        <div className="flex gap-1">
          <Button type="button" size="sm" variant="ghost" icon={<CopyIcon size={16} />} onClick={copy}>
            {t('Copy')}
          </Button>
          {filename && (
            <Button
              type="button"
              size="sm"
              variant="ghost"
              icon={<DownloadSimpleIcon size={16} />}
              onClick={() => downloadText(filename, value)}
            >
              {filename}
            </Button>
          )}
        </div>
      </div>
      <pre
        className={`rounded-lg bg-kumo-recessed px-3 py-2.5 font-mono text-[13px] leading-5 text-kumo-default ring ring-kumo-line ${
          layout === 'wrap' ? 'break-all whitespace-pre-wrap' : 'max-h-64 overflow-auto'
        }`}
      >
        {value}
      </pre>
      {description && (
        <Text variant="secondary" size="sm">
          {description}
        </Text>
      )}
    </div>
  )
}
