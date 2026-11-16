import { useQuery } from '@tanstack/react-query'
import { api } from './client'
import type { JobStatus } from './types.gen'

/**
 * Polls a background job's status route every intervalMs while it is running; stops
 * once it is done or failed. No request is made without a path.
 */
export function useJobStatus(path: string | undefined, intervalMs: number) {
  return useQuery({
    queryKey: ['job', path],
    queryFn: () => api.get<JobStatus>(path!),
    enabled: !!path,
    refetchInterval: (q) => (q.state.status !== 'error' && q.state.data?.state === 'running' ? intervalMs : false),
    retry: false,
  })
}
