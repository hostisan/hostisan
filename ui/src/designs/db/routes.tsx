import type { RouteObject } from 'react-router'
import { AddDatabasePage, EditDatabasePage } from './DatabaseFormPage'
import { DatabasesPage } from './DatabasesPage'

export const routes: RouteObject[] = [
  { path: '/db', element: <DatabasesPage /> },
  { path: '/db/new', element: <AddDatabasePage /> },
  { path: '/db/:database', element: <EditDatabasePage /> },
]
