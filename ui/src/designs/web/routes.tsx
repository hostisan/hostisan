import type { RouteObject } from 'react-router'
import { AddWebDomainPage } from './AddWebDomainPage'
import { EditWebDomainPage } from './EditWebDomainPage'
import { GenerateSslPage } from './GenerateSslPage'
import { SetupWebAppPage } from './SetupWebAppPage'
import { SslCertificatePage } from './SslCertificatePage'
import { WebAppsPage } from './WebAppsPage'
import { WebDomainsPage } from './WebDomainsPage'
import { WebLogsPage } from './WebLogsPage'

export const routes: RouteObject[] = [
  { path: '/web', element: <WebDomainsPage /> },
  { path: '/web/new', element: <AddWebDomainPage /> },
  { path: '/web/:domain', element: <EditWebDomainPage /> },
  { path: '/web/:domain/ssl', element: <SslCertificatePage /> },
  { path: '/web/:domain/ssl/generate', element: <GenerateSslPage /> },
  { path: '/web/:domain/apps', element: <WebAppsPage /> },
  { path: '/web/:domain/apps/:app', element: <SetupWebAppPage /> },
  { path: '/web/:domain/logs', element: <WebLogsPage /> },
]
