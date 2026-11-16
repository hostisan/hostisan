import { Navigate, type RouteObject } from 'react-router'
import { FirewallBanlistPage } from './FirewallBanlistPage'
import { FirewallIpsetsPage } from './FirewallIpsetsPage'
import { FirewallBanFormPage, FirewallIpsetFormPage } from './FirewallListFormPages'
import { FirewallRuleFormPage } from './FirewallRuleFormPage'
import { FirewallRulesPage } from './FirewallRulesPage'
import { GraphsPage } from './GraphsPage'
import { IpFormPage } from './IpFormPage'
import { IpListPage } from './IpListPage'
import { PreviewPage } from './PreviewPage'
import { ServerConfigPage } from './ServerConfigPage'
import { ServerInfoPage } from './ServerInfoPage'
import { ServiceEditPage } from './ServiceEditPage'
import { ServicesPage } from './ServicesPage'
import { UpdatesPage } from './UpdatesPage'
import { WhiteLabelPage } from './WhiteLabelPage'

export const routes: RouteObject[] = [
  { path: '/server', element: <Navigate to="/server/services" replace /> },
  { path: '/server/services', element: <ServicesPage /> },
  { path: '/server/services/:service', element: <ServiceEditPage /> },
  { path: '/server/config', element: <ServerConfigPage /> },
  { path: '/server/info', element: <ServerInfoPage /> },
  { path: '/server/ip', element: <IpListPage /> },
  { path: '/server/ip/new', element: <IpFormPage key="add" mode="add" /> },
  { path: '/server/ip/:ip', element: <IpFormPage key="edit" mode="edit" /> },
  { path: '/server/firewall', element: <FirewallRulesPage /> },
  { path: '/server/firewall/new', element: <FirewallRuleFormPage key="add" mode="add" /> },
  { path: '/server/firewall/banlist', element: <FirewallBanlistPage /> },
  { path: '/server/firewall/banlist/new', element: <FirewallBanFormPage /> },
  { path: '/server/firewall/ipsets', element: <FirewallIpsetsPage /> },
  { path: '/server/firewall/ipsets/new', element: <FirewallIpsetFormPage /> },
  { path: '/server/firewall/:rule', element: <FirewallRuleFormPage key="edit" mode="edit" /> },
  { path: '/server/updates', element: <UpdatesPage /> },
  { path: '/server/preview', element: <PreviewPage /> },
  { path: '/server/graphs', element: <GraphsPage /> },
  { path: '/server/white-label', element: <WhiteLabelPage /> },
]
