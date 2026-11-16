import type { RouteObject } from 'react-router'
import { PackageAddPage, PackageEditPage } from './PackageFormPage'
import { PackagesPage } from './PackagesPage'
import { UserAddPage, UserEditPage } from './UserEditPage'
import { UsersPage } from './UsersPage'

export const routes: RouteObject[] = [
  { path: '/users', element: <UsersPage /> },
  { path: '/users/new', element: <UserAddPage /> },
  { path: '/users/:user', element: <UserEditPage /> },
  { path: '/packages', element: <PackagesPage /> },
  { path: '/packages/new', element: <PackageAddPage /> },
  { path: '/packages/:package', element: <PackageEditPage /> },
]
