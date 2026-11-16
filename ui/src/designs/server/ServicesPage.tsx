import {
  Badge,
  Banner,
  Button,
  Dialog,
  DropdownMenu,
  Input,
  LayerCard,
  LinkButton,
  Loader,
  Select,
  Table,
  Text,
  Toolbar,
  useKumoToastManager,
} from '@cloudflare/kumo'
import {
  ArrowCounterClockwiseIcon,
  BinocularsIcon,
  ChartLineIcon,
  GearIcon,
  GearSixIcon,
  HardDrivesIcon,
  PackageIcon,
  PencilSimpleIcon,
  PlayIcon,
  ShieldIcon,
  StopIcon,
  WarningCircleIcon,
} from '@phosphor-icons/react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link as RouterLink, useNavigate } from 'react-router'
import { apiErrorText, failureText } from '../../api/errors'
import { useServerMutation, useServices } from '../../api/server'
import type { SysInfo } from '../../api/hestia.gen'
import type { BulkReply, JobStatus, ServiceRow } from '../../api/types.gen'
import { PageHeader } from '../../components/PageHeader'
import {
  ColumnsMenu,
  SortableHead,
  TableCard,
  TableToolbar,
  rowLink,
  rowMenuTrigger,
  useColumnVisibility,
  useDataTable,
  type ToggleableColumn,
} from '../../components/table'
import { useT } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { useSession, useSessionUser } from '../../session/SessionProvider'
import { formatServiceRole, humanizeMinutes } from './format'
import { JobProgress } from './JobProgress'
import { useRowShortcuts } from '../../shortcuts/useRowShortcuts'

type Verb = 'restart' | 'stop' | 'start'
type Pending = { verb: Verb; service: string } | null

/** Services whose stop locks the admin out; they need the name typed, on their own row. */
const guarded = ['ssh', 'iptables']

/**
 * A confirm that stays disabled until `expected` is typed (stopping ssh or the firewall,
 * restarting the server). Always rendered; `open` shows it, so it animates.
 */
function TypedConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  expected,
  confirmLabel,
  onConfirm,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description: string
  expected: string
  confirmLabel: string
  onConfirm: () => void
}) {
  const t = useT()
  const [typed, setTyped] = useState('')
  useEffect(() => {
    if (!open) setTyped('')
  }, [open])
  const matches = typed === expected
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog className="grid gap-4 px-6 py-5">
        <div className="grid gap-1.5">
          <Dialog.Title className="text-lg font-semibold">{title}</Dialog.Title>
          <Dialog.Description className="text-kumo-subtle">{description}</Dialog.Description>
        </div>
        <Input
          aria-label={t('Type %s to confirm', expected)}
          label={t('Type %s to confirm', expected)}
          value={typed}
          onChange={(e) => setTyped(e.target.value)}
          placeholder={expected}
          autoComplete="off"
          spellCheck={false}
        />
        <div className="flex justify-end gap-2">
          <Dialog.Close render={(props) => <Button {...props}>{t('Cancel')}</Button>} />
          <Button
            variant="destructive"
            disabled={!matches}
            onClick={() => {
              onConfirm()
              onOpenChange(false)
            }}
          >
            {confirmLabel}
          </Button>
        </div>
      </Dialog>
    </Dialog.Root>
  )
}

/** A plain confirm for the services whose stop is harmless. */
function PlainConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel,
  destructive,
  onConfirm,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description: string
  confirmLabel: string
  destructive: boolean
  onConfirm: () => void
}) {
  const t = useT()
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog className="grid gap-4 px-6 py-5">
        <div className="grid gap-1.5">
          <Dialog.Title className="text-lg font-semibold">{title}</Dialog.Title>
          <Dialog.Description className="text-kumo-subtle">{description}</Dialog.Description>
        </div>
        <div className="flex justify-end gap-2">
          <Dialog.Close render={(props) => <Button {...props}>{t('Cancel')}</Button>} />
          <Button
            variant={destructive ? 'destructive' : 'primary'}
            onClick={() => {
              onConfirm()
              onOpenChange(false)
            }}
          >
            {confirmLabel}
          </Button>
        </div>
      </Dialog>
    </Dialog.Root>
  )
}

