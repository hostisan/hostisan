import { Button, Dialog } from '@cloudflare/kumo'
import { XIcon } from '@phosphor-icons/react'
import { useT } from '../i18n/I18nProvider'
import { msgid } from '../i18n/msgid'
import { isMac } from './keys'
import { useShortcutsDialog } from './ShortcutsProvider'

const ctrl = isMac ? '⌘' : 'Ctrl'

/** One help row: its key combos (each a list of caps; alternatives side by side) and what it does. */
type Row = { keys: string[][]; label: string }

// Hestia's help dialog (footer.php), its msgids matched ignoring case, plus Ctrl+Shift+A,
// which Hestia registered but never listed. Ctrl+Backspace reads "Leave form without
// saving" instead of Hestia's unclear "Unsave form" (O10).
const columns: Row[][] = [
  [
    { keys: [['a']], label: msgid('Add new object') },
    { keys: [[ctrl, 'Enter']], label: msgid('Save form') },
    { keys: [[ctrl, 'Backspace']], label: msgid('Leave form without saving') },
    { keys: [[ctrl, 'Shift', 'A']], label: msgid('Select all') },
    { keys: [['1']], label: msgid('Go to web list') },
    { keys: [['2']], label: msgid('Go to DNS list') },
    { keys: [['3']], label: msgid('Go to mail list') },
    { keys: [['4']], label: msgid('Go to DB list') },
    { keys: [['5']], label: msgid('Go to cron list') },
    { keys: [['6']], label: msgid('Go to backup list') },
  ],
  [
    { keys: [['f']], label: msgid('Focus on search') },
    { keys: [['h'], ['?']], label: msgid('Display / Hide shortcuts') },
    { keys: [['←']], label: msgid('Move backward through top menu') },
    { keys: [['→']], label: msgid('Move forward through top menu') },
    { keys: [['Enter']], label: msgid('Enter focused element') },
    { keys: [['↑']], label: msgid('Move up through elements list') },
    { keys: [['↓']], label: msgid('Move down through elements list') },
  ],
]

function Keys({ combos }: { combos: string[][] }) {
  return (
    <span className="flex shrink-0 items-center gap-1.5">
      {combos.map((combo) => (
        <span key={combo.join('+')} className="flex items-center gap-1">
          {combo.map((cap) => (
            <kbd key={cap} className="rounded-md px-1.5 font-mono text-[0.9em] ring ring-kumo-line">
              {cap}
            </kbd>
          ))}
        </span>
      ))}
    </span>
  )
}

/** The keyboard help (h or ?, the user menu, the palette). Always mounted; `open` shows it. */
export function ShortcutsDialog() {
  const t = useT()
  const { open, setOpen } = useShortcutsDialog()
  return (
    <Dialog.Root open={open} onOpenChange={setOpen}>
      <Dialog size="xl" className="grid gap-4 px-6 py-5">
        <div data-shortcuts-dialog className="flex items-center justify-between gap-4">
          <Dialog.Title className="text-lg font-semibold">{t('Shortcuts')}</Dialog.Title>
          <Dialog.Close
            render={(props) => (
              <Button {...props} variant="ghost" shape="square" icon={XIcon} aria-label={t('Close')} />
            )}
          />
        </div>
        <div data-shortcut-columns className="grid grid-cols-1 gap-x-8 gap-y-2 sm:grid-cols-2">
          {columns.map((rows) => (
            <ul key={rows[0].label} className="grid content-start gap-2">
              {rows.map((row) => (
                <li key={row.label} className="flex items-center justify-between gap-4">
                  <span className="min-w-0">{t(row.label)}</span>
                  <Keys combos={row.keys} />
                </li>
              ))}
            </ul>
          ))}
        </div>
      </Dialog>
    </Dialog.Root>
  )
}
