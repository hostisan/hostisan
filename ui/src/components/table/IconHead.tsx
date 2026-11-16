import { Table, Tooltip } from '@cloudflare/kumo'
import type { Icon as PhosphorIcon } from '@phosphor-icons/react'

/** Icon-only count column header, as Hestia labels its count columns; keeps wide tables compact. */
export function IconHead({ icon: Icon, label }: { icon: PhosphorIcon; label: string }) {
  return (
    <Table.Head className="text-right">
      <Tooltip content={label}>
        <span className="inline-flex h-lh items-center align-middle" aria-label={label}>
          <Icon size={16} aria-hidden />
        </span>
      </Tooltip>
    </Table.Head>
  )
}
