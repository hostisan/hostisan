import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './client'
import type { AccessKey } from './hestia.gen'
import type {
  AccessKeyCreated,
  AccessKeys,
  AccountOptions,
  AccountRequest,
  AccountSaved,
  AccountView,
  AddAccessKeyRequest,
  AddSSHKeyRequest,
  AddUserRequest,
  AuthLog,
  BulkReply,
  BulkRequest,
  Logs,
  Notifications,
  OK,
  PasswordConfirmRequest,
  ResetConfirmRequest,
  ResetRequest,
  SSHKeys,
  Stats,
  TwoFAConfirmRequest,
  TwoFASetup,
  UnlockConfirmRequest,
  UnlockRequest,
  UserEditRequest,
} from './types.gen'

const enc = encodeURIComponent
const userPath = (name: string) => `/users/${enc(name)}`
/** "?user=<name>" when user is set: admins viewing another account's logs or stats. */
const userQuery = (user?: string) => (user ? `?user=${enc(user)}` : '')

/**
 * Query keys: the profile and keys under 'account', another user's page under 'users'
 * (so user writes refresh it), and notifications, logs and stats each on their own.
 */
export const accountKeys = {
  account: ['account'] as const,
  options: ['account', 'options'] as const,
  sshKeys: ['account', 'ssh-keys'] as const,
  accessKeys: ['account', 'access-keys'] as const,
  accessKey: (id: string) => ['account', 'access-keys', id] as const,
  user: (name: string) => ['users', 'detail', name] as const,
  notifications: ['notifications'] as const,
  logs: (user: string) => ['logs', 'user', user] as const,
  authLog: (user: string) => ['logs', 'auth', user] as const,
  stats: (user: string) => ['stats', user] as const,
}

/** Like useMutation over a list of calls: each settles by invalidating keys. */
function useWrites(keys: readonly (readonly unknown[])[]) {
  const client = useQueryClient()
  const run = useMutation({
    mutationFn: (call: () => Promise<unknown>) => call(),
    onSettled: () => Promise.all(keys.map((queryKey) => client.invalidateQueries({ queryKey }))),
  })
  return <T>(call: () => Promise<T>) => run.mutateAsync(call) as Promise<T>
}

// ---------------------------------------------------------------- profile

/** GET /account: the effective user's profile. */
export function useAccount() {
  return useQuery({ queryKey: accountKeys.account, queryFn: () => api.get<AccountView>('/account') })
}

/** GET /account/options: languages, themes and (admins) packages, shells and PHP versions. */
export function useAccountOptions() {
  return useQuery({
    queryKey: accountKeys.options,
    queryFn: () => api.get<AccountOptions>('/account/options'),
    staleTime: 60_000,
  })
}

/** GET /users/{user} (admins); idle while name is empty. */
export function useUser(name: string) {
  return useQuery({
    queryKey: accountKeys.user(name),
    queryFn: () => api.get<AccountView>(userPath(name)),
    enabled: name !== '',
  })
}

/**
 * Profile and user writes. Each call refetches ['account'], ['users'] and
 * ['bootstrap'] (the session's name, language and theme) when it settles.
 */
export function useAccountMutation() {
  const go = useWrites([accountKeys.account, ['users'], ['bootstrap']])
  return {
    save: (body: AccountRequest) => go(() => api.put<AccountSaved>('/account', body)),
    addUser: (body: AddUserRequest) => go(() => api.post<OK>('/users', body)),
    editUser: (name: string, body: UserEditRequest) => go(() => api.put<AccountSaved>(userPath(name), body)),
    /** Starts enrolment: nothing is saved until twofaConfirm. */
    twofaSetup: () => go(() => api.post<TwoFASetup>('/account/2fa/setup')),
    twofaConfirm: (body: TwoFAConfirmRequest) => go(() => api.post<OK>('/account/2fa/confirm', body)),
    /** Turns your own 2FA off; needs the current password. */
    twofaDisable: (body: PasswordConfirmRequest) => go(() => api.del<OK>('/account/2fa', body)),
    /** An admin turns another user's 2FA off; no password. */
    userTwofaDisable: (name: string) => go(() => api.del<OK>(`${userPath(name)}/2fa`)),
  }
}

// ---------------------------------------------------------------- recovery

/**
 * The anonymous recovery steps (the CSRF token comes from the anonymous bootstrap).
 * The request steps always answer {status: "sent"}, whatever happened.
 */
