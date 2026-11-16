import {
  Badge,
  Button,
  DropdownMenu,
  Empty,
  LinkButton,
  Select,
  Switch,
  Table,
  Text,
  Toolbar,
  useKumoToastManager,
} from '@cloudflare/kumo'
import {
  ArrowDownIcon,
  ArrowUpIcon,
  PauseCircleIcon,
  PencilSimpleIcon,
  PlayCircleIcon,
  PlusIcon,
  ShieldIcon,
  ShieldSlashIcon,
  TrashIcon,
} from '@phosphor-icons/react'
import { useMemo, useState } from 'react'
import { Link as RouterLink, useNavigate } from 'react-router'
import { useFirewall, useServerMutation } from '../../api/server'
import type { BulkReply, FirewallRuleRow } from '../../api/types.gen'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { PageHeader } from '../../components/PageHeader'
import { TableCard, TableToolbar, rowLink, rowMenuTrigger, useDataTable } from '../../components/table'
import { useT } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { DnsError, DnsLoading } from '../dns/shared'
import { FirewallNav } from './FirewallNav'
import { ruleComment, useRuleError } from './firewallShared'
import { ErrorNote, JobPanel } from './parts'
import { failureText } from '../../api/errors'
import { shortcutTarget } from '../../shortcuts/targets'
import { useRowShortcuts } from '../../shortcuts/useRowShortcuts'

const actionFilters = { all: msgid('Show all'), ACCEPT: 'ACCEPT', DROP: 'DROP' }

export function ActionBadge({ action }: { action: string }) {
  return action === 'ACCEPT' ? <Badge variant="green">ACCEPT</Badge> : <Badge variant="red">DROP</Badge>
}

type Ask = {
  key: string
  title: string
  description: string
  label: string
  destructive?: boolean
  done: string
  run: () => Promise<unknown>
}

