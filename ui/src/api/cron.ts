import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './client'
import type { CronJob } from './hestia.gen'
import type { BulkReply, BulkRequest, CronJobs, CronOptions, CronRequest, OK } from './types.gen'

const jobPath = (id: string) => `/cron/${encodeURIComponent(id)}`

/** Every cron key starts with 'cron', so invalidating ['cron'] refetches them all. */
export const cronKeys = {
  all: ['cron'] as const,
  list: ['cron', 'list'] as const,
  options: ['cron', 'options'] as const,
  job: (id: string) => ['cron', 'job', id] as const,
}

/** GET /cron: the viewed account's jobs, the reports flag and usage. */
export function useCronJobs({ enabled = true } = {}) {
  return useQuery({ queryKey: cronKeys.list, queryFn: () => api.get<CronJobs>('/cron'), enabled })
}

/** GET /cron/{job}; idle while id is empty (the add page). */
export function useCronJob(id: string) {
  return useQuery({ queryKey: cronKeys.job(id), queryFn: () => api.get<CronJob>(jobPath(id)), enabled: id !== '' })
}

/** GET /cron/options: the server's time zone and time. */
export function useCronOptions() {
  return useQuery({ queryKey: cronKeys.options, queryFn: () => api.get<CronOptions>('/cron/options') })
}

/** Every cron write. Each call refetches ['cron'] when it settles. */
export function useCronMutation() {
  const client = useQueryClient()
  const run = useMutation({
    mutationFn: (call: () => Promise<unknown>) => call(),
    onSettled: () => client.invalidateQueries({ queryKey: cronKeys.all }),
  })
  const go = <T>(call: () => Promise<T>) => run.mutateAsync(call) as Promise<T>
  return {
    add: (body: CronRequest) => go(() => api.post<OK>('/cron', body)),
    edit: (id: string, body: CronRequest) => go(() => api.put<OK>(jobPath(id), body)),
    remove: (id: string) => go(() => api.del<OK>(jobPath(id))),
    suspend: (id: string) => go(() => api.post<OK>(`${jobPath(id)}/suspend`)),
    unsuspend: (id: string) => go(() => api.post<OK>(`${jobPath(id)}/unsuspend`)),
    bulk: (body: BulkRequest) => go(() => api.post<BulkReply>('/cron/bulk', body)),
    setReports: (enabled: boolean) => go(() => api.post<OK>('/cron/reports', { enabled })),
  }
}
