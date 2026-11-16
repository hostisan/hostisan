import { Button } from '@cloudflare/kumo'
import { DotsThreeIcon } from '@phosphor-icons/react'

/**
 * The "…" button that opens a row's action menu. Kumo sizes button icons at 1em,
 * which is barely visible in a small button, so the icon size is set explicitly.
 *
 *   <DropdownMenu.Trigger render={rowMenuTrigger(`Actions for ${name}`)} />
 */
export function rowMenuTrigger(label: string) {
  return (
    <Button
      variant="ghost"
      size="sm"
      shape="square"
      aria-label={label}
      icon={<DotsThreeIcon size={20} weight="bold" />}
    />
  )
}
