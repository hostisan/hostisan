import {
  Badge,
  Banner,
  Button,
  Checkbox,
  ClipboardText,
  DropdownMenu,
  Empty,
  Input,
  LinkButton,
  Table,
  Text,
  useKumoToastManager,
} from '@cloudflare/kumo'
import { EyeIcon, LockKeyIcon, PlusIcon, TrashIcon, WarningIcon } from '@phosphor-icons/react'
import { useEffect, useState, type FormEvent } from 'react'
import { Link as RouterLink, useLocation, useNavigate, useParams } from 'react-router'
import { useQueryClient } from '@tanstack/react-query'
import { accountKeys, useAccessKey, useAccessKeys, useKeyMutation } from '../../api/account'
import { ApiError, api } from '../../api/client'
import type { AccessKeyCreated } from '../../api/types.gen'
import { apiErrorText } from '../../api/errors'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { braceMessage } from '../../components/BraceMessage'
import { FormPage } from '../../components/FormPage'
import type { FormGuard } from '../../unsaved/types'
import { FormSection } from '../../components/FormSection'
import { PageHeader } from '../../components/PageHeader'
import { SortableHead, TableCard, TableToolbar, rowLink, rowMenuTrigger, useDataTable } from '../../components/table'
import { ListConfirmDialog, useConfirm } from '../../components/useListConfirm'
import { useT } from '../../i18n/I18nProvider'
import { useSessionUser } from '../../session/SessionProvider'
import { DnsError, DnsLoading } from '../dns/shared'
import { shortcutTarget } from '../../shortcuts/targets'
import { useRowShortcuts } from '../../shortcuts/useRowShortcuts'

const listHref = '/account/access-keys'

function PermissionBadges({ permissions }: { permissions: string[] }) {
  return (
    <div className="flex flex-wrap gap-1">
      {permissions.map((p) => (
        <Badge key={p} variant="neutral">
          {p}
        </Badge>
      ))}
    </div>
  )
}

