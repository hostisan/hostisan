import { Meter, Sidebar, Text, useSidebar } from '@cloudflare/kumo'
import { useLocation } from 'react-router'
import { matches } from '../shortcuts/keys'
import { useShortcutHandler } from '../shortcuts/ShortcutsProvider'
import { useT } from '../i18n/I18nProvider'
import { formatMB, usagePercent } from '../lib/format'
import { useSession, useSessionUser } from '../session/SessionProvider'
import { hasFeature, navGroups } from './nav'

function BrandMark() {
  return (
    <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-kumo-brand text-sm font-semibold text-white">
      H
    </span>
  )
}

function SidebarBrand() {
  const { state } = useSidebar()
  const appName = useSession().bootstrap?.appName ?? 'Hostisan'
  return (
    <div className="flex items-center gap-2 px-2 py-1.5">
      <BrandMark />
      {state !== 'collapsed' && (
        <Text variant="heading" as="span">
          {appName}
        </Text>
      )}
    </div>
  )
}

function UsageMeters() {
  const { state } = useSidebar()
  const usage = useSessionUser()?.usage
  const t = useT()
  if (state === 'collapsed' || !usage) return null
  const disk = usagePercent(usage.diskUsed, usage.diskQuota)
  const bandwidth = usagePercent(usage.bandwidthUsed, usage.bandwidthQuota)
  return (
    <Sidebar.Group>
      <Sidebar.GroupLabel>{t('Usage')}</Sidebar.GroupLabel>
      <div className="grid gap-3 px-2 py-1.5">
        <Meter
          label={t('Disk')}
          value={disk ?? 0}
          customValue={`${formatMB(usage.diskUsed)} / ${formatMB(usage.diskQuota)}`}
        />
        <Meter
          label={t('Bandwidth')}
          value={bandwidth ?? 0}
          customValue={`${formatMB(usage.bandwidthUsed)} / ${formatMB(usage.bandwidthQuota)}`}
        />
      </div>
    </Sidebar.Group>
  )
}

const navAttr = 'data-shortcut-nav'
const sectionProps = { [navAttr]: '' }

/**
 * Hestia's menu keys: ← and → move focus across the top-level sections (wrapping,
 * starting from either end), and Enter opens the focused one.
 */
function useSectionKeys() {
  useShortcutHandler((e) => {
    const sections = [...document.querySelectorAll<HTMLElement>(`[${navAttr}]`)]
    const at = sections.indexOf(document.activeElement as HTMLElement)
    if (matches(e, { key: 'Enter' })) {
      if (at < 0) return false
      sections[at].click()
      return true
    }
    const step = matches(e, { key: 'ArrowRight' }) ? 1 : matches(e, { key: 'ArrowLeft' }) ? -1 : 0
    if (step === 0 || sections.length === 0) return false
    const next = at < 0 ? (step > 0 ? 0 : sections.length - 1) : (at + step + sections.length) % sections.length
    sections[next].focus()
    return true
  })
}

export function AppSidebar() {
  const { pathname } = useLocation()
  const user = useSessionUser()
  const isAdmin = user?.role === 'admin' && user.look === ''
  const t = useT()
  useSectionKeys()

  return (
    <Sidebar>
      <Sidebar.Header>
        <SidebarBrand />
      </Sidebar.Header>
      <Sidebar.Content>
        {navGroups
          .filter((group) => !group.adminOnly || isAdmin)
          .map((group) => (
            <Sidebar.Group key={group.label}>
              <Sidebar.GroupLabel>{t(group.label)}</Sidebar.GroupLabel>
              <Sidebar.Menu>
                {group.items
                  .filter((item) => hasFeature(item, user?.features))
                  .map((item) =>
                    item.children ? (
                      <Sidebar.MenuItem key={item.path}>
                        <Sidebar.Collapsible defaultOpen={pathname.startsWith(item.path)}>
                          <Sidebar.CollapsibleTrigger
                            render={
                              <Sidebar.MenuButton
                                {...sectionProps}
                                icon={item.icon}
                                tooltip={t(item.label)}
                                active={pathname.startsWith(item.path)}
                              >
                                {t(item.label)}
                                <Sidebar.MenuChevron />
                              </Sidebar.MenuButton>
                            }
                          />
                          <Sidebar.CollapsibleContent>
                            <Sidebar.MenuSub>
                              {item.children
                                .filter((child) => hasFeature(child, user?.features))
                                .map((child) => (
                                  <Sidebar.MenuSubButton
                                    key={child.path}
                                    href={child.path}
                                    active={pathname.startsWith(child.path)}
                                  >
                                    {t(child.label)}
                                  </Sidebar.MenuSubButton>
                                ))}
                            </Sidebar.MenuSub>
                          </Sidebar.CollapsibleContent>
                        </Sidebar.Collapsible>
                      </Sidebar.MenuItem>
                    ) : (
                      <Sidebar.MenuButton
                        {...sectionProps}
                        key={item.path}
                        href={item.path}
                        icon={item.icon}
                        tooltip={t(item.label)}
                        active={pathname.startsWith(item.path)}
                      >
                        {t(item.label)}
                      </Sidebar.MenuButton>
                    ),
                  )}
              </Sidebar.Menu>
            </Sidebar.Group>
          ))}
        <UsageMeters />
      </Sidebar.Content>
      <Sidebar.Footer>
        <Sidebar.Trigger />
      </Sidebar.Footer>
    </Sidebar>
  )
}
