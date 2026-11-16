import { ListConfirmDialog, ResourceDeleteDialog, useConfirm, useResourceDelete } from '../../components/useListConfirm'
import type { PendingConfirm } from '../../components/useListConfirm'
import {
  Badge,
  Banner,
  Button,
  DropdownMenu,
  LinkButton,
  Meter,
  Select,
  Table,
  Text,
  Toolbar,
  Tooltip,
} from '@cloudflare/kumo'
import {
  ArrowSquareOutIcon,
  PauseCircleIcon,
  PencilSimpleIcon,
  PlayCircleIcon,
  PlusIcon,
  SignpostIcon,
  TrashIcon,
  UserCircleIcon,
  WarningIcon,
} from '@phosphor-icons/react'
import { useMemo, useState } from 'react'
import { Link as RouterLink, useNavigate, useParams } from 'react-router'
import { PageHeader } from '../../components/PageHeader'
import { StatusBadge } from '../../components/StatusBadge'
import { SortableHead, TableCard, TableToolbar, rowLink, rowMenuTrigger, useDataTable } from '../../components/table'
import { formatMB, usagePercent } from '../../lib/format'
import type { MailAccount } from '../../api/hestia.gen'
import { useMailAccounts, useMailMutation, useMailOptions } from '../../api/mail'
import { useSessionUser } from '../../session/SessionProvider'
import { useT, type Translate } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { DnsError, DnsLoading } from '../dns/shared'
import { mailWebmailUrl } from './shared'
import { shortcutTarget } from '../../shortcuts/targets'
import { useRowShortcuts } from '../../shortcuts/useRowShortcuts'

const statusFilters = { all: msgid('Show all'), active: msgid('Active'), suspended: msgid('Suspended') }

function QuotaCell({ a }: { a: MailAccount }) {
  const t = useT()
  const pct = usagePercent(a.U_DISK, a.QUOTA)
  if (pct === null) {
    return (
      <Text as="span" variant="secondary">
        {t('%s of unlimited', formatMB(a.U_DISK))}
      </Text>
    )
  }
  return (
    <div className="w-36">
      <Meter
        label={t('%s of %s', formatMB(a.U_DISK), formatMB(a.QUOTA))}
        value={pct}
        customValue={`${pct}%`}
        indicatorClassName={pct >= 90 ? 'bg-kumo-danger' : pct >= 75 ? 'bg-kumo-warning' : undefined}
      />
    </div>
  )
}

function ListCell({ items, empty }: { items: string[]; empty: string }) {
  if (items.length === 0) return <span className="text-kumo-subtle">{empty}</span>
  const first = (
    <span className="inline-block max-w-40 truncate align-bottom" title={items[0]}>
      {items[0]}
    </span>
  )
  if (items.length === 1) return first
  return (
    <span className="inline-flex items-center gap-1.5">
      {first}
      <Tooltip
        content={items.slice(1).join(', ')}
        render={
          <span>
            <Badge variant="neutral">+{items.length - 1}</Badge>
          </span>
        }
      />
    </span>
  )
}

/** The suspend or unsuspend question for an account, shared by its menu and the s key. */
function suspendConfirm(
  a: MailAccount,
  domain: string,
  t: Translate,
  run: (name: string, suspended: boolean) => Promise<unknown>,
): PendingConfirm {
  const suspended = a.SUSPENDED === 'yes'
  const email = `${a.NAME}@${domain}`
  return {
    title: suspended ? t('Unsuspend mail account') : t('Suspend mail account'),
    description: suspended
      ? t('Are you sure you want to unsuspend %s?', email)
      : t('Are you sure you want to suspend %s?', email),
    confirmLabel: suspended ? t('Unsuspend') : t('Suspend'),
    destructive: !suspended,
    done: suspended ? t('%s unsuspended', email) : t('%s suspended', email),
    run: () => run(a.NAME, suspended),
  }
}

