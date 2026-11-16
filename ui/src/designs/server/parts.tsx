import { Banner, Checkbox, InputArea, Switch, Text } from '@cloudflare/kumo'
import { WarningCircleIcon, WarningIcon } from '@phosphor-icons/react'
import { useState, type ReactNode } from 'react'
import { apiErrorText } from '../../api/errors'
import type { JobStatus } from '../../api/types.gen'
import { useT } from '../../i18n/I18nProvider'
import { JobProgress } from './JobProgress'

/**
 * Raw config file editor (Hestia's `u-console` textarea), labelled with the file path.
 * Controlled when onChange is given, else it keeps its own text. A read error replaces
 * the textarea, so the error text can never be saved over the file (decision 12).
 */
export function ConfigEditor({
  path,
  value,
  onChange,
  error,
  readOnly = false,
  rows = 18,
}: {
  path: string
  value: string
  onChange?: (text: string) => void
  error?: string
  readOnly?: boolean
  rows?: number
}) {
  const [own, setOwn] = useState(value)
  const label = <span className="font-mono text-[0.9em]">{path}</span>
  if (error) {
    return (
      <div className="grid gap-1.5">
        <Text as="span">{label}</Text>
        <Banner variant="error" icon={<WarningCircleIcon />} title={error} className="whitespace-pre-line" />
      </div>
    )
  }
  return (
    <InputArea
      label={label}
      value={onChange ? value : own}
      onValueChange={onChange ?? setOwn}
      readOnly={readOnly}
      rows={rows}
      spellCheck={false}
      className="resize-y font-mono text-[13px] leading-5 whitespace-pre"
    />
  )
}

/** The editor's "restart after saving" checkbox; checked by default, controlled when checked is given. */
export function RestartCheckbox({
  checked,
  onCheckedChange,
}: {
  checked?: boolean
  onCheckedChange?: (checked: boolean) => void
} = {}) {
  const t = useT()
  const [own, setOwn] = useState(true)
  return (
    <Checkbox
      label={t('Restart the service after saving')}
      checked={checked ?? own}
      onCheckedChange={(v) => (onCheckedChange ?? setOwn)(v === true)}
    />
  )
}

/** A yes/no Hestia select shown as a labelled switch; controlled when checked is given. */
export function ToggleField({
  label,
  description,
  defaultChecked = false,
  checked,
  onCheckedChange,
  disabled,
}: {
  label: ReactNode
  description?: ReactNode
  defaultChecked?: boolean
  checked?: boolean
  onCheckedChange?: (checked: boolean) => void
  disabled?: boolean
}) {
  const [own, setOwn] = useState(defaultChecked)
  return (
    <div className="flex items-start justify-between gap-4">
      <div className="grid gap-0.5">
        <Text as="span">{label}</Text>
        {description && (
          <Text variant="secondary" size="sm" as="span">
            {description}
          </Text>
        )}
      </div>
      <Switch
        checked={checked ?? own}
        onCheckedChange={(v) => (onCheckedChange ?? setOwn)(v)}
        disabled={disabled}
        aria-label={typeof label === 'string' ? label : undefined}
      />
    </div>
  )
}

/** Read-only label/value pair used for system facts (Hestia's "Web Server: apache2"). */
export function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-0.5">
      <Text variant="secondary" as="span">
        {label}
      </Text>
      <Text as="span" bold>
        {children}
      </Text>
    </div>
  )
}

/** A failed request in an alert banner; messages may span lines. */
export function ErrorNote({ error }: { error: unknown }) {
  const t = useT()
  if (!error) return null
  return (
    <div role="alert">
      <Banner
        variant="error"
        icon={<WarningIcon weight="fill" />}
        title={typeof error === 'string' ? error : apiErrorText(error, t)}
        className="whitespace-pre-line"
      />
    </div>
  )
}

/** Follows a started server job in a bordered panel; renders nothing without one. */
export function JobPanel({ jobId, onDone }: { jobId: string; onDone?: (status: JobStatus) => void }): ReactNode {
  if (!jobId) return null
  return (
    <div className="rounded-xl px-5 py-4 ring ring-kumo-line">
      <JobProgress jobId={jobId} onDone={onDone} />
    </div>
  )
}
