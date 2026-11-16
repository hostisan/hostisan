import { Badge, Button, LinkButton, Popover, Text, useKumoToastManager } from '@cloudflare/kumo'
import { BellIcon, BellSlashIcon, XIcon } from '@phosphor-icons/react'
import { useState } from 'react'
import { useNotificationMutation, useNotifications } from '../api/account'
import { apiErrorText } from '../api/errors'
import { SafeNotice } from '../components/SafeNotice'
import { useT } from '../i18n/I18nProvider'
import { useSession, useSessionUser } from '../session/SessionProvider'

/** The topbar's notifications bell and its popover. The list loads on the first click. */
export function NotificationsTray() {
  const user = useSessionUser()
  const t = useT()
  const toast = useKumoToastManager()
  const { refresh } = useSession()
  const [open, setOpen] = useState(false)
  const [opened, setOpened] = useState(false)
  const list = useNotifications({ enabled: opened })
  const m = useNotificationMutation()
  const items = list.data?.items ?? []
  const loaded = list.data !== undefined
  // The session's flag covers what is not loaded yet; the list adds what it shows.
  const unread = (user?.notifications ?? false) || items.some((n) => n.ACK === 'no')
  // The protected admin has no tray; other read-only sessions can read but not change.
  const readOnly = user?.readOnly ?? false
  if (readOnly && user?.role === 'admin') return null

  const run = (call: () => Promise<unknown>, closeAfter = false) =>
    call().then(
      () => {
        if (closeAfter) setOpen(false)
        return refresh()
      },
      (err) => toast.add({ title: t('Notifications'), description: apiErrorText(err, t), type: 'error' }),
    )

  return (
    <Popover
      open={open}
      onOpenChange={(o) => {
        setOpen(o)
        if (o) {
          // Opened before: the list may be stale, so ask again.
          if (opened) void list.refetch()
          setOpened(true)
        }
      }}
    >
      <Popover.Trigger
        render={
          <Button
            variant="ghost"
            shape="square"
            icon={<BellIcon size={18} />}
            aria-label={unread ? t('Notifications, unread') : t('Notifications')}
            className="relative"
          >
            {unread && <span className="absolute top-1 right-1 size-2 rounded-full bg-kumo-danger" />}
          </Button>
        }
      />
      <Popover.Content className="w-80 p-0">
        <div className="flex items-center justify-between gap-3 border-b border-kumo-line px-4 py-3">
          <Popover.Title>{t('Notifications')}</Popover.Title>
          {!readOnly && items.length > 2 && (
            <Button variant="ghost" onClick={() => run(m.removeAll, true)}>
              {t('Delete all notifications')}
            </Button>
          )}
        </div>
        {list.isError ? (
          <div className="px-4 py-3">
            <Text variant="secondary">{apiErrorText(list.error, t)}</Text>
          </div>
        ) : !loaded ? (
          <div className="px-4 py-3" role="status">
            <Text variant="secondary">{t('Loading...')}</Text>
          </div>
        ) : items.length === 0 ? (
          <div className="grid justify-items-center gap-2 px-4 py-8">
            <BellSlashIcon size={32} className="text-kumo-inactive" />
            <Text variant="secondary">{t('No notifications')}</Text>
          </div>
        ) : (
          <ul className="grid max-h-80 overflow-y-auto">
            {items.map((n) => (
              <li key={n.ID} className="grid gap-0.5 border-b border-kumo-hairline px-4 py-3 last:border-b-0">
                <div className="flex items-start gap-2">
                  <span className="flex flex-wrap items-center gap-2">
                    <Text bold as="span">
                      {n.TOPIC}
                    </Text>
                    {n.ACK === 'no' && <Badge variant="info">{t('New')}</Badge>}
                  </span>
                  {!readOnly && (
                    <span className="ml-auto flex h-lh items-center">
                      <Button
                        variant="ghost"
                        size="sm"
                        shape="square"
                        icon={<XIcon size={14} />}
                        aria-label={t('Delete notification')}
                        onClick={() => run(() => m.remove(n.ID), items.length === 1)}
                      />
                    </span>
                  )}
                </div>
                <SafeNotice html={n.NOTICE} className="text-kumo-subtle" />
                <Text variant="secondary">
                  {n.DATE} {n.TIME}
                </Text>
              </li>
            ))}
          </ul>
        )}
        <div className="border-t border-kumo-line p-1.5">
          <LinkButton
            href="/notifications"
            variant="ghost"
            className="w-full justify-center"
            onClick={() => setOpen(false)}
          >
            {t('View all notifications')}
          </LinkButton>
        </div>
      </Popover.Content>
    </Popover>
  )
}
