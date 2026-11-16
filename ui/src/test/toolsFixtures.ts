// Fake tools API for screen tests (Plan 5): builders in the generated wire types, and
// toolsRoutes() serving every tools route with the Go dev fixtures' data: the searches
// for "example" (5 rows in the core's order; bob's rows need a login-as from admin),
// "www" (bob's web alias and an unverified record row) and "info" (an account row),
// a terminal token, the file-manager handoff, and a per-file restore of snapshot
// 3c9b8a7e whose job (RESTORE_JOB_ID, GET /backup/restore-jobs/{id}) is done.
import type { JobStatus, SearchResults, SearchRow, TerminalToken } from '../api/types.gen'
import { signedIn, type Route } from './renderApp'

/** The job id every toolsRoutes() restore answers. */
export const RESTORE_JOB_ID = 'fedcba9876543210fedcba9876543210'

/** The snapshot toolsRoutes() restores from (the dev fixture's short id). */
export const RESTORE_SNAPSHOT = '3c9b8a7e'

/** One search row; a linked, active row of admin's own unless overridden. */
export function searchHit(over: Partial<SearchRow> = {}): SearchRow {
  return {
    id: '1',
    user: 'admin',
    type: 'web',
    key: 'DOMAIN',
    result: 'example.net',
    alias: '',
    link: '',
    parent: '',
    suspended: false,
    date: '2026-04-01',
    time: '11:02:55',
    route: '/web/example.net',
    linked: true,
    loginAs: false,
    ...over,
  }
}

const bob = (o: Partial<SearchRow>) => searchHit({ user: 'bob', loginAs: true, ...o })

/** GET /search?q=example as admin sees it (the dev v-search-object fixture). */
export function searchResults(over: Partial<SearchResults> = {}): SearchResults {
  const day = { date: '2026-10-19', time: '14:20:03' }
  return {
    query: 'example',
    user: '',
    rows: [
      searchHit({ suspended: true }),
      bob({ id: '2', result: 'bakery.example', alias: 'www.bakery.example', route: '/web/bakery.example', ...day }),
      bob({ id: '3', type: 'dns', result: 'bakery.example', route: '/dns/bakery.example/edit', ...day }),
      bob({ id: '4', type: 'mail', result: 'bakery.example', route: '/mail/bakery.example/edit', ...day }),
      bob({
        id: '5',
        type: 'cron',
        key: 'JOB',
        result: 'wget -q -O /dev/null https://bakery.example/',
        link: '2',
        parent: '2',
        route: '/cron/2',
        date: '2026-02-03',
        time: '14:00:00',
      }),
    ],
    ...over,
  }
}

/**
 * The record row of the "www" search. As the dev fixture has it, the core took the
 * zone from the line parsed before (SR33), so the server couldn't verify it: linked
 * false. Override result/parent/linked for a verified row; route follows parent.
 */
export function searchRecordRow(over: Partial<SearchRow> = {}): SearchRow {
  const parent = over.parent ?? 'bakery.example'
  return bob({
    id: '2',
    type: 'dns',
    key: 'RECORD',
    result: 'www.bakery.example',
    link: '3',
    parent,
    route: `/dns/${parent}/records/3`,
    linked: false,
    date: '2026-01-10',
    time: '12:00:01',
    ...over,
  })
}

/** The account row of the "info" search. */
export function searchAccountRow(over: Partial<SearchRow> = {}): SearchRow {
  return bob({
    id: '1',
    type: 'mail',
    key: 'ACCOUNT',
    result: 'info@bakery.example',
    link: 'info',
    parent: 'bakery.example',
    route: '/mail/bakery.example/accounts/info',
    date: '2026-01-11',
    time: '12:06:00',
    ...over,
  })
}

/** POST /terminal/token's reply. */
export function terminalToken(over: Partial<TerminalToken> = {}): TerminalToken {
  return { token: '0123456789abcdef0123456789abcdef', expires: '2026-10-28T10:00:30Z', ...over }
}

function restoreJob(over: Partial<JobStatus>): JobStatus {
  return {
    id: RESTORE_JOB_ID,
    title: 'Restore files',
    state: 'running',
    step: '',
    steps: [],
    error: null,
    result: null,
    started: '2026-10-28T10:00:00Z',
    finished: '',
    ...over,
  }
}

/** A finished restore: every path restored, in order (each one a step). */
export function restoreJobDone(restored: string[] = ['/home/admin/Documents/notes.txt']): JobStatus {
  return restoreJob({
    state: 'done',
    steps: restored,
    step: restored[restored.length - 1] ?? '',
    result: { restored },
    finished: '2026-10-28T10:00:05Z',
  })
}

/**
 * A failed restore: the paths in restored went back, then path failed with message.
 * The error's args[0] is the failing path; the steps list every path tried.
 */
export function restoreJobFailed(
  path: string,
  restored: string[] = [],
  message = 'Error: Unable to download snapshot data',
): JobStatus {
  return restoreJob({
    state: 'failed',
    steps: [...restored, path],
    step: path,
    error: { code: 'command_failed', message, args: [path] },
    finished: '2026-10-28T10:00:05Z',
  })
}

/**
 * Every tools route with the dev data and the bootstrap of a root admin with every
 * feature. overrides replace or add routes by key ("METHOD /path").
 */
export function toolsRoutes(overrides: Record<string, Route> = {}): Record<string, Route> {
  return {
    'GET /bootstrap': () => ({ body: signedIn({}) }),
    'GET /search?q=example': () => ({ body: searchResults() }),
    'GET /search?q=www': () => ({
      body: searchResults({ query: 'www', rows: [{ ...searchResults().rows[1], id: '1' }, searchRecordRow()] }),
    }),
    'GET /search?q=info': () => ({ body: searchResults({ query: 'info', rows: [searchAccountRow()] }) }),
    'POST /auth/login-as': () => ({ body: { status: 'ok', landing: '/web' } }),
    'POST /terminal/token': () => ({ body: terminalToken() }),
    'POST /files/handoff': () => ({ body: { url: '/fm/' } }),
    [`POST /backup/incremental/${RESTORE_SNAPSHOT}/restore-files`]: () => ({
      status: 202,
      body: { jobId: RESTORE_JOB_ID },
    }),
    [`GET /backup/restore-jobs/${RESTORE_JOB_ID}`]: () => ({ body: restoreJobDone() }),
    ...overrides,
  }
}