function RowActions({
  a,
  domain,
  domainSuspended,
  webmailHref,
  readOnly,
  isAdmin,
  ask,
  onDelete,
  run,
}: {
  a: MailAccount
  domain: string
  domainSuspended: boolean
  /** Empty when the account has no webmail to open. */
  webmailHref: string
  readOnly: boolean
  /** Suspend and unsuspend are admin-only on the server. */
  isAdmin: boolean
  ask: (p: PendingConfirm) => void
  onDelete: (name: string) => void
  run: (name: string, suspended: boolean) => Promise<unknown>
}) {
  const navigate = useNavigate()
  const t = useT()
  const suspended = a.SUSPENDED === 'yes'
  const email = `${a.NAME}@${domain}`
  return (
    <DropdownMenu>
      <DropdownMenu.Trigger render={rowMenuTrigger(t('Actions for %s', email))} />
      <DropdownMenu.Content align="end">
        {!readOnly && !suspended && !domainSuspended && (
          <DropdownMenu.Item icon={PencilSimpleIcon} onClick={() => navigate(`/mail/${domain}/accounts/${a.NAME}`)}>
            {t('Edit mail account')}
          </DropdownMenu.Item>
        )}
        {webmailHref !== '' && (
          <DropdownMenu.LinkItem icon={ArrowSquareOutIcon} href={webmailHref} target="_blank" rel="noopener noreferrer">
            {t('Open webmail')}
          </DropdownMenu.LinkItem>
        )}
        {!readOnly && (
          <>
            <DropdownMenu.Separator />
            {isAdmin && (
              <DropdownMenu.Item
                icon={suspended ? PlayCircleIcon : PauseCircleIcon}
                onClick={() => ask(suspendConfirm(a, domain, t, run))}
              >
                {suspended ? t('Unsuspend') : t('Suspend')}
              </DropdownMenu.Item>
            )}
            <DropdownMenu.Item icon={TrashIcon} variant="danger" onClick={() => onDelete(email)}>
              {t('Delete')}
            </DropdownMenu.Item>
          </>
        )}
      </DropdownMenu.Content>
    </DropdownMenu>
  )
}

/** Each bulk action's msgids: dialog title, button label and question. */
const bulkText = {
  suspend: { title: msgid('Suspend mail accounts'), label: msgid('Suspend'), ask: msgid('Are you sure you want to suspend %s?') },
  unsuspend: {
    title: msgid('Unsuspend mail accounts'),
    label: msgid('Unsuspend'),
    ask: msgid('Are you sure you want to unsuspend %s?'),
  },
  delete: { title: msgid('Delete mail accounts'), label: msgid('Delete'), ask: msgid('Are you sure you want to delete %s?') },
}

