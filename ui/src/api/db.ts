import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './client'
import type { Database } from './hestia.gen'
import type {
  AddDBReply,
  AddDBRequest,
  BulkReply,
  BulkRequest,
  DBOptions,
  Databases,
  EditDBRequest,
  OK,
} from './types.gen'

const dbPath = (name: string) => `/db/${encodeURIComponent(name)}`

export const dbKeys = {
  all: ['db'] as const,
  one: (name: string) => ['db', name] as const,
  options: ['db', 'options'] as const,
}

/** Plain links: the browser receives the file / follows the redirect itself. */
export const downloadUrl = (name: string) => `/api/v1${dbPath(name)}/download`
export const phpMyAdminSsoUrl = (name: string) => `/api/v1${dbPath(name)}/phpmyadmin`

export function useDatabases({ enabled = true } = {}) {
  return useQuery({
    queryKey: dbKeys.all,
    queryFn: () => api.get<Databases>('/db'),
    enabled,
  })
}

/** One database; idle while name is empty (the add page has none). */
export function useDatabase(name: string) {
  return useQuery({
    queryKey: dbKeys.one(name),
    queryFn: () => api.get<Database>(dbPath(name)),
    enabled: name !== '',
  })
}

export function useDbOptions() {
  return useQuery({ queryKey: dbKeys.options, queryFn: () => api.get<DBOptions>('/db/options'), staleTime: 60_000 })
}

/**
 * Every write to databases. Each call refetches the database queries when it settles,
 * and rejects with an ApiError whose message is Hestia's text.
 */
export function useDbMutation() {
  const client = useQueryClient()
  const run = useMutation({
    mutationFn: (call: () => Promise<unknown>) => call(),
    onSettled: () => client.invalidateQueries({ queryKey: dbKeys.all }),
  })
  const go = <T>(call: () => Promise<T>) => run.mutateAsync(call) as Promise<T>
  return {
    addDb: (body: AddDBRequest) => go(() => api.post<AddDBReply>('/db', body)),
    editDb: (name: string, body: EditDBRequest) => go(() => api.put<OK>(dbPath(name), body)),
    deleteDb: (name: string) => go(() => api.del<OK>(dbPath(name))),
    suspendDb: (name: string) => go(() => api.post<OK>(`${dbPath(name)}/suspend`)),
    unsuspendDb: (name: string) => go(() => api.post<OK>(`${dbPath(name)}/unsuspend`)),
    bulkDb: (body: BulkRequest) => go(() => api.post<BulkReply>('/db/bulk', body)),
  }
}