export function AccessKeysPage() {
  const navigate = useNavigate()
  const t = useT()
  const readOnly = useSessionUser()?.readOnly ?? false
  const list = useAccessKeys()
  const m = useKeyMutation()
  const confirm = useConfirm()
  const keys = list.data?.keys ?? []

  const table = useDataTable({
    rows: keys,
    getId: (k) => k.ACCESS_KEY_ID,
    search: (k, q) => k.ACCESS_KEY_ID.toLowerCase().includes(q) || k.COMMENT.toLowerCase().includes(q),
    sorts: {
      key: (k) => k.ACCESS_KEY_ID,
      comment: (k) => k.COMMENT,
      date: (k) => `${k.DATE} ${k.TIME}`,
    },
    initialSort: { key: 'date', dir: 'desc' },
  })

  const askDelete = (ids: string[]) =>
    confirm.ask({
      title: t('Delete access key'),
      description: `${t('Are you sure you want to delete access key %s?', ids.join(', '))} ${t('Integrations using it stop working immediately.')}`,
      confirmLabel: t('Delete'),
      destructive: true,
      done: t('Access key deleted'),
      onDone: table.clearSelection,
      run: () =>
        ids.length === 1 ? m.removeAccessKey(ids[0]) : m.bulkAccessKeys({ action: 'delete', names: ids }),
    })
  // Hestia's access key list keys: Enter open, Delete.
  const rowKeys = useRowShortcuts(table.rows, (k) => k.ACCESS_KEY_ID, (k) => ({
    enter: () => navigate(`${listHref}/${k.ACCESS_KEY_ID}`),
    delete: readOnly ? undefined : () => askDelete([k.ACCESS_KEY_ID]),
  }))

  const add = (
    <LinkButton href={`${listHref}/new`} variant="primary" icon={PlusIcon} {...shortcutTarget('add')}>
      {t('Add access key')}
    </LinkButton>
  )

  return (
    <div className="grid gap-6">
      <PageHeader
        title={t('Access keys')}
        description={t('Keys that let scripts and integrations call the Hostisan API on your behalf.')}
        back={{ href: '/account', label: t('My account') }}
        actions={!readOnly && add}
      />

      <div className="grid gap-3">
        <TableToolbar
          search={{
            label: t('Search access keys'),
            placeholder: t('Search keys or comments'),
            value: table.query,
            onChange: table.setQuery,
          }}
          selectedCount={readOnly ? 0 : table.selectedCount}
          onClearSelection={table.clearSelection}
          bulkActions={
            <Button
              size="sm"
              variant="secondary-destructive"
              icon={TrashIcon}
              onClick={() => askDelete(table.selectedRows.map((k) => k.ACCESS_KEY_ID))}
            >
              {t('Delete')}
            </Button>
          }
        />

        {list.isPending ? (
          <DnsLoading />
        ) : list.isError ? (
          <DnsError error={list.error} />
        ) : (
          <TableCard
            rowShortcuts={rowKeys}
            noun={{ one: t('access key'), other: t('access keys') }}
            filteredCount={table.filteredCount}
            totalCount={keys.length}
            onClearSearch={() => table.setQuery('')}
            emptyState={{
              icon: <LockKeyIcon size={40} className="text-kumo-inactive" />,
              title: t('No access keys yet'),
              description: t('Create a key to use the API from billing systems, deploy scripts or a DNS cluster.'),
              action: !readOnly && add,
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
                      label={t('Select all access keys')}
                    />
                  )}
                  <SortableHead sortKey="key" sort={table.sort} onSort={table.toggleSort} sticky="left">
                    {t('Access key')}
                  </SortableHead>
                  <SortableHead sortKey="comment" sort={table.sort} onSort={table.toggleSort}>
                    {t('Comment')}
                  </SortableHead>
                  <Table.Head>{t('Permissions')}</Table.Head>
                  <SortableHead sortKey="date" sort={table.sort} onSort={table.toggleSort}>
                    {t('Date')}
                  </SortableHead>
                  <Table.Head>{t('Time')}</Table.Head>
                  <Table.Head sticky="right">
                    <span className="sr-only">{t('Actions')}</span>
                  </Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {table.rows.map((k) => (
                  <Table.Row
                    key={k.ACCESS_KEY_ID}
                    variant={table.isSelected(k.ACCESS_KEY_ID) ? 'selected' : 'default'}
                    {...rowLink(() => navigate(`${listHref}/${k.ACCESS_KEY_ID}`))}
                    {...rowKeys.rowProps(k.ACCESS_KEY_ID)}
                  >
                    {!readOnly && (
                      <Table.CheckCell
                        checked={table.isSelected(k.ACCESS_KEY_ID)}
                        onCheckedChange={(_, details) => table.toggle(k.ACCESS_KEY_ID, details)}
                        label={t('Select %s', k.ACCESS_KEY_ID)}
                      />
                    )}
                    <Table.Cell sticky="left">
                      <RouterLink
                        to={`${listHref}/${k.ACCESS_KEY_ID}`}
                        className="font-mono text-[0.9em] font-medium text-kumo-default hover:underline"
                      >
                        {k.ACCESS_KEY_ID}
                      </RouterLink>
                    </Table.Cell>
                    <Table.Cell>{k.COMMENT || <span className="text-kumo-subtle">-</span>}</Table.Cell>
                    <Table.Cell>
                      <PermissionBadges permissions={k.PERMISSIONS} />
                    </Table.Cell>
                    <Table.Cell className="tabular-nums">{k.DATE}</Table.Cell>
                    <Table.Cell className="tabular-nums text-kumo-subtle">{k.TIME}</Table.Cell>
                    <Table.Cell sticky="right" className="text-right">
                      <DropdownMenu>
                        <DropdownMenu.Trigger render={rowMenuTrigger(t('Actions for %s', k.ACCESS_KEY_ID))} />
                        <DropdownMenu.Content align="end">
                          <DropdownMenu.Item icon={EyeIcon} onClick={() => navigate(`${listHref}/${k.ACCESS_KEY_ID}`)}>
                            {t('View')}
                          </DropdownMenu.Item>
                          {!readOnly && (
                            <>
                              <DropdownMenu.Separator />
                              <DropdownMenu.Item
                                icon={TrashIcon}
                                variant="danger"
                                onClick={() => askDelete([k.ACCESS_KEY_ID])}
                              >
                                {t('Delete')}
                              </DropdownMenu.Item>
                            </>
                          )}
                        </DropdownMenu.Content>
                      </DropdownMenu>
                    </Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </Table>
          </TableCard>
        )}
      </div>

      <ListConfirmDialog confirm={confirm} />
    </div>
  )
}

/** Creating a key lands on the new key's page, where the secret is shown once. */
export function AccessKeyAddPage() {
  const navigate = useNavigate()
  const t = useT()
  const toast = useKumoToastManager()
  const readOnly = useSessionUser()?.readOnly ?? false
  const list = useAccessKeys()
  const client = useQueryClient()
  const [apis, setApis] = useState<string[]>([])
  const [comment, setComment] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const onSubmit = async (e: FormEvent, guard: FormGuard) => {
    e.preventDefault()
    setError('')
    setBusy(true)
    try {
      // Called directly, not through a mutation: a mutation keeps its result (the secret)
      // in the query client's cache for minutes.
      const created = await api.post<AccessKeyCreated>('/account/access-keys', { permissions: apis, comment })
      await client.invalidateQueries({ queryKey: accountKeys.account })
      // The secret travels in the navigation state only: never in the URL or a query.
      navigate(`${listHref}/${created.key.ACCESS_KEY_ID}`, { state: { secret: created.secret } })
      toast.add({
        title: braceMessage(t('Access key {%s} has been created successfully.'), created.key.ACCESS_KEY_ID, (id) => (
          <code className="font-mono text-[0.9em]">{id}</code>
        )),
        type: 'success',
      })
    } catch (err) {
      // A raw request, not a mutation: say the save failed.
      guard.rearm()
      setError(apiErrorText(err, t))
      setBusy(false)
    }
  }

  return (
    <FormPage
      title={t('Add access key')}
      description={t('The secret key is shown once, right after you create the key.')}
      back={{ href: listHref, label: t('Access keys') }}
      submitLabel={t('Add access key')}
      submitDisabled={apis.length === 0 || busy}
      readOnly={readOnly}
      onSubmit={onSubmit}
    >
      {error && (
        <Banner variant="error" icon={<WarningIcon weight="fill" />} title={error} className="whitespace-pre-line" />
      )}
      <FormSection
        title={t('Permissions')}
        description={t('The APIs this key may call. Each API allows a fixed set of commands.')}
      >
        {list.isPending ? (
          <DnsLoading />
        ) : list.isError ? (
          <DnsError error={list.error} />
        ) : list.data.apis.length === 0 ? (
          <Text variant="secondary">{t('No APIs available')}</Text>
        ) : (
          <fieldset className="grid gap-3">
            <legend className="sr-only">{t('Permissions')}</legend>
            {list.data.apis.map((api) => (
              <Checkbox
                key={api.Name}
                name="v_apis[]"
                checked={apis.includes(api.Name)}
                onCheckedChange={(c) =>
                  setApis((prev) => (c ? [...prev, api.Name] : prev.filter((a) => a !== api.Name)))
                }
                label={
                  <span className="grid gap-0.5">
                    <span className="flex items-center gap-2">
                      {api.Name}
                      {api.ROLE === 'admin' && <Badge variant="purple">{t('Admin')}</Badge>}
                    </span>
                    <span className="text-sm text-kumo-subtle">
                      {api.COMMANDS.slice(0, 3).join(', ')}
                      {api.COMMANDS.length > 3 && ` ${t('and %s more', api.COMMANDS.length - 3)}`}
                    </span>
                  </span>
                }
              />
            ))}
          </fieldset>
        )}
      </FormSection>
      <FormSection title={t('Comment')} description={t('Helps you recognise the key later.')}>
        <Input
          label={t('Comment')}
          name="v_comment"
          maxLength={255}
          value={comment}
          onChange={(e) => setComment(e.target.value)}
          placeholder="WHMCS billing integration"
          required={false}
        />
      </FormSection>
    </FormPage>
  )
}

export function AccessKeyPage() {
  const { key = '' } = useParams()
  const location = useLocation()
  const navigate = useNavigate()
  const t = useT()
  const toast = useKumoToastManager()
  const readOnly = useSessionUser()?.readOnly ?? false
  const one = useAccessKey(key)
  const apis = useAccessKeys().data?.apis ?? []
  const m = useKeyMutation()
  const [confirmOpen, setConfirmOpen] = useState(false)
  // The secret came with the create reply. It lives in this component only, and the
  // history entry is cleared so a reload or a later visit never shows it.
  const [secret] = useState(() => (location.state as { secret?: string } | null)?.secret ?? '')
  useEffect(() => {
    if (secret) navigate(location.pathname, { replace: true, state: null })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  if (one.isPending) return <DnsLoading />
  if (one.isError) {
    if (one.error instanceof ApiError && one.error.status === 404) {
      return (
        <div className="grid gap-6">
          <PageHeader title={t('Access key not found')} back={{ href: listHref, label: t('Access keys') }} />
          <Empty
            icon={<LockKeyIcon size={32} />}
            title={t("There's no access key “%s”", key)}
            description={t('It may have been deleted.')}
            contents={<LinkButton href={listHref}>{t('Back to access keys')}</LinkButton>}
          />
        </div>
      )
    }
    return <DnsError error={one.error} />
  }
  const data = one.data

  const rows: [string, React.ReactNode][] = [
    [t('User'), data.USER],
    [t('Comment'), data.COMMENT || <span className="text-kumo-subtle">{t('No comment')}</span>],
    [t('Created'), `${data.DATE} ${data.TIME}`],
  ]

  return (
    <div className="grid gap-8">
      <PageHeader
        title={t('Access key')}
        description={data.COMMENT || t('API credentials for scripts and integrations.')}
        back={{ href: listHref, label: t('Access keys') }}
        actions={
          !readOnly && (
            <Button variant="secondary" icon={TrashIcon} onClick={() => setConfirmOpen(true)}>
              {t('Delete')}
            </Button>
          )
        }
      />

      <FormSection title={t('Credentials')} description={t('Send both values with every API request.')}>
        <div className="grid gap-1.5">
          <Text bold>{t('Access key ID')}</Text>
          <ClipboardText text={data.ACCESS_KEY_ID} size="base" />
        </div>
        {secret ? (
          <div className="grid gap-3">
            <Banner
              variant="alert"
              icon={<WarningIcon weight="fill" />}
              title={t('Warning! Last chance to save secret key!')}
              description={t("Copy it now and store it somewhere safe. You won't be able to see it again after leaving this page.")}
            />
            <div className="grid gap-1.5">
              <Text bold>{t('Secret key')}</Text>
              <ClipboardText text={secret} size="base" />
            </div>
          </div>
        ) : (
          <div className="grid gap-1">
            <Text bold>{t('Secret key')}</Text>
            <Text variant="secondary">{t('Hidden. Secret keys are only shown once, when the key is created.')}</Text>
          </div>
        )}
      </FormSection>

      <FormSection title={t('Permissions')} description={t('APIs this key may call.')}>
        <ul className="grid gap-3">
          {data.PERMISSIONS.map((p) => {
            const api = apis.find((a) => a.Name === p)
            return (
              <li key={p} className="grid gap-1">
                <div className="flex items-center gap-2">
                  <Text bold>{p}</Text>
                  {api?.ROLE === 'admin' && <Badge variant="purple">{t('Admin')}</Badge>}
                </div>
                {api && (
                  <Text variant="secondary" size="sm">
                    {api.COMMANDS.join(', ')}
                  </Text>
                )}
              </li>
            )
          })}
          {data.PERMISSIONS.length === 0 && <Text variant="secondary">{t('No permissions')}</Text>}
        </ul>
      </FormSection>

      <FormSection title={t('Details')}>
        <dl className="grid gap-3">
          {rows.map(([k, v]) => (
            <div key={k} className="grid gap-0.5 sm:grid-cols-[8rem_1fr]">
              <dt className="text-kumo-subtle">{k}</dt>
              <dd>{v}</dd>
            </div>
          ))}
        </dl>
      </FormSection>

      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={t('Delete access key')}
        description={t('Are you sure you want to delete access key %s?', data.ACCESS_KEY_ID)}
        confirmLabel={t('Delete')}
        destructive
        onConfirm={() =>
          m.removeAccessKey(data.ACCESS_KEY_ID).then(
            () => {
              toast.add({ title: t('Access key deleted'), type: 'success' })
              navigate(listHref)
            },
            (err) => toast.add({ title: t('Delete access key'), description: apiErrorText(err, t), type: 'error' }),
          )
        }
      />
    </div>
  )
}