function ServiceState({ state }: { state: string }) {
  const t = useT()
  return state === 'running' ? (
    <Badge variant="success" appearance="dot">
      {t('Running')}
    </Badge>
  ) : (
    <Badge variant="error" appearance="dot">
      {t('Stopped')}
    </Badge>
  )
}

function ServerSummary({ info, release }: { info: SysInfo; release: string }) {
  const t = useT()
  const appName = useSession().bootstrap?.appName ?? ''
  const items = [
    {
      label: appName,
      value: (
        <span className="flex items-center gap-1.5">
          {`v${info.HESTIA}`}
          {release === 'release' ? (
            <Badge variant="neutral">{t('Production release')}</Badge>
          ) : (
            <Badge variant="warning">{release}</Badge>
          )}
        </span>
      ),
    },
    { label: t('Operating system'), value: `${info.OS} ${info.VERSION} (${info.ARCH})` },
    { label: t('Load average (1m / 5m / 15m)'), value: info.LOADAVERAGE },
    { label: t('Uptime'), value: humanizeMinutes(info.UPTIME, t) },
  ]
  return (
    <LayerCard className="grid gap-4 px-5 py-4">
      <div className="flex items-center gap-3">
        <div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-kumo-tint text-kumo-default">
          <HardDrivesIcon size={22} />
        </div>
        <div className="grid min-w-0 gap-0.5">
          <Text variant="heading" as="h2" DANGEROUS_className="truncate">
            {info.HOSTNAME}
          </Text>
          <Text variant="secondary" size="sm">
            {t('Server summary')}
          </Text>
        </div>
      </div>
      <dl className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {items.map((item) => (
          <div key={item.label} className="grid gap-1">
            <dt>
              <Text variant="secondary" size="sm" as="span">
                {item.label}
              </Text>
            </dt>
            <dd>
              <Text as="span" bold>
                {item.value}
              </Text>
            </dd>
          </div>
        ))}
      </dl>
    </LayerCard>
  )
}

const optionalColumns: ToggleableColumn[] = [
  { key: 'description', label: msgid('Description') },
  { key: 'uptime', label: msgid('Uptime') },
]

const confirmText: Record<Verb, string> = {
  restart: msgid('Are you sure you want to restart the %s service?'),
  stop: msgid('Are you sure you want to stop the %s service?'),
  start: msgid('Are you sure you want to start the %s service?'),
}
const verbLabel: Record<Verb, string> = { restart: msgid('Restart'), stop: msgid('Stop'), start: msgid('Start') }
const verbTitle: Record<Verb, string> = {
  restart: msgid('Restart %s?'),
  stop: msgid('Stop %s?'),
  start: msgid('Start %s?'),
}

