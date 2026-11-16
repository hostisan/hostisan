import { Link, Text } from '@cloudflare/kumo'
import { ArrowLeftIcon } from '@phosphor-icons/react'
import type { ReactNode } from 'react'
import { shortcutTarget } from '../shortcuts/targets'

type PageHeaderProps = {
  title: string
  description?: ReactNode
  actions?: ReactNode
  /** Link back to the parent list, shown above the title on add/edit pages (Ctrl+Backspace follows it). */
  back?: { href: string; label: string }
}

export function PageHeader({ title, description, actions, back }: PageHeaderProps) {
  return (
    <div className="grid gap-3">
      {back && (
        <Link href={back.href} className="flex w-fit items-center gap-1.5 text-kumo-subtle" {...shortcutTarget('back')}>
          <ArrowLeftIcon />
          {back.label}
        </Link>
      )}
      <div className="flex min-w-0 flex-wrap items-end justify-between gap-4">
        <div className="grid min-w-0 gap-1">
          <Text variant="heading" size="lg" as="h1">
            {title}
          </Text>
          {description && <Text variant="secondary">{description}</Text>}
        </div>
        {actions && <div className="flex min-w-0 flex-wrap items-center gap-2">{actions}</div>}
      </div>
    </div>
  )
}
