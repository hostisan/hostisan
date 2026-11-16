import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './client'
import type {
  BackupView,
  Backups,
  BulkReply,
  BulkRequest,
  DownloadReply,
  ExclusionsRequest,
  ExclusionsView,
  OK,
  QueuedReply,
  RestoreRequest,
  SnapshotFiles,
  SnapshotView,
  Snapshots,
} from './types.gen'

const enc = encodeURIComponent
const backupPath = (name: string) => `/backup/${enc(name)}`
const snapshotPath = (id: string) => `/backup/incremental/${enc(id)}`

/** Every backup key starts with 'backup', so invalidating ['backup'] refetches them all. */
export const backupKeys = {
  all: ['backup'] as const,
  list: ['backup', 'list'] as const,
  one: (name: string) => ['backup', 'one', name] as const,
  exclusions: ['backup', 'exclusions'] as const,
  snapshots: ['backup', 'snapshots'] as const,
  snapshot: (id: string) => ['backup', 'snapshots', id] as const,
  files: (id: string, path: string) => ['backup', 'snapshots', id, 'files', path] as const,
}

/**
 * The GET download path of a local backup, for a plain link once prepareDownload says
 * "ready". It is /backup/file/<name>, never /backup/<name>/file (which would clash with
 * /backup/incremental/<id> in the server's router).
 */
export const fileUrl = (name: string) => `/api/v1/backup/file/${enc(name)}`

/** GET /backup: the viewed account's backups, usage and whether snapshots are on. */
export function useBackups({ enabled = true } = {}) {
  return useQuery({ queryKey: backupKeys.list, queryFn: () => api.get<Backups>('/backup'), enabled })
}

/** GET /backup/{backup}; idle while name is empty. */
export function useBackup(name: string) {
  return useQuery({
    queryKey: backupKeys.one(name),
    queryFn: () => api.get<BackupView>(backupPath(name)),
    enabled: name !== '',
  })
}

/** GET /backup/exclusions. */
export function useBackupExclusions() {
  return useQuery({ queryKey: backupKeys.exclusions, queryFn: () => api.get<ExclusionsView>('/backup/exclusions') })
}

/** GET /backup/incremental: restic snapshots, newest first. */
export function useSnapshots({ enabled = true } = {}) {
  return useQuery({ queryKey: backupKeys.snapshots, queryFn: () => api.get<Snapshots>('/backup/incremental'), enabled })
}

/** GET /backup/incremental/{snapshot}; idle while id is empty. */
export function useSnapshot(id: string) {
  return useQuery({
    queryKey: backupKeys.snapshot(id),
    queryFn: () => api.get<SnapshotView>(snapshotPath(id)),
    enabled: id !== '',
  })
}

/** GET /backup/incremental/{snapshot}/files?path=; idle while id or path is empty. */
export function useSnapshotFiles(id: string, path: string) {
  return useQuery({
    queryKey: backupKeys.files(id, path),
    queryFn: () => api.get<SnapshotFiles>(`${snapshotPath(id)}/files?path=${enc(path)}`),
    enabled: id !== '' && path !== '',
  })
}

/**
 * Every backup write. Each call refetches ['backup'] when it settles. Scheduled jobs
 * answer a QueuedReply whose message is the msgid to toast; completion arrives by email
 * and in the notification tray.
 */
export function useBackupMutation() {
  const client = useQueryClient()
  const run = useMutation({
    mutationFn: (call: () => Promise<unknown>) => call(),
    onSettled: () => client.invalidateQueries({ queryKey: backupKeys.all }),
  })
  const go = <T>(call: () => Promise<T>) => run.mutateAsync(call) as Promise<T>
  return {
    schedule: () => go(() => api.post<QueuedReply>('/backup')),
    /** "ready": open fileUrl(name); "scheduled": toast the message. */
    prepareDownload: (name: string) => go(() => api.post<DownloadReply>(`${backupPath(name)}/download`)),
    remove: (name: string) => go(() => api.del<OK>(backupPath(name))),
    bulk: (body: BulkRequest) => go(() => api.post<BulkReply>('/backup/bulk', body)),
    restore: (name: string, body: RestoreRequest) =>
      go(() => api.post<QueuedReply>(`${backupPath(name)}/restore`, body)),
    saveExclusions: (body: ExclusionsRequest) => go(() => api.put<OK>('/backup/exclusions', body)),
    scheduleSnapshot: () => go(() => api.post<QueuedReply>('/backup/incremental')),
    restoreSnapshot: (id: string, body: RestoreRequest) =>
      go(() => api.post<QueuedReply>(`${snapshotPath(id)}/restore`, body)),
    bulkSnapshots: (body: BulkRequest) => go(() => api.post<BulkReply>('/backup/incremental/bulk', body)),
  }
}
