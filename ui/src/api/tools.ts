import { useMutation, useQuery } from '@tanstack/react-query'
import { api } from './client'
import { useJobStatus } from './jobs'
import type { FilesHandoff, JobRef, RestoreFilesRequest, SearchResults, TerminalToken } from './types.gen'

// Tools (Plan 5) data layer: search, the web terminal token, the file-manager handoff
// and per-file snapshot restore. Mutations resolve to their reply and reject with an
// ApiError.

const enc = encodeURIComponent

export const toolsKeys = {
  search: (q: string, u: string) => ['search', q, u] as const,
}

/** GET /search?q=&u=; idle while q is empty (the UI never searches an empty box). */
export function useSearch(q: string, u = '') {
  return useQuery({
    queryKey: toolsKeys.search(q, u),
    queryFn: () => api.get<SearchResults>(`/search?q=${enc(q)}${u ? `&u=${enc(u)}` : ''}`),
    enabled: q !== '',
  })
}

/** POST /terminal/token: a single-use token for the terminal websocket's auth message. */
export function useTerminalToken() {
  return useMutation({ mutationFn: () => api.post<TerminalToken>('/terminal/token') })
}

/** POST /files/handoff: prepares the file manager's session; resolves to where to go. */
export function useFilesHandoff() {
  return useMutation({ mutationFn: () => api.post<FilesHandoff>('/files/handoff') })
}

/** POST /backup/incremental/{snapshot}/restore-files: starts the restore job. */
export function useRestoreFiles(snapshot: string) {
  return useMutation({
    mutationFn: (body: RestoreFilesRequest) =>
      api.post<JobRef>(`/backup/incremental/${enc(snapshot)}/restore-files`, body),
  })
}

/**
 * Polls a per-file restore job (GET /backup/restore-jobs/{id}, ruling R1) every second
 * until it ends; idle without an id.
 */
export function useRestoreJob(id: string) {
  return useJobStatus(id ? `/backup/restore-jobs/${enc(id)}` : undefined, 1000)
}