export function useRecovery() {
  const go = useWrites([])
  return {
    requestReset: (body: ResetRequest) => go(() => api.post<OK>('/auth/reset/request', body)),
    confirmReset: (body: ResetConfirmRequest) => go(() => api.post<OK>('/auth/reset/confirm', body)),
    requestUnlock: (body: UnlockRequest) => go(() => api.post<OK>('/auth/reset-2fa/request', body)),
    confirmUnlock: (body: UnlockConfirmRequest) => go(() => api.post<OK>('/auth/reset-2fa/confirm', body)),
  }
}

// ---------------------------------------------------------------- keys

/** GET /account/ssh-keys. */
export function useSshKeys() {
  return useQuery({ queryKey: accountKeys.sshKeys, queryFn: () => api.get<SSHKeys>('/account/ssh-keys') })
}

/** GET /account/access-keys: the keys and the APIs the user may grant. */
export function useAccessKeys() {
  return useQuery({ queryKey: accountKeys.accessKeys, queryFn: () => api.get<AccessKeys>('/account/access-keys') })
}

/** GET /account/access-keys/{key}; idle while id is empty. Never carries the secret. */
export function useAccessKey(id: string) {
  return useQuery({
    queryKey: accountKeys.accessKey(id),
    queryFn: () => api.get<AccessKey>(`/account/access-keys/${enc(id)}`),
    enabled: id !== '',
  })
}

/** SSH and access key writes. Each call refetches ['account'] when it settles. */
export function useKeyMutation() {
  const go = useWrites([accountKeys.account])
  return {
    addSshKey: (body: AddSSHKeyRequest) => go(() => api.post<OK>('/account/ssh-keys', body)),
    removeSshKey: (id: string) => go(() => api.del<OK>(`/account/ssh-keys/${enc(id)}`)),
    /**
     * The reply carries the secret, once. Unused by the pages: AccessKeyAddPage calls
     * the API directly so the secret stays out of the mutation cache. Kept so the
     * contract test covers every access key route.
     */
    addAccessKey: (body: AddAccessKeyRequest) => go(() => api.post<AccessKeyCreated>('/account/access-keys', body)),
    removeAccessKey: (id: string) => go(() => api.del<OK>(`/account/access-keys/${enc(id)}`)),
    bulkAccessKeys: (body: BulkRequest) => go(() => api.post<BulkReply>('/account/access-keys/bulk', body)),
  }
}

// ---------------------------------------------------------------- notifications

/** GET /notifications: by priority, then newest first. */
export function useNotifications({ enabled = true } = {}) {
  return useQuery({
    queryKey: accountKeys.notifications,
    queryFn: () => api.get<Notifications>('/notifications'),
    enabled,
  })
}

/** Notification writes. Each call refetches ['notifications'] when it settles. */
export function useNotificationMutation() {
  const go = useWrites([accountKeys.notifications])
  return {
    remove: (id: string) => go(() => api.del<OK>(`/notifications/${enc(id)}`)),
    removeAll: () => go(() => api.del<OK>('/notifications')),
    ack: (id: string) => go(() => api.post<OK>(`/notifications/${enc(id)}/ack`)),
    ackAll: () => go(() => api.post<OK>('/notifications/ack')),
  }
}

// ---------------------------------------------------------------- logs and stats

/** GET /logs[?user=]: the user log, newest first; admins may pass a user or "system". */
export function useLogs(user?: string) {
  return useQuery({ queryKey: accountKeys.logs(user ?? ''), queryFn: () => api.get<Logs>(`/logs${userQuery(user)}`) })
}

/** GET /logs/auth[?user=]: the login history, newest first. */
export function useAuthLog(user?: string) {
  return useQuery({
    queryKey: accountKeys.authLog(user ?? ''),
    queryFn: () => api.get<AuthLog>(`/logs/auth${userQuery(user)}`),
  })
}

/** Log deletes. Each call refetches ['logs'] when it settles. */
export function useLogMutation() {
  const go = useWrites([['logs']])
  return {
    clear: (user?: string) => go(() => api.del<OK>(`/logs${userQuery(user)}`)),
    clearAuth: (user?: string) => go(() => api.del<OK>(`/logs/auth${userQuery(user)}`)),
  }
}

/**
 * GET /stats[?user=]: monthly usage, newest first. Admins with no user get the
 * overall totals; with a user, that user's months.
 */
export function useStats(user?: string) {
  return useQuery({
    queryKey: accountKeys.stats(user ?? ''),
    queryFn: () => api.get<Stats>(`/stats${userQuery(user)}`),
  })
}
