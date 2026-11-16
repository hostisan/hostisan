import { Tabs } from '@cloudflare/kumo'
import { useNavigate } from 'react-router'
import { useT } from '../../i18n/I18nProvider'
import { useSessionUser } from '../../session/SessionProvider'

/** Switches between the firewall lists; the ban list only exists with a firewall extension. */
export function FirewallNav({ current }: { current: string }) {
  const navigate = useNavigate()
  const t = useT()
  const bans = useSessionUser()?.features.firewallBans ?? false
  const items = [
    { value: '/server/firewall', label: t('Rules') },
    ...(bans ? [{ value: '/server/firewall/banlist', label: t('Banned IP addresses') }] : []),
    { value: '/server/firewall/ipsets', label: t('IPset IP lists') },
  ]
  return (
    <div className="-mx-4 overflow-x-auto px-4 sm:mx-0 sm:px-0">
      <Tabs variant="underline" value={current} onValueChange={(v) => navigate(v)} tabs={items} className="min-w-max" />
    </div>
  )
}
