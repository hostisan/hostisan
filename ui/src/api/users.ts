import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './client'
import type { Package } from './hestia.gen'
import type {
  BulkReply,
  OK,
  PackageList,
  PackageOptions,
  PackageRequest,
  PackageSaved,
  UserBulkRequest,
  UserList,
} from './types.gen'

const userPath = (name: string) => `/users/${encodeURIComponent(name)}`
const packagePath = (name: string) => `/packages/${encodeURIComponent(name)}`

/** Users keys start with 'users' and package keys with 'packages'. */
export const usersKeys = {
  users: ['users'] as const,
  list: ['users', 'list'] as const,
  packages: ['packages'] as const,
  packageList: ['packages', 'list'] as const,
  packageOptions: ['packages', 'options'] as const,
  pkg: (name: string) => ['packages', 'one', name] as const,
}

/** The actions POST /users/bulk accepts. All but update-counters take one name per request. */
export const USER_BULK_ACTIONS = [
  'rebuild',
  'rebuild-user',
  'rebuild-web',
  'rebuild-dns',
  'rebuild-mail',
  'rebuild-db',
  'rebuild-cron',
  'update-counters',
  'suspend',
  'unsuspend',
  'delete',
] as const

export type UserBulkAction = (typeof USER_BULK_ACTIONS)[number]

/** GET /users (admins): every account, ROOT_USER and the signed-in user. */
export function useUsers({ enabled = true } = {}) {
  return useQuery({ queryKey: usersKeys.list, queryFn: () => api.get<UserList>('/users'), enabled })
}

/** GET /packages (admins). */
export function usePackages({ enabled = true } = {}) {
  return useQuery({ queryKey: usersKeys.packageList, queryFn: () => api.get<PackageList>('/packages'), enabled })
}

/** GET /packages/{package}; idle while name is empty (the add page). */
export function usePackage(name: string) {
  return useQuery({
    queryKey: usersKeys.pkg(name),
    queryFn: () => api.get<Package>(packagePath(name)),
    enabled: name !== '',
  })
}

/** GET /packages/options: templates, shells and which sections the server has. */
export function usePackageOptions() {
  return useQuery({
    queryKey: usersKeys.packageOptions,
    queryFn: () => api.get<PackageOptions>('/packages/options'),
    staleTime: 60_000,
  })
}

/** User writes (admins). Each call refetches ['users'] when it settles. */
export function useUserMutation() {
  const client = useQueryClient()
  const run = useMutation({
    mutationFn: (call: () => Promise<unknown>) => call(),
    onSettled: () => client.invalidateQueries({ queryKey: usersKeys.users }),
  })
  const go = <T>(call: () => Promise<T>) => run.mutateAsync(call) as Promise<T>
  return {
    suspend: (name: string) => go(() => api.post<OK>(`${userPath(name)}/suspend`)),
    unsuspend: (name: string) => go(() => api.post<OK>(`${userPath(name)}/unsuspend`)),
    remove: (name: string) => go(() => api.del<OK>(userPath(name))),
    bulk: (body: UserBulkRequest) => go(() => api.post<BulkReply>('/users/bulk', body)),
  }
}

/** Package writes (admins). Each call refetches ['packages'] and ['users'] when it settles. */
export function usePackageMutation() {
  const client = useQueryClient()
  const run = useMutation({
    mutationFn: (call: () => Promise<unknown>) => call(),
    onSettled: () =>
      Promise.all([
        client.invalidateQueries({ queryKey: usersKeys.packages }),
        client.invalidateQueries({ queryKey: usersKeys.users }),
      ]),
  })
  const go = <T>(call: () => Promise<T>) => run.mutateAsync(call) as Promise<T>
  return {
    add: (body: PackageRequest) => go(() => api.post<PackageSaved>('/packages', body)),
    /** The reply's name is the package's name after the save (it may be renamed). */
    edit: (name: string, body: PackageRequest) => go(() => api.put<PackageSaved>(packagePath(name), body)),
    copy: (name: string) => go(() => api.post<OK>(`${packagePath(name)}/copy`)),
    remove: (name: string) => go(() => api.del<OK>(packagePath(name))),
    bulk: (body: UserBulkRequest) => go(() => api.post<BulkReply>('/packages/bulk', body)),
  }
}
