import { LayerCard, Meter, Text, Tooltip } from '@cloudflare/kumo'
import { InfoIcon } from '@phosphor-icons/react'
import { createContext, useContext, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { useT } from '../i18n/I18nProvider'

/** The rail element AppLayout renders beside the page; PageAside portals into it. */
export const PageAsideSlot = createContext<HTMLElement | null>(null)

/**
 * Right-hand summary rail (usage, stats, details), like the Cloudflare dashboard's
 * overview pages. Use it only where a page has real side information to show.
 */
export function PageAside({ children }: { children: ReactNode }) {
  const slot = useContext(PageAsideSlot)
  return slot ? createPortal(children, slot) : null
}

export function AsideSection({
  title,
  action,
  children,
}: {
  title: string
  action?: ReactNode
  children: ReactNode
}) {
  return (
    <section className="grid content-start gap-4">
      <div className="flex items-center justify-between gap-3">
        <Text variant="heading" size="lg" as="h2">
          {title}
        </Text>
        {action}
      </div>
      {children}
    </section>
  )
}

/** "used / limit" with a bar; Hestia's "unlimited" limits show no bar. */
export function UsageMeter({ label, used, limit }: { label: string; used: number; limit: string }) {
  const t = useT()
  const max = Number(limit)
  // A limit of 0 means none allowed, as Hestia shows it; only "unlimited" is open-ended.
  if (limit === 'unlimited' || !max) {
    const shown = limit === '0' ? '0' : t('Unlimited')
    return (
      <div className="flex items-center justify-between gap-3">
        <Text variant="secondary" as="span">
          {label}
        </Text>
        <Text as="span">
          {used.toLocaleString()} / {shown}
        </Text>
      </div>
    )
  }
  return (
    <Meter
      label={label}
      value={Math.min(100, (used / max) * 100)}
      customValue={`${used.toLocaleString()} / ${max.toLocaleString()}`}
    />
  )
}

export function StatGrid({ children }: { children: ReactNode }) {
  return <div className="grid grid-cols-2 gap-3">{children}</div>
}

export function StatTile({ label, value, hint }: { label: string; value: ReactNode; hint?: string }) {
  const t = useT()
  return (
    <LayerCard className="grid gap-1.5 px-4 py-3.5">
      <div className="flex items-center gap-1 text-kumo-subtle">
        <Text variant="secondary" size="sm" as="span">
          {label}
        </Text>
        {hint && (
          <Tooltip
            content={hint}
            render={
              <button type="button" aria-label={t('About %s', label.toLowerCase())} className="flex">
                <InfoIcon size={14} />
              </button>
            }
          />
        )}
      </div>
      <span className="text-2xl font-semibold text-kumo-default tabular-nums">{value}</span>
    </LayerCard>
  )
}

export function AsideCard({
  title,
  description,
  children,
}: {
  title: string
  description?: ReactNode
  children?: ReactNode
}) {
  return (
    <LayerCard className="grid gap-3 px-4 py-3.5">
      <div className="grid gap-1">
        <Text bold as="h3">
          {title}
        </Text>
        {description && <Text variant="secondary">{description}</Text>}
      </div>
      {children}
    </LayerCard>
  )
}

/** Label/value row for an AsideCard; rows after the first get a divider. */
export function DetailRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-4 border-kumo-hairline py-2 not-first:border-t">
      <span className="shrink-0 text-kumo-subtle">{label}</span>
      <div className="min-w-0 truncate text-right">{children}</div>
    </div>
  )
}
