import { Button, DropdownMenu } from '@cloudflare/kumo'
import { GearSixIcon } from '@phosphor-icons/react'
import { useState } from 'react'
import { useT } from '../../i18n/I18nProvider'

export type ToggleableColumn = { key: string; label: string }

// v2: choices saved before "every column starts visible" could include columns the
// app hid by default, so those older keys are ignored.
const storageKey = (tableId: string) => `hostisan-columns:v2:${tableId}`

function read(tableId: string): string[] | null {
  try {
    const raw = localStorage.getItem(storageKey(tableId))
    return raw ? (JSON.parse(raw) as string[]) : null
  } catch {
    return null
  }
}

/**
 * Which optional columns are visible. Every column starts visible; only the user hides
 * them, and that choice is remembered per table in this browser.
 */
export function useColumnVisibility(tableId: string, columns: ToggleableColumn[]) {
  const [hidden, setHidden] = useState<Set<string>>(() => new Set(read(tableId) ?? []))
  const toggle = (key: string) =>
    setHidden((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      try {
        localStorage.setItem(storageKey(tableId), JSON.stringify([...next]))
      } catch {
        // Storage blocked: the choice still applies until reload.
      }
      return next
    })
  return { isVisible: (key: string) => !hidden.has(key), toggle, columns }
}

/**
 * Gear button that toggles optional columns, as in the Cloudflare dashboard. It lives
 * in the header cell of the (sticky right) actions column:
 *
 *   <Table.Head sticky="right" className="w-px text-right"><ColumnsMenu visibility={columns} /></Table.Head>
 */
export function ColumnsMenu({ visibility }: { visibility: ReturnType<typeof useColumnVisibility> }) {
  const t = useT()
  return (
    <DropdownMenu>
      <DropdownMenu.Trigger
        render={
          <Button
            variant="ghost"
            size="sm"
            shape="square"
            aria-label={t('Edit columns')}
            icon={<GearSixIcon size={18} className="text-kumo-subtle" />}
          />
        }
      />
      <DropdownMenu.Content align="end">
        {visibility.columns.map((c) => (
          <DropdownMenu.CheckboxItem
            key={c.key}
            checked={visibility.isVisible(c.key)}
            onCheckedChange={() => visibility.toggle(c.key)}
            closeOnClick={false}
          >
            {c.label}
          </DropdownMenu.CheckboxItem>
        ))}
      </DropdownMenu.Content>
    </DropdownMenu>
  )
}
