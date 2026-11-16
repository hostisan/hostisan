import { Banner, Button, Empty, Text } from '@cloudflare/kumo'
import { ArrowClockwiseIcon, TerminalWindowIcon, WarningCircleIcon } from '@phosphor-icons/react'
import { FitAddon } from '@xterm/addon-fit'
import { Terminal } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTerminalToken } from '../../api/tools'
import { ApiError } from '../../api/client'
import { apiErrorText } from '../../api/errors'
import { PageHeader } from '../../components/PageHeader'
import { useT } from '../../i18n/I18nProvider'
import { useSessionUser } from '../../session/SessionProvider'
import { openTerminal, type TerminalConnection } from './terminalSocket'

const cssVar = (name: string, fallback: string) =>
  getComputedStyle(document.documentElement).getPropertyValue(name).trim() || fallback

/** The thin xterm wrapper: colours come from Kumo's CSS variables at mount. */
export function createXterm(host: HTMLElement) {
  const term = new Terminal({
    cursorBlink: true,
    fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
    fontSize: 14,
    theme: {
      background: cssVar('--color-kumo-base', '#ffffff'),
      foreground: cssVar('--text-color-kumo-default', '#1a1a1a'),
      cursor: cssVar('--text-color-kumo-default', '#1a1a1a'),
      selectionBackground: cssVar('--color-kumo-fill', '#cccccc'),
    },
  })
  const fit = new FitAddon()
  term.loadAddon(fit)
  term.open(host)
  return { term, fit }
}

const socketUrl = () =>
  `${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/api/v1/terminal/socket`

/** A 403 other than no_shell is the protected-account refusal; say it for this page. */
function refusalText(err: unknown, t: (s: string) => string): string {
  if (err instanceof ApiError && err.status === 403 && err.code !== 'no_shell' && !err.message)
    return t("The web terminal isn't available for this account.")
  return apiErrorText(err, t)
}

type Phase = { kind: 'connecting' } | { kind: 'open' } | { kind: 'closed'; reason: string; clean: boolean }

function Unavailable({ readOnly }: { readOnly: boolean }) {
  const t = useT()
  return (
    <div className="grid gap-6">
      <PageHeader title={t('Web terminal')} />
      <Empty
        icon={<TerminalWindowIcon size={40} className="text-kumo-inactive" />}
        title={t('The web terminal is not available')}
        description={
          readOnly
            ? t("Read-only sessions can't use the web terminal.")
            : t('The web terminal is not turned on for this account.')
        }
        className="py-16"
      />
    </div>
  )
}

export function TerminalPage() {
  const user = useSessionUser()
  const enabled = !!user?.features.terminal && !user.readOnly
  if (!enabled) return <Unavailable readOnly={!!user?.readOnly} />
  return <TerminalSession landing={user?.landing || '/'} />
}

function TerminalSession({ landing }: { landing: string }) {
  const t = useT()
  const hostRef = useRef<HTMLDivElement>(null)
  const xr = useRef<ReturnType<typeof createXterm> | null>(null)
  const conn = useRef<TerminalConnection | null>(null)
  const alive = useRef(true)
  const token = useTerminalToken()
  const [phase, setPhase] = useState<Phase>({ kind: 'connecting' })
  const [error, setError] = useState<unknown>(null)

  const connect = useCallback((focus = false) => {
    const x = xr.current
    if (!x) return
    conn.current?.close()
    conn.current = null
    x.term.reset()
    setError(null)
    setPhase({ kind: 'connecting' })
    if (focus) x.term.focus()
    token.mutate(undefined, {
      onSuccess: (r) => {
        if (!alive.current) return
        x.fit.fit()
        conn.current = openTerminal({
          url: socketUrl(),
          token: r.token,
          cols: x.term.cols,
          rows: x.term.rows,
          onData: (d) => {
            setPhase((p) => {
              if (p.kind === 'open') return p
              x.term.focus()
              token.reset()
              return { kind: 'open' }
            })
            x.term.write(d)
          },
          onClose: (reason, clean) => setPhase({ kind: 'closed', reason, clean }),
        })
      },
      onError: (e) => alive.current && setError(e),
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    alive.current = true
    const host = hostRef.current
    if (!host) return
    const x = createXterm(host)
    xr.current = x
    const input = x.term.onData((d) => conn.current?.send(d))
    let timer: ReturnType<typeof setTimeout> | undefined
    let last = ''
    const observer = new ResizeObserver(() => {
      clearTimeout(timer)
      timer = setTimeout(() => {
        try {
          x.fit.fit()
        } catch {
          return
        }
        const dims = `${x.term.cols}x${x.term.rows}`
        if (dims !== last) {
          last = dims
          conn.current?.resize(x.term.cols, x.term.rows)
        }
      }, 100)
    })
    observer.observe(host)
    connect(false)
    return () => {
      alive.current = false
      clearTimeout(timer)
      observer.disconnect()
      input.dispose()
      conn.current?.close()
      conn.current = null
      x.term.dispose()
      xr.current = null
    }
  }, [connect])

  return (
    <div className="grid gap-4">
      <PageHeader
        title={t('Web terminal')}
        back={{ href: landing, label: t('Back') }}
        actions={
          (phase.kind === 'closed' || error != null) && (
            <Button variant="primary" icon={ArrowClockwiseIcon} onClick={() => connect(true)}>
              {t('Reconnect')}
            </Button>
          )
        }
      />
      {error != null && <Banner variant="error" icon={<WarningCircleIcon />} title={refusalText(error, t)} />}
      {phase.kind === 'closed' && (
        <Banner
          variant={phase.clean ? 'default' : 'error'}
          icon={phase.clean ? undefined : <WarningCircleIcon />}
          title={t(phase.reason)}
        />
      )}
      {phase.kind === 'connecting' && error == null && <Text variant="secondary">{t('Connecting…')}</Text>}
      <div
        data-testid="terminal"
        ref={hostRef}
        className="h-[calc(100dvh-16rem)] min-h-72 w-full overflow-hidden rounded-lg bg-kumo-base p-2 ring ring-kumo-line"
      />
    </div>
  )
}
