import { Badge, Button, DropdownMenu, Empty, Link, LinkButton, Table, useKumoToastManager } from '@cloudflare/kumo'
import {
  AtIcon,
  BookBookmarkIcon,
  ClockIcon,
  DatabaseIcon,
  EnvelopeSimpleIcon,
  GlobeHemisphereWestIcon,
  MagnifyingGlassIcon,
  SortAscendingIcon,
  type Icon,
} from '@phosphor-icons/react'
import { useMemo } from 'react'
import { useNavigate, useSearchParams } from 'react-router'
import { api } from '../../api/client'
import { apiErrorText } from '../../api/errors'
import { useSearch } from '../../api/tools'
import type { SearchRow, StatusReply } from '../../api/types.gen'
import { PageHeader } from '../../components/PageHeader'
import { LoadingState, RequestError } from '../../components/RequestState'
import { TableCard, useDataTable } from '../../components/table'
import { useT } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { useSession, useSessionUser } from '../../session/SessionProvider'

/** The core's TYPE and KEY of a hit, as the label and icon Hestia shows for it. */
function kindOf(row: SearchRow): { label: string; icon: Icon } {
  const k = `${row.type} ${row.key}`
  switch (k) {
    case 'web DOMAIN':
      return { label: msgid('Web Domain'), icon: GlobeHemisphereWestIcon }
    case 'dns DOMAIN':
      return { label: msgid('DNS Domain'), icon: BookBookmarkIcon }
    case 'dns RECORD':
      return { label: msgid('Record'), icon: BookBookmarkIcon }
    case 'mail DOMAIN':
      return { label: msgid('Mail Domain'), icon: EnvelopeSimpleIcon }
    case 'mail ACCOUNT':
      return { label: msgid('Account'), icon: AtIcon }
    case 'db DATABASE':
      return { label: msgid('Database'), icon: DatabaseIcon }
    case 'cron JOB':
      return { label: msgid('Cron Jobs'), icon: ClockIcon }
    default:
      return { label: row.type, icon: MagnifyingGlassIcon }
  }
}

const sortKeys = ['date', 'name'] as const

