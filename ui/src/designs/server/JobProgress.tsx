import { Banner, Loader, Text } from '@cloudflare/kumo'
import { CheckCircleIcon, WarningCircleIcon, XCircleIcon } from '@phosphor-icons/react'
import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef } from 'react'
import { ApiError } from '../../api/client'
import { apiErrorText } from '../../api/errors'
import { serverKeys, useJob } from '../../api/server'
import type { JobStatus } from '../../api/types.gen'
import { useT } from '../../i18n/I18nProvider'
import { stepText } from './format'

/**
 * Follows a server job (POST … → 202 JobRef): its title, the steps so far with the
 * current one spinning, and the failure in an error banner. When the job ends it
 * refetches every server query and calls onDone once with the final status.
 */
export function JobProgress({ jobId, onDone }: { jobId: string; onDone?: (status: JobStatus) => void }) {
  const t = useT()
  const client = useQueryClient()
  const { data, error } = useJob(jobId)
  const reported = useRef('')

  const finished = data && data.state !== 'running' ? data : undefined
  useEffect(() => {
    if (!finished || reported.current === finished.id) return
    reported.current = finished.id
    void client.invalidateQueries({ queryKey: serverKeys.all })
    onDone?.(finished)
  }, [finished, client, onDone])

  if (!jobId) return null
  if (error) {
    return (
      <div role="alert">
        <Banner variant="error" icon={<WarningCircleIcon />} title={apiErrorText(error, t)} />
      </div>
    )
  }
  if (!data) return <Loader />

  const running = data.state === 'running'
  const failed = data.state === 'failed'
  const last = data.steps.length - 1
  return (
    <div className="grid gap-3">
      <div className="grid gap-1.5">
        <Text as="h3" variant="heading">
          {stepText(data.title, t)}
        </Text>
        <ol className="grid gap-1">
          {data.steps.map((step, i) => (
            <li key={`${i}-${step}`} className="flex items-start gap-2">
              <span className="flex h-lh items-center">
                {i === last && running ? (
                  <Loader size="sm" />
                ) : i === last && failed ? (
                  <XCircleIcon className="text-kumo-danger" />
                ) : (
                  <CheckCircleIcon className="text-kumo-success" />
                )}
              </span>
              <Text as="span">{stepText(step, t)}</Text>
            </li>
          ))}
        </ol>
        <span role="status" aria-live="polite" className="sr-only">
          {running ? stepText(data.step, t) : ''}
        </span>
      </div>
      {failed && data.error && (
        <div role="alert">
          <Banner
            variant="error"
            icon={<WarningCircleIcon />}
            title={apiErrorText(new ApiError(0, data.error.code, data.error.message, data.error.args ?? []), t)}
            className="whitespace-pre-line"
          />
        </div>
      )}
    </div>
  )
}
