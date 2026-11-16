import type { ReactNode } from 'react'

/** Label/value rows, like Hestia's `values-list`. */
export function ValuesList({ items }: { items: { label: string; value: ReactNode }[] }) {
  return (
    <dl className="grid gap-x-6 gap-y-2.5 sm:grid-cols-[10rem_1fr]">
      {items.map((item) => (
        <div key={item.label} className="contents">
          <dt className="text-kumo-subtle">{item.label}</dt>
          <dd className="min-w-0 break-words">{item.value}</dd>
        </div>
      ))}
    </dl>
  )
}