/** Search results (/search?q=&u=): every object whose name matches, across the account's types. */
export function SearchPage() {
  const t = useT()
  const toast = useKumoToastManager()
  const navigate = useNavigate()
  const { refresh } = useSession()
  const user = useSessionUser()
  const isAdmin = user?.role === 'admin' && user.look === ''
  const [params] = useSearchParams()
  const q = (params.get('q') ?? '').trim()
  const u = isAdmin ? (params.get('u') ?? '') : ''
  const search = useSearch(q, u)
  const rows = useMemo(() => search.data?.rows ?? [], [search.data])

  const table = useDataTable({
    rows,
    getId: (r) => r.id,
    sorts: {
      date: (r) => `${r.date} ${r.time}`,
      name: (r) => r.result,
    },
    initialSort: { key: 'date', dir: 'desc' },
  })
  const sortLabels = { date: t('Date'), name: t('Name') }
  const sortKey = (table.sort?.key ?? 'date') as (typeof sortKeys)[number]

  const open = async (row: SearchRow) => {
    try {
      if (row.loginAs) {
        await api.post<StatusReply>('/auth/login-as', { user: row.user })
        await refresh()
      }
      navigate(row.route)
    } catch (err) {
      toast.add({ title: t("Couldn't open this object"), description: apiErrorText(err, t), type: 'error' })
    }
  }

  const count = table.totalCount
  return (
    <div className="grid gap-6">
      <PageHeader
        title={t('Search results')}
        description={q ? `${t('Search')}: ${q}` : undefined}
        actions={
          u ? (
            <LinkButton variant="secondary" href={`/search?q=${encodeURIComponent(q)}`}>
              {t('Show all owners')}
            </LinkButton>
          ) : undefined
        }
      />
      {!q ? (
        <Empty
          icon={<MagnifyingGlassIcon size={40} className="text-kumo-inactive" />}
          title={t('Type something to search for')}
          description={t('Search for a domain, a database, a mail account, a DNS record or a cron job by name.')}
        />
      ) : search.isPending ? (
        <LoadingState />
      ) : search.isError ? (
        <RequestError error={search.error} />
      ) : (
        <div className="grid gap-3">
          <div className="flex justify-end">
            <DropdownMenu>
              <DropdownMenu.Trigger
                render={
                  <Button variant="secondary" icon={SortAscendingIcon}>
                    {t('Sort by')}: {sortLabels[sortKey]}
                  </Button>
                }
              />
              <DropdownMenu.Content align="end">
                {sortKeys.map((key) => (
                  <DropdownMenu.Item
                    key={key}
                    onClick={() => table.setSort({ key, dir: key === 'date' ? 'desc' : 'asc' })}
                  >
                    {sortLabels[key]}
                  </DropdownMenu.Item>
                ))}
              </DropdownMenu.Content>
            </DropdownMenu>
          </div>
          <TableCard
            filteredCount={count}
            totalCount={count}
            noun={count === 1 ? t('object') : t('objects')}
            emptyState={{
              icon: <MagnifyingGlassIcon size={40} className="text-kumo-inactive" />,
              title: t('No objects found'),
              description: t('Nothing is named like that. Check the spelling or try a shorter search.'),
            }}
          >
            <Table>
              <Table.Header>
                <Table.Row>
                  <Table.Head className="w-px" />
                  <Table.Head>{t('Status')}</Table.Head>
                  <Table.Head>{t('Search Results')}</Table.Head>
                  <Table.Head>{t('Date')}</Table.Head>
                  <Table.Head>{t('Owner')}</Table.Head>
                  <Table.Head>{t('Type')}</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {table.rows.map((r) => {
                  const kind = kindOf(r)
                  return (
                    <Table.Row key={r.id} variant={r.suspended ? 'selected' : 'default'}>
                      <Table.Cell className="w-px">
                        <kind.icon size={18} data-type-icon={kind.label} className="text-kumo-subtle" aria-hidden />
                      </Table.Cell>
                      <Table.Cell>
                        {r.suspended ? (
                          <Badge variant="warning" appearance="dot">
                            {t('Suspended')}
                          </Badge>
                        ) : (
                          <Badge variant="success" appearance="dot">
                            {t('Active')}
                          </Badge>
                        )}
                      </Table.Cell>
                      <Table.Cell className="min-w-48">
                        <div className="flex flex-col gap-0.5">
                          {r.linked && r.route && r.loginAs ? (
                            // Opening another owner's object logs in as them first. A button, not
                            // a link: Ctrl/Cmd/middle-click can't open the route without the login-as.
                            <button
                              type="button"
                              onClick={() => void open(r)}
                              className="w-fit cursor-pointer text-left text-kumo-link hover:underline"
                            >
                              {r.result}
                            </button>
                          ) : r.linked && r.route ? (
                            <Link href={r.route}>{r.result}</Link>
                          ) : (
                            <span>{r.result}</span>
                          )}
                          {r.alias && <span className="text-kumo-subtle">{r.alias}</span>}
                        </div>
                      </Table.Cell>
                      <Table.Cell className="whitespace-nowrap tabular-nums">
                        <div className="flex flex-col gap-0.5">
                          <span>{r.date}</span>
                          <span className="text-kumo-subtle">{r.time}</span>
                        </div>
                      </Table.Cell>
                      <Table.Cell>
                        {isAdmin ? (
                          <Link href={`/search?q=${encodeURIComponent(q)}&u=${encodeURIComponent(r.user)}`}>{r.user}</Link>
                        ) : (
                          r.user
                        )}
                      </Table.Cell>
                      <Table.Cell className="whitespace-nowrap">{t(kind.label)}</Table.Cell>
                    </Table.Row>
                  )
                })}
              </Table.Body>
            </Table>
          </TableCard>
        </div>
      )}
    </div>
  )
}