export function ServicesPage() {
  const t = useT()
  const toast = useKumoToastManager()
  const navigate = useNavigate()
  const features = useSessionUser()?.features
  const { data, error, refetch } = useServices()
  const { serviceAction, bulkServices, reboot } = useServerMutation()

  const [state, setState] = useState<'all' | 'running' | 'stopped'>('all')
  const [pending, setPending] = useState<Pending>(null)
  const [pendingOpen, setPendingOpen] = useState(false)
  const [rebootOpen, setRebootOpen] = useState(false)
  const [jobId, setJobId] = useState('')
  const [jobDone, setJobDone] = useState(true)
  const [failed, setFailed] = useState<BulkReply['failed']>([])
  const columns = useColumnVisibility(
    'server-services',
    optionalColumns.map((c) => ({ ...c, label: t(c.label) })),
  )
  const show = columns.isVisible

  const all = useMemo<ServiceRow[]>(() => data?.services ?? [], [data])
  const byState = useMemo(() => (state === 'all' ? all : all.filter((s) => s.state === state)), [all, state])
  // Hestia lists services by name, which is the order the API sends.
  const table = useDataTable({
    rows: byState,
    getId: (s) => s.name,
    search: (s, q) => s.name.includes(q) || s.system.includes(q),
    sorts: {
      name: (s) => s.name,
      uptime: (s) => Number(s.rtime),
      cpu: (s) => Number(s.cpu),
      memory: (s) => Number(s.mem),
    },
  })

  const busy = jobId !== '' && !jobDone
  const follow = (id: string) => {
    setJobId(id)
    setJobDone(false)
    setFailed([])
  }
  const run = async (start: () => Promise<{ jobId: string }>) => {
    try {
      follow((await start()).jobId)
    } catch (e) {
      toast.add({ title: apiErrorText(e, t), type: 'error' })
    }
  }
  const onDone = useCallback((status: JobStatus) => {
    setJobDone(true)
    const result = status.result as BulkReply | null
    if (status.state === 'done' && result?.failed?.length) setFailed(result.failed)
  }, [])

  const ask = (verb: Verb, service: string) => {
    setPending({ verb, service })
    setPendingOpen(true)
  }
  const confirmPending = () => {
    if (!pending) return
    const { verb, service } = pending
    void run(() =>
      serviceAction(service, verb, verb === 'stop' && guarded.includes(service) ? { confirm: service } : {}),
    )
  }
  const bulk = (action: Verb) => {
    const names = table.selectedRows.map((s) => s.name)
    table.clearSelection()
    void run(() => bulkServices({ action, names }))
  }
  const guardedSelected = table.selectedRows.filter((s) => guarded.includes(s.name)).map((s) => s.name)

  const hostname = data?.info.HOSTNAME ?? ''
  const showFirewall = (data?.firewall ?? false) && (features?.firewall ?? false)

  // iptables has no editor; its configuration is the firewall page.
  const editHref = (s: ServiceRow) =>
    s.editor
      ? `/server/services/${encodeURIComponent(s.editor)}`
      : s.name === 'iptables' && showFirewall
        ? '/server/firewall'
        : ''
  // Hestia's service list keys: Enter edit, s restart, Delete stop or start.
  const keys = useRowShortcuts(
    table.rows,
    (s) => s.name,
    (s) => ({
      enter: editHref(s) ? () => navigate(editHref(s)) : undefined,
      ...(!busy && {
        s: () => ask('restart', s.name),
        delete: () => ask(s.state === 'running' ? 'stop' : 'start', s.name),
      }),
    }),
  )

  return (
    <div className="grid gap-6">
      <PageHeader
        title={t('Services')}
        description={t('System services running on this server, with their resource use.')}
        actions={
          <>
            <LinkButton href="/server/config" variant="secondary" icon={GearSixIcon}>
              {t('Configure')}
            </LinkButton>
            <LinkButton href="/server/graphs" variant="secondary" icon={ChartLineIcon}>
              {t('Graphs')}
            </LinkButton>
            <LinkButton href="/server/updates" variant="secondary" icon={PackageIcon}>
              {t('Updates')}
            </LinkButton>
            {showFirewall && (
              <LinkButton href="/server/firewall" variant="secondary" icon={ShieldIcon}>
                {t('Firewall')}
              </LinkButton>
            )}
            <LinkButton href="/logs?user=system" variant="secondary" icon={BinocularsIcon}>
              {t('Logs')}
            </LinkButton>
            <Button
              variant="destructive"
              icon={ArrowCounterClockwiseIcon}
              disabled={!data || busy}
              onClick={() => setRebootOpen(true)}
            >
              {t('Restart server')}
            </Button>
          </>
        }
      />

      {error ? (
        <div role="alert">
          <Banner
            variant="error"
            icon={<WarningCircleIcon />}
            title={apiErrorText(error, t)}
            className="whitespace-pre-line"
          />
        </div>
      ) : !data ? (
        <Loader />
      ) : (
        <>
          <ServerSummary info={data.info} release={data.release} />

          {jobId && (
            <LayerCard className="px-5 py-4">
              <JobProgress jobId={jobId} onDone={onDone} />
            </LayerCard>
          )}
          {failed.length > 0 && (
            <div role="alert">
              <Banner
                variant="error"
                icon={<WarningCircleIcon />}
                title={failed.map((f) => failureText(f, t)).join('\n')}
                className="whitespace-pre-line"
              />
            </div>
          )}

          <div className="grid gap-3">
            <TableToolbar
              search={{
                label: t('Search services'),
                placeholder: t('Search services or roles'),
                value: table.query,
                onChange: table.setQuery,
              }}
              filters={
                <Select
                  aria-label={t('Filter by status')}
                  value={state}
                  onValueChange={(v) => setState(v as typeof state)}
                  items={{ all: t('Show all'), running: t('Running'), stopped: t('Stopped') }}
                  render={<Toolbar.Button />}
                />
              }
              onRefresh={async () => {
                await refetch()
              }}
              selectedCount={table.selectedCount}
              onClearSelection={table.clearSelection}
              bulkActions={
                <>
                  {guardedSelected.length > 0 && (
                    <Text variant="secondary" size="sm" as="span">
                      {t('Stop "%s" from its own row.', guardedSelected.join(', '))}
                    </Text>
                  )}
                  <Button size="sm" icon={ArrowCounterClockwiseIcon} disabled={busy} onClick={() => bulk('restart')}>
                    {t('Restart')}
                  </Button>
                  <Button size="sm" icon={PlayIcon} disabled={busy} onClick={() => bulk('start')}>
                    {t('Start')}
                  </Button>
                  <Button
                    size="sm"
                    variant="secondary-destructive"
                    icon={StopIcon}
                    disabled={busy || guardedSelected.length > 0}
                    onClick={() => bulk('stop')}
                  >
                    {t('Stop')}
                  </Button>
                </>
              }
            />

            <TableCard
              rowShortcuts={keys}
              noun={{ one: t('service'), other: t('services') }}
              filteredCount={table.filteredCount}
              totalCount={all.length}
              onClearSearch={() => {
                table.setQuery('')
                setState('all')
              }}
              emptyState={{
                icon: <GearIcon size={40} className="text-kumo-inactive" />,
                title: t('No services found'),
                description: t('The service list could not be read from this server.'),
              }}
            >
              <Table className="whitespace-nowrap">
                <Table.Header>
                  <Table.Row>
                    <Table.CheckHead
                      checked={table.allSelected}
                      indeterminate={table.someSelected && !table.allSelected}
                      onCheckedChange={table.toggleAll}
                      label={t('Select all services')}
                    />
                    <SortableHead sortKey="name" sort={table.sort} onSort={table.toggleSort} sticky="left">
                      {t('Service')}
                    </SortableHead>
                    {show('description') && <Table.Head>{t('Description')}</Table.Head>}
                    <Table.Head>{t('Status')}</Table.Head>
                    {show('uptime') && (
                      <SortableHead sortKey="uptime" sort={table.sort} onSort={table.toggleSort} align="right">
                        {t('Uptime')}
                      </SortableHead>
                    )}
                    <SortableHead sortKey="cpu" sort={table.sort} onSort={table.toggleSort} align="right">
                      {t('CPU')}
                    </SortableHead>
                    <SortableHead sortKey="memory" sort={table.sort} onSort={table.toggleSort} align="right">
                      {t('Memory')}
                    </SortableHead>
                    <Table.Head sticky="right" className="w-px text-right">
                      <ColumnsMenu visibility={columns} />
                    </Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {table.rows.map((s) => {
                    const running = s.state === 'running'
                    const href = editHref(s)
                    const link = href ? rowLink(() => navigate(href)) : undefined
                    return (
                      <Table.Row
                        key={s.name}
                        variant={table.isSelected(s.name) ? 'selected' : 'default'}
                        {...link}
                        {...keys.rowProps(s.name)}
                        className={`${link?.className ?? ''} ${running ? '' : 'opacity-60'}`}
                      >
                        <Table.CheckCell
                          checked={table.isSelected(s.name)}
                          onCheckedChange={(_, details) => table.toggle(s.name, details)}
                          label={t('Select %s', s.name)}
                        />
                        <Table.Cell sticky="left">
                          {href ? (
                            <RouterLink to={href} className="font-medium text-kumo-default hover:underline">
                              {s.name}
                            </RouterLink>
                          ) : (
                            <Text bold as="span">
                              {s.name}
                            </Text>
                          )}
                        </Table.Cell>
                        {show('description') && (
                          <Table.Cell>
                            <Text variant="secondary" as="span">
                              {formatServiceRole(s.system)}
                            </Text>
                          </Table.Cell>
                        )}
                        <Table.Cell>
                          <ServiceState state={s.state} />
                        </Table.Cell>
                        {show('uptime') && (
                          <Table.Cell className="text-right tabular-nums">{humanizeMinutes(s.rtime, t)}</Table.Cell>
                        )}
                        <Table.Cell className="text-right tabular-nums">
                          {running ? (s.cpu.trim() ? `${s.cpu}%` : '–') : '-'}
                        </Table.Cell>
                        <Table.Cell className="text-right tabular-nums">
                          {running ? (s.mem.trim() ? `${s.mem} MB` : '–') : '-'}
                        </Table.Cell>
                        <Table.Cell sticky="right" className="text-right">
                          <DropdownMenu>
                            <DropdownMenu.Trigger render={rowMenuTrigger(t('Actions for %s', s.name))} />
                            <DropdownMenu.Content align="end">
                              {href && (
                                <DropdownMenu.LinkItem icon={PencilSimpleIcon} href={href}>
                                  {t('Edit configuration')}
                                </DropdownMenu.LinkItem>
                              )}
                              <DropdownMenu.Item
                                icon={ArrowCounterClockwiseIcon}
                                disabled={busy}
                                onClick={() => ask('restart', s.name)}
                              >
                                {t('Restart')}
                              </DropdownMenu.Item>
                              <DropdownMenu.Separator />
                              {running ? (
                                <DropdownMenu.Item
                                  icon={StopIcon}
                                  variant="danger"
                                  disabled={busy}
                                  onClick={() => ask('stop', s.name)}
                                >
                                  {t('Stop')}
                                </DropdownMenu.Item>
                              ) : (
                                <DropdownMenu.Item icon={PlayIcon} disabled={busy} onClick={() => ask('start', s.name)}>
                                  {t('Start')}
                                </DropdownMenu.Item>
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
        </>
      )}

      <PlainConfirmDialog
        open={pendingOpen && !(pending?.verb === 'stop' && guarded.includes(pending.service))}
        onOpenChange={setPendingOpen}
        title={pending ? t(verbTitle[pending.verb], pending.service) : ''}
        description={pending ? t(confirmText[pending.verb], pending.service) : ''}
        confirmLabel={pending ? t(verbLabel[pending.verb]) : ''}
        destructive={pending?.verb === 'stop'}
        onConfirm={confirmPending}
      />
      <TypedConfirmDialog
        open={pendingOpen && pending?.verb === 'stop' && guarded.includes(pending.service)}
        onOpenChange={setPendingOpen}
        title={pending ? t('Stop %s?', pending.service) : ''}
        description={t('Stopping this service can lock you out of the server or leave it unprotected.')}
        expected={pending?.service ?? ''}
        confirmLabel={pending ? t('Stop %s', pending.service) : ''}
        onConfirm={confirmPending}
      />
      <TypedConfirmDialog
        open={rebootOpen}
        onOpenChange={setRebootOpen}
        title={t('Restart server?')}
        description={t('Every website, mailbox and database on this server will be unavailable until it boots again.')}
        expected={hostname}
        confirmLabel={t('Restart server')}
        onConfirm={() => void run(() => reboot({ hostname }))}
      />
    </div>
  )
}