export function MailAccountsPage() {
  const t = useT()
  const { domain = '' } = useParams()
  const navigate = useNavigate()
  const user = useSessionUser()
  const readOnly = user?.readOnly ?? false
  // The signed-in role, even while looking at another user
  const isAdmin = user?.role === 'admin'
  const list = useMailAccounts(domain)
  const options = useMailOptions().data
  const m = useMailMutation()
  const d = list.data?.domain
  const domainSuspended = d?.SUSPENDED === 'yes'
  const [status, setStatus] = useState<keyof typeof statusFilters>('all')
  const confirm = useConfirm()
  const del = useResourceDelete()

  const all = useMemo<MailAccount[]>(() => list.data?.accounts ?? [], [list.data])
  const byStatus = useMemo(
    () => (status === 'all' ? all : all.filter((a) => (a.SUSPENDED === 'yes') === (status === 'suspended'))),
    [all, status],
  )
  const table = useDataTable({
    rows: byStatus,
    getId: (a) => a.NAME,
    search: (a, q) => a.NAME.includes(q) || a.ALIAS.includes(q) || a.FWD.includes(q),
    sorts: {
      name: (a) => a.NAME,
      disk: (a) => Number(a.U_DISK),
    },
    initialSort: { key: 'name', dir: 'asc' },
  })

  const suspendRun = (name: string, wasSuspended: boolean) =>
    wasSuspended ? m.unsuspendAccount(domain, name) : m.suspendAccount(domain, name)
  // Hestia's account list keys: Enter edit, s suspend, Delete.
  const keys = useRowShortcuts(table.rows, (a) => a.NAME, (a) => ({
    enter: a.SUSPENDED === 'yes' ? undefined : () => navigate(`/mail/${domain}/accounts/${a.NAME}`),
    ...(!readOnly && {
      s: isAdmin ? () => confirm.ask(suspendConfirm(a, domain, t, suspendRun)) : undefined,
      delete: () => del.ask(`${a.NAME}@${domain}`),
    }),
  }))

  if (list.isPending || list.isError || !d) {
    return (
      <div className="grid gap-6">
        <PageHeader title={t('Mail accounts')} back={{ href: '/mail', label: t('Mail') }} />
        {list.isError ? <DnsError error={list.error} /> : <DnsLoading />}
      </div>
    )
  }

  const bulk = (action: keyof typeof bulkText, destructive = false) => {
    const n = table.selectedCount
    const subject = n === 1 ? `${table.selectedRows[0].NAME}@${domain}` : t('%s mail accounts', n)
    const text = bulkText[action]
    confirm.ask({
      title: t(text.title),
      description: t(text.ask, subject),
      confirmLabel: t(text.label),
      destructive,
      done: t('%s applied to %s', t(text.label), subject),
      onDone: table.clearSelection,
      run: () => m.bulkAccounts(domain, { action, names: table.selectedRows.map((a) => a.NAME) }),
    })
  }

  return (
    <div className="grid gap-6">
      <PageHeader
        title={domain}
        description={
          <span className="inline-flex flex-wrap items-center gap-2">
            {t('Mail accounts')} · {t('%s used', formatMB(d.U_DISK))} <StatusBadge suspended={domainSuspended} />
          </span>
        }
        back={{ href: '/mail', label: t('Mail') }}
        actions={
          <>
            <LinkButton href={`/mail/${domain}/dns`} variant="secondary" icon={SignpostIcon}>
              {t('DNS records')}
            </LinkButton>
            {!readOnly && (
              <LinkButton href={`/mail/${domain}/edit`} variant="secondary" icon={PencilSimpleIcon}>
                {t('Edit mail domain')}
              </LinkButton>
            )}
            {!readOnly && !domainSuspended && (
              <LinkButton href={`/mail/${domain}/accounts/new`} variant="primary" icon={PlusIcon} {...shortcutTarget('add')}>
                {t('Add mail account')}
              </LinkButton>
            )}
          </>
        }
      />

      {domainSuspended && (
        <Banner
          variant="alert"
          icon={<WarningIcon weight="fill" />}
          title={t('This mail domain is suspended')}
          description={t("Its accounts can't receive mail or sign in until the domain is unsuspended.")}
        />
      )}

      <div className="grid gap-3">
        <TableToolbar
          search={{
            label: t('Search mail accounts'),
            placeholder: t('Search accounts, aliases or forwards'),
            value: table.query,
            onChange: table.setQuery,
          }}
          filters={
            <Select
              aria-label={t('Filter by status')}
              value={status}
              onValueChange={(v) => setStatus(v as keyof typeof statusFilters)}
              items={{ all: t(statusFilters.all), active: t(statusFilters.active), suspended: t(statusFilters.suspended) }}
              render={<Toolbar.Button />}
            />
          }
          selectedCount={readOnly ? 0 : table.selectedCount}
          onClearSelection={table.clearSelection}
          bulkActions={
            <>
              {isAdmin && (
                <>
                  <Button size="sm" icon={PauseCircleIcon} onClick={() => bulk('suspend', true)}>
                    {t('Suspend')}
                  </Button>
                  <Button size="sm" icon={PlayCircleIcon} onClick={() => bulk('unsuspend')}>
                    {t('Unsuspend')}
                  </Button>
                </>
              )}
              <Button size="sm" variant="secondary-destructive" icon={TrashIcon} onClick={() => bulk('delete', true)}>
                {t('Delete')}
              </Button>
            </>
          }
        />

        <TableCard
          rowShortcuts={keys}
          noun={{ one: t('mail account'), other: t('mail accounts') }}
          filteredCount={table.filteredCount}
          totalCount={all.length}
          onClearSearch={() => {
            table.setQuery('')
            setStatus('all')
          }}
          emptyState={{
            icon: <UserCircleIcon size={40} className="text-kumo-inactive" />,
            title: t('No mail accounts yet'),
            description: t('Create a mailbox such as %s to start receiving mail.', `hello@${domain}`),
            action: !readOnly && !domainSuspended && (
              <LinkButton href={`/mail/${domain}/accounts/new`} variant="primary" icon={PlusIcon}>
                {t('Add mail account')}
              </LinkButton>
            ),
          }}
        >
          <Table className="whitespace-nowrap">
            <Table.Header>
              <Table.Row>
                {!readOnly && (
                  <Table.CheckHead
                    checked={table.allSelected}
                    indeterminate={table.someSelected && !table.allSelected}
                    onCheckedChange={table.toggleAll}
                    label={t('Select all mail accounts')}
                  />
                )}
                <SortableHead sortKey="name" sort={table.sort} onSort={table.toggleSort} sticky="left">
                  {t('Name')}
                </SortableHead>
                <SortableHead sortKey="disk" sort={table.sort} onSort={table.toggleSort}>
                  {t('Disk / quota')}
                </SortableHead>
                <Table.Head>{t('Aliases')}</Table.Head>
                <Table.Head>{t('Forwarding')}</Table.Head>
                <Table.Head>{t('Auto reply')}</Table.Head>
                <Table.Head>{t('Status')}</Table.Head>
                <Table.Head sticky="right">
                  <span className="sr-only">{t('Actions')}</span>
                </Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {table.rows.map((a) => {
                const email = `${a.NAME}@${domain}`
                const href = `/mail/${domain}/accounts/${a.NAME}`
                const aliases = a.ALIAS ? a.ALIAS.split(',') : []
                const fwd = a.FWD === ':blackhole:' ? [] : a.FWD ? a.FWD.split(',') : []
                const suspended = a.SUSPENDED === 'yes'
                // Parity 7: needs the system webmail, a webmail client on the domain and a live account
                const hasWebmail = options?.webmail && d.WEBMAIL !== '' && d.WEBMAIL !== 'disabled' && !suspended
                return (
                  <Table.Row
                    key={a.NAME}
                    variant={table.isSelected(a.NAME) ? 'selected' : 'default'}
                    {...(suspended ? {} : rowLink(() => navigate(href)))}
                    {...keys.rowProps(a.NAME)}
                  >
                    {!readOnly && (
                      <Table.CheckCell
                        checked={table.isSelected(a.NAME)}
                        onCheckedChange={(_, details) => table.toggle(a.NAME, details)}
                        label={t('Select %s', email)}
                      />
                    )}
                    <Table.Cell sticky="left">
                      {suspended ? (
                        <Text bold as="span">
                          {email}
                        </Text>
                      ) : (
                        <RouterLink to={href} className="font-medium text-kumo-default hover:underline">
                          {email}
                        </RouterLink>
                      )}
                    </Table.Cell>
                    <Table.Cell>
                      <QuotaCell a={a} />
                    </Table.Cell>
                    <Table.Cell>
                      <ListCell items={aliases} empty={t('No aliases')} />
                    </Table.Cell>
                    <Table.Cell>
                      {a.FWD === ':blackhole:' ? (
                        <Badge variant="neutral">{t('Discard all mail')}</Badge>
                      ) : (
                        <span className="inline-flex items-center gap-1.5">
                          <ListCell items={fwd} empty={t('Disabled')} />
                          {a.FWD_ONLY === 'yes' && <Badge variant="outline">{t('Forward only')}</Badge>}
                        </span>
                      )}
                    </Table.Cell>
                    <Table.Cell>
                      {a.AUTOREPLY !== 'no' ? (
                        <Badge variant="blue">{t('Enabled')}</Badge>
                      ) : (
                        <span className="text-kumo-subtle">{t('Disabled')}</span>
                      )}
                    </Table.Cell>
                    <Table.Cell>
                      <StatusBadge suspended={suspended} />
                    </Table.Cell>
                    <Table.Cell sticky="right" className="text-right">
                      <RowActions
                        a={a}
                        domain={domain}
                        domainSuspended={domainSuspended}
                        webmailHref={hasWebmail ? mailWebmailUrl(options.webmailAlias, domain, a.NAME) : ''}
                        readOnly={readOnly}
                        isAdmin={isAdmin}
                        ask={confirm.ask}
                        onDelete={del.ask}
                        run={suspendRun}
                      />
                    </Table.Cell>
                  </Table.Row>
                )
              })}
            </Table.Body>
          </Table>
        </TableCard>
      </div>

      <ListConfirmDialog confirm={confirm} />
      <ResourceDeleteDialog
        state={del}
        resourceType={t('mail account')}
        onDelete={(email) => m.deleteAccount(domain, email.split('@')[0])}
      />
    </div>
  )
}
