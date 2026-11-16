import { Badge, Flow, Text } from '@cloudflare/kumo'
import { DesktopIcon, LightningIcon, LockSimpleIcon, type Icon } from '@phosphor-icons/react'
import { type ReactNode, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useT } from '../../i18n/I18nProvider'
import { formatBackend } from '../../lib/format'

type RequestFlowProps = {
  domain: string
  ssl: boolean
  letsEncrypt: boolean
  proxy: boolean
  proxyTemplate: string
  fastcgiCache: boolean
  template: string
  backend: string
  /** The raw WEB_SYSTEM, WEB_BACKEND and PROXY_SYSTEM values of GET /web/options. */
  webSystem: string
  webBackend: string
  proxySystem: string
}

function Node({
  title,
  detail,
  icon: IconComponent,
  extra,
}: {
  title: string
  detail: ReactNode
  icon?: Icon
  extra?: ReactNode
}) {
  return (
    <div className="grid gap-1">
      <div className="flex items-center gap-1.5">
        {IconComponent && <IconComponent className="text-kumo-subtle" />}
        <Text bold as="span">
          {title}
        </Text>
      </div>
      <Text variant="secondary" as="span">
        {detail}
      </Text>
      {extra && <div className="flex flex-wrap gap-1 pt-0.5">{extra}</div>}
    </div>
  )
}

const label = (system: string) =>
  ({ nginx: 'NGINX', apache2: 'Apache', 'php-fpm': 'PHP-FPM' })[system] ?? system.toUpperCase()

/**
 * Horizontal while the diagram fits its box, stacked vertically when it doesn't (owner
 * ruling 22): measured, so nothing is clipped and the box never scrolls. The horizontal
 * width that overflowed is remembered, and a resize wider than it tries horizontal again;
 * a resize that makes the horizontal layout overflow stacks it.
 * A change of content (key) tries horizontal again too.
 */
function useFitLayout(key: string) {
  const ref = useRef<HTMLDivElement>(null)
  const [orientation, setOrientation] = useState<'horizontal' | 'vertical'>('horizontal')
  const needed = useRef(0)
  const shown = useRef(key)
  const current = useRef(orientation)

  // Before paint: new content tries horizontal first; a horizontal layout that overflows is stacked.
  useLayoutEffect(() => {
    current.current = orientation
    if (shown.current !== key) {
      shown.current = key
      if (orientation === 'vertical') {
        setOrientation('horizontal')
        return
      }
    }
    const el = ref.current
    if (el && orientation === 'horizontal' && el.scrollWidth > el.clientWidth) {
      needed.current = el.scrollWidth
      setOrientation('vertical')
    }
  })

  useEffect(() => {
    const el = ref.current
    if (!el) return
    const observer = new ResizeObserver(() => {
      if (current.current === 'vertical') {
        if (el.clientWidth >= needed.current) setOrientation('horizontal')
      } else if (el.scrollWidth > el.clientWidth) {
        needed.current = el.scrollWidth
        setOrientation('vertical')
      }
    })
    observer.observe(el)
    return () => observer.disconnect()
  }, [])

  return { ref, orientation }
}

/** Visitor → proxy → web server → PHP, derived from the domain's current (unsaved) settings. */
export function RequestFlow(props: RequestFlowProps) {
  const {
    domain,
    ssl,
    letsEncrypt,
    proxy,
    proxyTemplate,
    fastcgiCache,
    template,
    backend,
    webSystem,
    webBackend,
    proxySystem,
  } = props
  const t = useT()
  const hasProxy = !!proxySystem && proxy
  const caching = fastcgiCache || (hasProxy && proxyTemplate.startsWith('caching'))
  const noPhp = backend === 'no-php'
  const sslBadge = ssl ? (
    <Badge variant={letsEncrypt ? 'green' : 'blue'}>
      <LockSimpleIcon className="mr-1 inline" />
      {letsEncrypt ? "SSL · Let's Encrypt" : t('SSL · custom certificate')}
    </Badge>
  ) : (
    <Badge variant="neutral">{t('HTTP only')}</Badge>
  )

  const layout = useFitLayout(
    [domain, ssl, letsEncrypt, hasProxy, proxyTemplate, caching, template, backend, webSystem, webBackend, proxySystem].join('|'),
  )

  return (
    <div
      ref={layout.ref}
      data-testid="request-flow"
      data-orientation={layout.orientation}
      className="-mx-1 min-w-0 overflow-x-hidden px-1"
    >
      <Flow
        canvas={false}
        orientation={layout.orientation}
        align={layout.orientation === 'horizontal' ? 'center' : 'start'}
        padding={{ x: 2, y: 2 }}
        className="overflow-visible"
      >
        <Flow.Node>
          <Node
            title={t('Visitor')}
            icon={DesktopIcon}
            detail={<span className="font-mono text-[0.9em] break-all">{`${ssl ? 'https' : 'http'}://${domain}`}</span>}
            extra={sslBadge}
          />
        </Flow.Node>
        {hasProxy && (
          <Flow.Node>
            <Node
              title={label(proxySystem)}
              detail={`${t('Template')}: ${proxyTemplate}`}
              extra={
                caching ? (
                  <Badge variant="orange">
                    <LightningIcon className="mr-1 inline" />
                    {t('Cache')}
                  </Badge>
                ) : undefined
              }
            />
          </Flow.Node>
        )}
        <Flow.Node>
          <Node title={label(webSystem)} detail={`${t('Template')}: ${template}`} />
        </Flow.Node>
        {webBackend && (
          <Flow.Node disabled={noPhp}>
            <Node
              title={label(webBackend)}
              detail={noPhp ? t('PHP disabled') : backend === 'default' ? t('Server default PHP') : formatBackend(backend)}
            />
          </Flow.Node>
        )}
      </Flow>
    </div>
  )
}
