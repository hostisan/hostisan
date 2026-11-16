import { Banner, Button, Empty } from '@cloudflare/kumo'
import { ArrowClockwiseIcon, FolderOpenIcon, WarningCircleIcon } from '@phosphor-icons/react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiError } from '../../api/client'
import { apiErrorText } from '../../api/errors'
import { useFilesHandoff } from '../../api/tools'
import { PageHeader } from '../../components/PageHeader'
import { DnsLoading } from '../dns/shared'
import { useT } from '../../i18n/I18nProvider'
import { useSessionUser } from '../../session/SessionProvider'

function refusalText(err: unknown, t: (s: string) => string): string {
  if (err instanceof ApiError && err.status === 403 && !err.message)
    return t("The file manager isn't available for this account.")
  return apiErrorText(err, t)
}

/** The file-manager handoff (/files): prepare its session, then leave for /fm/. */
export function FilesPage() {
  const t = useT()
  const user = useSessionUser()
  const readOnly = !!user?.readOnly
  const enabled = !!user?.features.fileManager && !readOnly
  const handoff = useFilesHandoff()
  // A ref survives StrictMode's second effect run, so the request goes out once.
  const started = useRef(false)
  const [badUrl, setBadUrl] = useState(false)

  const go = useCallback(
    () =>
      handoff.mutate(undefined, {
        onSuccess: (r) => {
          // Only a path on this site: never "//host" or a full URL.
          if (/^\/(?!\/)/.test(r.url)) window.location.assign(r.url)
          else setBadUrl(true)
        },
      }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  )
  useEffect(() => {
    if (!enabled || started.current) return
    started.current = true
    go()
  }, [enabled, go])

  if (!enabled) {
    return (
      <div className="grid gap-6">
        <PageHeader title={t('File manager')} />
        <Empty
          icon={<FolderOpenIcon size={40} className="text-kumo-inactive" />}
          title={t('The file manager is not available')}
          description={
            readOnly
              ? t("Read-only sessions can't use the file manager.")
              : t('The file manager is not turned on for this account.')
          }
          className="py-16"
        />
      </div>
    )
  }
  return (
    <div className="grid gap-6">
      <PageHeader title={t('File manager')} />
      {handoff.isError || badUrl ? (
        <div className="grid justify-items-start gap-3">
          <Banner
            variant="error"
            icon={<WarningCircleIcon />}
            title={badUrl ? t("The file manager couldn't be opened.") : refusalText(handoff.error, t)}
            className="w-full"
          />
          <Button
            icon={ArrowClockwiseIcon}
            onClick={() => {
              setBadUrl(false)
              go()
            }}
          >
            {t('Retry')}
          </Button>
        </div>
      ) : (
        <DnsLoading />
      )}
    </div>
  )
}
