import { Navigate, type RouteObject } from 'react-router'
import { EmptyPackagePage } from './EmptyPackagePage'
import { FilesPage } from './FilesPage'
import { NotificationsPage } from './NotificationsPage'
import { SearchPage } from './SearchPage'
import { TerminalPage } from './TerminalPage'

export const routes: RouteObject[] = [
  { path: '/search', element: <SearchPage /> },
  { path: '/terminal', element: <TerminalPage /> },
  { path: '/files', element: <FilesPage /> },
  // The empty-package page (decision 30).
  { path: '/error', element: <EmptyPackagePage /> },
  // Hestia's old /list/<section>/ bookmarks land on the start page (FM11, GAP11).
  { path: '/list/*', element: <Navigate to="/" replace /> },
  { path: '/notifications', element: <NotificationsPage /> },
]
