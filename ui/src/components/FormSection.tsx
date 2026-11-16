import { LayerCard, Text } from '@cloudflare/kumo'
import type { ReactNode } from 'react'

type FormSectionProps = {
  title: string
  description?: ReactNode
  /** Optional control shown next to the title, e.g. a Switch that enables the section. */
  aside?: ReactNode
  children?: ReactNode
}

/**
 * One block of a settings form: title and description on the left, fields in a card
 * on the right (stacked on narrow screens). Put fields directly inside; never nest
 * another LayerCard in here.
 */
export function FormSection({ title, description, aside, children }: FormSectionProps) {
  return (
    <section className="grid gap-4 lg:grid-cols-[16rem_1fr] lg:gap-8">
      <div className="grid h-fit gap-1">
        <div className="flex items-center justify-between gap-3">
          <Text variant="heading" as="h2">
            {title}
          </Text>
          {aside && <div className="lg:hidden">{aside}</div>}
        </div>
        {description && <Text variant="secondary">{description}</Text>}
      </div>
      <LayerCard className="grid gap-4 px-5 py-4">
        {aside && <div className="hidden lg:block">{aside}</div>}
        {children}
      </LayerCard>
    </section>
  )
}
