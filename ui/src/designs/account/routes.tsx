import type { RouteObject } from 'react-router'
import { RequireFeature } from '../../layout/RequireFeature'
import { AccessKeyAddPage, AccessKeyPage, AccessKeysPage } from './AccessKeysPage'
import { AccountPage } from './AccountPage'
import { AuthLogPage, LogsPage } from './LogsPage'
import { SshKeyAddPage, SshKeysPage } from './SshKeysPage'
import { StatsPage } from './StatsPage'

export const routes: RouteObject[] = [
  { path: '/account', element: <AccountPage /> },
  { path: '/account/ssh-keys', element: <SshKeysPage /> },
  { path: '/account/ssh-keys/new', element: <SshKeyAddPage /> },
  {
    element: <RequireFeature feature="accessKeys" />,
    children: [
      { path: '/account/access-keys', element: <AccessKeysPage /> },
      { path: '/account/access-keys/new', element: <AccessKeyAddPage /> },
      { path: '/account/access-keys/:key', element: <AccessKeyPage /> },
    ],
  },
  { path: '/logs', element: <LogsPage /> },
  { path: '/logs/auth', element: <AuthLogPage /> },
  { path: '/stats', element: <StatsPage /> },
]