export function FirewallRulesPage() {
  const toast = useKumoToastManager()
  const t = useT()
  const navigate = useNavigate()
  const view = useFirewall()
  const m = useServerMutation()
  const ruleError = useRuleError()
  const [action, setAction] = useState<keyof typeof actionFilters>('all')
  const [ask, setAsk] = useState<Ask | null>(null)
  const [askOpen, setAskOpen] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState('')
  const [jobId, setJobId] = useState('')
  const [running, setJobRunning] = useState(false)

  const rules = useMemo<FirewallRuleRow[]>(() => view.data?.rules ?? [], [view.data])
  const byAction = useMemo(() => (action === 'all' ? rules : rules.filter((r) => r.action === action)), [rules, action])
  // No column sorting: iptables evaluates rules top to bottom, so the list shows the real order.
  const table = useDataTable({
    rows: byAction,
    getId: (r) => r.id,
    // Protected rules can't be bulk-deleted (the API refuses the whole batch)
    canSelect: (r) => r.protected === '',
    search: (r, q) =>
      r.comment.toLowerCase().includes(q) ||
      ruleComment(r.comment).toLowerCase().includes(q) ||
      r.port.includes(q) ||
      r.ip.includes(q) ||
      r.protocol.toLowerCase().includes(q),
  })

  // Evaluation order is by id (highest first), so the ends are the extreme ids.
  const ids = rules.map((r) => Number(r.id))
  const first = Math.max(...ids)
  const last = Math.min(...ids)

  /** Runs a confirmed action; a refusal lands in the banner and the list is refetched by the mutation. */
  const act = async (a: Ask) => {
    setError('')
    setBusy(a.key)
    try {
      const result = await a.run()
      const failed = (result as BulkReply | undefined)?.failed ?? []
      if (failed.length > 0) setError(failed.map((f) => failureText(f, t)).join('\n'))
      else toast.add({ title: a.done, type: 'success' })
      table.clearSelection()
    } catch (err) {
      setError(ruleError(err))
    } finally {
      setBusy('')
    }
  }
  const confirm = (a: Ask) => {
    setAsk(a)
    setAskOpen(true)
  }

  const toggle = async (enabled: boolean) => {
    setError('')
    setBusy('toggle')
    try {
      const ref = await m.toggleFirewall({ enabled })
      setJobRunning(true)
      setJobId(ref.jobId)
    } catch (err) {
      setError(ruleError(err))
    } finally {
      setBusy('')
    }
  }
  const rowAsk = (r: FirewallRuleRow, verb: 'delete' | 'suspend' | 'unsuspend' | 'move-up' | 'move-down'): Ask => {
    const id = r.id
    const text = {
      delete: [
        t('Delete rule?'),
        t('Are you sure you want to delete rule #%s?', id),
        t('Delete'),
        t('Rule #%s deleted', id),
      ],
      suspend: [
        t('Suspend rule?'),
        t('Are you sure you want to suspend rule #%s?', id),
        t('Suspend'),
        t('Rule #%s suspended', id),
      ],
      unsuspend: [
        t('Unsuspend rule?'),
        t('Are you sure you want to unsuspend rule #%s?', id),
        t('Unsuspend'),
        t('Rule #%s unsuspended', id),
      ],
      'move-up': [
        t('Move rule up?'),
        t('Are you sure you want to move rule #%s up?', id),
        t('Move up'),
        t('Rule #%s moved up', id),
      ],
      'move-down': [
        t('Move rule down?'),
        t('Are you sure you want to move rule #%s down?', id),
        t('Move down'),
        t('Rule #%s moved down', id),
      ],
    }[verb]
    return {
      key: id,
      title: text[0],
      description: text[1],
      label: text[2],
      done: text[3],
      destructive: verb === 'delete',
      run: () => m.ruleAction(id, verb, { expect: r.expect }),
    }
  }

  // Hestia's rule list keys: Enter edit, s suspend, Delete (never a protected rule).
  const keys = useRowShortcuts(
    table.rows,
    (r) => r.id,
    (r) => ({
      enter: () => navigate(`/server/firewall/${r.id}`),
      ...(r.protected === '' &&
        busy === '' && {
          s: () => confirm(rowAsk(r, r.suspended ? 'unsuspend' : 'suspend')),
          delete: () => confirm(rowAsk(r, 'delete')),
        }),
    }),
  )
  const [stopOpen, setStopOpen] = useState(false)

  if (view.error) return <DnsError error={view.error} />
  if (!view.data) return <DnsLoading />
  const enabled = view.data.enabled

  return (
    <div className="grid gap-6">
      <PageHeader
        title={t('Firewall')}
        description={t('iptables rules applied to incoming traffic, in order from top to bottom.')}
        actions={
          <>
            <div className="flex h-9 items-center rounded-lg px-3 ring ring-kumo-line">
              <Switch
                label={enabled ? t('Firewall running') : t('Firewall stopped')}
                checked={enabled}
                disabled={busy === 'toggle' || running}
                onCheckedChange={(v) => (v ? void toggle(true) : setStopOpen(true))}
              />
            </div>
            {enabled && (
              <LinkButton href="/server/firewall/new" variant="primary" icon={PlusIcon} {...shortcutTarget('add')}>
                {t('Add rule')}
              </LinkButton>
            )}
          </>
        }
      />

      <FirewallNav current="/server/firewall" />

      {error && <ErrorNote error={error} />}
      <JobPanel jobId={jobId} onDone={() => setJobRunning(false)} />

      {!enabled ? (
        <Empty
          icon={<ShieldSlashIcon size={40} className="text-kumo-inactive" />}
          title={t('The firewall is disabled')}
          description={t('All ports are open. Turn the firewall on to enforce rules, bans and IP lists.')}
          contents={
            <Button
              variant="primary"
              icon={ShieldIcon}
              disabled={busy === 'toggle' || running}
              onClick={() => void toggle(true)}
            >
              {t('Turn on firewall')}
            </Button>
          }
          className="py-16"
        />
      ) : (
        <div className="grid gap-3">
          <TableToolbar
            search={{
              label: t('Search firewall rules'),
              placeholder: t('Search ports, IPs or comments'),
              value: table.query,
              onChange: table.setQuery,
            }}
            filters={
              <Select
                aria-label={t('Filter by action')}
                value={action}
                onValueChange={(v) => setAction(v as keyof typeof actionFilters)}
                items={Object.fromEntries(Object.entries(actionFilters).map(([k, v]) => [k, t(v)]))}
                render={<Toolbar.Button />}
              />
            }
            selectedCount={table.selectedCount}
            onClearSelection={table.clearSelection}
            bulkActions={
              <Button
                size="sm"
                variant="secondary-destructive"
                icon={TrashIcon}
                onClick={() => {
                  const picked = table.selectedRows
                  confirm({
                    key: 'bulk',
                    title: t('Delete %s rules?', picked.length),
                    description: t('Are you sure you want to delete the selected firewall rules?'),
                    label: t('Delete'),
                    destructive: true,
                    done: t('Firewall rules deleted'),
                    run: () =>
                      m.bulkRules({ action: 'delete', items: picked.map((r) => ({ id: r.id, expect: r.expect })) }),
                  })
                }}
              >
                {t('Delete')}
              </Button>
            }
          />

          <TableCard
            rowShortcuts={keys}
            noun={{ one: t('rule'), other: t('rules') }}
            filteredCount={table.filteredCount}
            totalCount={rules.length}
            onClearSearch={() => {
              table.setQuery('')
              setAction('all')
            }}
            emptyState={{
              icon: <ShieldIcon size={40} className="text-kumo-inactive" />,
              title: t('No firewall rules'),
              description: t(
                'Without rules every port is closed except those opened by default. Add a rule to allow traffic.',
              ),
              action: (
                <LinkButton href="/server/firewall/new" variant="primary" icon={PlusIcon}>
                  {t('Add rule')}
                </LinkButton>
              ),
            }}
          >
            <Table className="whitespace-nowrap">
              <Table.Header>
                <Table.Row>
                  <Table.CheckHead
                    checked={table.allSelected}
                    indeterminate={table.someSelected && !table.allSelected}
                    onCheckedChange={table.toggleAll}
                    label={t('Select all rules')}
                  />
                  <Table.Head sticky="left" className="text-right" title={t('Rules are applied in this order')}>
                    {t('Order')}
                  </Table.Head>
                  <Table.Head>{t('Action')}</Table.Head>
                  <Table.Head>{t('Comment')}</Table.Head>
                  <Table.Head>{t('Protocol')}</Table.Head>
                  <Table.Head>{t('Port')}</Table.Head>
                  <Table.Head>{t('IP address')}</Table.Head>
                  <Table.Head>{t('Status')}</Table.Head>
                  <Table.Head sticky="right">
                    <span className="sr-only">{t('Actions')}</span>
                  </Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {table.rows.map((r) => {
                  const id = r.id
                  const href = `/server/firewall/${id}`
                  const locked = r.protected !== ''
                  return (
                    <Table.Row
                      key={id}
                      variant={table.isSelected(id) ? 'selected' : 'default'}
                      {...rowLink(() => navigate(href))}
                      {...keys.rowProps(id)}
                    >
                      <Table.CheckCell
                        checked={table.isSelected(id)}
                        disabled={locked}
                        onCheckedChange={(_, details) => table.toggle(id, details)}
                        label={t('Select rule %s', id)}
                      />
                      <Table.Cell sticky="left" className="text-right tabular-nums">
                        <RouterLink
                          to={href}
                          className="font-medium text-kumo-default hover:underline"
                          aria-label={t('Edit rule %s', id)}
                        >
                          {id}
                        </RouterLink>
                      </Table.Cell>
                      <Table.Cell>
                        <ActionBadge action={r.action} />
                      </Table.Cell>
                      <Table.Cell>
                        {r.comment ? (
                          <Text bold as="span">
                            {ruleComment(r.comment)}
                          </Text>
                        ) : (
                          <Text variant="secondary" as="span">
                            -
                          </Text>
                        )}
                      </Table.Cell>
                      <Table.Cell>{r.protocol}</Table.Cell>
                      <Table.Cell>
                        <Text variant="mono" as="span">
                          {r.port === '0' ? t('All') : r.port}
                        </Text>
                      </Table.Cell>
                      <Table.Cell>
                        <Text variant="mono" as="span">
                          {r.ip}
                        </Text>
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
                      <Table.Cell sticky="right" className="text-right">
                        <DropdownMenu>
                          <DropdownMenu.Trigger render={rowMenuTrigger(t('Actions for rule %s', id))} />
                          <DropdownMenu.Content align="end">
                            <DropdownMenu.Item icon={PencilSimpleIcon} onClick={() => navigate(href)}>
                              {t('Edit firewall rule')}
                            </DropdownMenu.Item>
                            {!locked && (
                              <>
                                {Number(id) !== first && (
                                  <DropdownMenu.Item
                                    icon={ArrowUpIcon}
                                    disabled={busy !== ''}
                                    onClick={() => confirm(rowAsk(r, 'move-up'))}
                                  >
                                    {t('Move up')}
                                  </DropdownMenu.Item>
                                )}
                                {Number(id) !== last && (
                                  <DropdownMenu.Item
                                    icon={ArrowDownIcon}
                                    disabled={busy !== ''}
                                    onClick={() => confirm(rowAsk(r, 'move-down'))}
                                  >
                                    {t('Move down')}
                                  </DropdownMenu.Item>
                                )}
                                <DropdownMenu.Separator />
                                <DropdownMenu.Item
                                  icon={r.suspended ? PlayCircleIcon : PauseCircleIcon}
                                  disabled={busy !== ''}
                                  onClick={() => confirm(rowAsk(r, r.suspended ? 'unsuspend' : 'suspend'))}
                                >
                                  {r.suspended ? t('Unsuspend') : t('Suspend')}
                                </DropdownMenu.Item>
                                <DropdownMenu.Item
                                  icon={TrashIcon}
                                  variant="danger"
                                  disabled={busy !== ''}
                                  onClick={() => confirm(rowAsk(r, 'delete'))}
                                >
                                  {t('Delete')}
                                </DropdownMenu.Item>
                              </>
                            )}
                          </DropdownMenu.Content>
                        </DropdownMenu>
                      </Table.Cell>
                    </Table.Row>
                  )
                })}
              </Table.Body>
            </Table>
          </TableCard>
        </div>
      )}

      <ConfirmDialog
        open={askOpen}
        onOpenChange={setAskOpen}
        title={ask?.title ?? ''}
        description={ask?.description ?? ''}
        confirmLabel={ask?.label ?? ''}
        destructive={ask?.destructive}
        onConfirm={() => ask && void act(ask)}
      />
      <ConfirmDialog
        open={stopOpen}
        onOpenChange={setStopOpen}
        title={t('Stop the firewall?')}
        description={t(
          'All rules, bans and IP lists stop being enforced and every port on the server becomes reachable.',
        )}
        confirmLabel={t('Stop firewall')}
        destructive
        onConfirm={() => void toggle(false)}
      />
    </div>
  )
}
