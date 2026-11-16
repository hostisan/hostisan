import { Banner, Button, Input, Select, useKumoToastManager } from '@cloudflare/kumo'
import { LockSimpleIcon, TrashIcon } from '@phosphor-icons/react'
import { useMemo, useState, type FormEvent } from 'react'
import { useNavigate, useParams } from 'react-router'
import { useFirewallRule, useIpsets, useServerMutation } from '../../api/server'
import type { FirewallRuleRow } from '../../api/types.gen'
import { Optional } from '../../components/fields'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { FormPage } from '../../components/FormPage'
import { FormSection } from '../../components/FormSection'
import { useT } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { DnsError, DnsLoading } from '../dns/shared'
import { ruleComment, useRuleError } from './firewallShared'
import { ErrorNote } from './parts'

const backHref = '/server/firewall'

export function FirewallRuleFormPage({ mode }: { mode: 'add' | 'edit' }) {
  const { rule = '' } = useParams()
  const row = useFirewallRule(mode === 'edit' ? rule : '')
  const ipsets = useIpsets()
  const [error, setError] = useState('')
  const t = useT()
  const back = { href: backHref, label: t('Firewall') }
  const title = mode === 'add' ? t('Add firewall rule') : t('Edit firewall rule #%s', rule)
  if (row.error || (mode === 'edit' && !row.data)) {
    return (
      <FormPage title={title} back={back} readOnly>
        {row.error ? <DnsError error={row.error} /> : <DnsLoading />}
      </FormPage>
    )
  }
  // Only IPv4 lists that are not suspended can back a rule (the core's own helper list)
  const lists = (ipsets.data?.rows ?? [])
    .filter((l) => l.SUSPENDED !== 'yes' && l.IP_VERSION !== 'v6')
    .map((l) => l.LISTNAME)
    .sort((a, b) => a.localeCompare(b))
  // A refetched rule (after a stale save) starts the form over with the fresh values
  return (
    <RuleForm
      key={row.data?.expect ?? 'new'}
      mode={mode}
      rule={row.data}
      ruleId={rule}
      lists={lists}
      error={error}
      setError={setError}
    />
  )
}

function RuleForm({
  mode,
  rule,
  ruleId,
  lists,
  error,
  setError,
}: {
  mode: 'add' | 'edit'
  rule?: FirewallRuleRow
  ruleId: string
  lists: string[]
  error: string
  setError: (text: string) => void
}) {
  const t = useT()
  const m = useServerMutation()
  const toast = useKumoToastManager()
  const navigate = useNavigate()
  const ruleError = useRuleError()
  const back = { href: backHref, label: t('Firewall') }
  const locked = (rule?.protected ?? '') !== ''

  const [action, setAction] = useState(rule?.action ?? 'DROP')
  const [protocol, setProtocol] = useState(rule?.protocol ?? 'TCP')
  const [port, setPort] = useState(rule?.port ?? '')
  const [ip, setIp] = useState(rule?.ip ?? '')
  const [comment, setComment] = useState(rule ? ruleComment(rule.comment) : '')
  const [busy, setBusy] = useState(false)
  const [deleteOpen, setDeleteOpen] = useState(false)

  const ipsetItems = useMemo(
    () => ({ '': t('Clear'), ...Object.fromEntries(lists.map((n) => [`ipset:${n}`, n])) }),
    [lists, t],
  )

  const finish = async (call: () => Promise<unknown>, message: string, ...args: string[]) => {
    setError('')
    setBusy(true)
    try {
      await call()
      toast.add({ title: t(message, ...args), type: 'success' })
      navigate(back.href)
    } catch (err) {
      setError(ruleError(err))
      setBusy(false)
    }
  }
  const onSubmit = (e: FormEvent) => {
    void e
    const body = { action, protocol, port: port.trim(), ip: ip.trim(), comment: comment.trim() }
    if (mode === 'add') return finish(() => m.addRule(body), msgid('Rule has been created successfully.'))
    return finish(() => m.editRule(ruleId, { ...body, expect: rule?.expect }), msgid('Changes have been saved.'))
  }

  return (
    <FormPage
      title={mode === 'add' ? t('Add firewall rule') : t('Edit firewall rule #%s', ruleId)}
      description={mode === 'add' ? t('New rules are added to the end of the list.') : ruleComment(rule?.comment ?? '')}
      back={back}
      submitLabel={mode === 'add' ? t('Add rule') : t('Save')}
      onSubmit={onSubmit}
      submitDisabled={busy}
      readOnly={locked}
      actions={
        mode === 'edit' &&
        !locked && (
          <Button
            type="button"
            variant="secondary-destructive"
            icon={TrashIcon}
            disabled={busy}
            onClick={() => setDeleteOpen(true)}
          >
            {t('Delete')}
          </Button>
        )
      }
    >
      {locked && (
        <Banner
          variant="alert"
          icon={<LockSimpleIcon weight="fill" />}
          title={
            rule?.protected === 'ssh' ? t('This rule keeps SSH reachable') : t('This rule keeps the panel reachable')
          }
          description={t("It can't be changed, suspended, moved or deleted from here. Use SSH if you really need to.")}
        />
      )}
      {error && <ErrorNote error={error} />}

      <FormSection title={t('Rule')} description={t('Which traffic this rule matches and what happens to it.')}>
        <div className="grid gap-4 sm:grid-cols-2">
          <Select
            label={t('Action')}
            value={action}
            onValueChange={(v) => setAction(String(v))}
            items={{ DROP: 'DROP', ACCEPT: 'ACCEPT' }}
            disabled={locked}
            className="w-full"
          />
          <Select
            label={t('Protocol')}
            value={protocol}
            onValueChange={(v) => setProtocol(String(v))}
            items={{ TCP: 'TCP', UDP: 'UDP', ICMP: 'ICMP' }}
            disabled={locked}
            className="w-full"
          />
        </div>
        <Input
          label={t('Port')}
          description={t('Ranges and lists are acceptable')}
          value={port}
          onChange={(e) => setPort(e.target.value)}
          disabled={locked}
          required
          placeholder={t('All ports: 0, Range: 80-82, List: 80,443,8080,8443')}
          className="font-mono"
        />
        <div className="grid gap-2">
          <Input
            label={t('IP address / IP list')}
            description={t('Support CIDR format')}
            value={ip}
            onChange={(e) => setIp(e.target.value)}
            disabled={locked}
            required
            placeholder="0.0.0.0/0"
            className="font-mono"
          />
          {lists.length > 0 && !locked && (
            <Select
              aria-label={t('Use an IP list')}
              placeholder={t('Use an IP list')}
              value={ip.startsWith('ipset:') ? ip : null}
              onValueChange={(v) => setIp(String(v ?? ''))}
              items={ipsetItems}
              className="w-full sm:w-64"
            />
          )}
        </div>
        <Input
          label={
            <>
              {t('Comment')} <Optional />
            </>
          }
          value={comment}
          onChange={(e) => setComment(e.target.value)}
          disabled={locked}
          maxLength={255}
        />
      </FormSection>

      <ConfirmDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        title={t('Delete rule?')}
        description={t('Are you sure you want to delete rule #%s?', ruleId)}
        confirmLabel={t('Delete')}
        destructive
        onConfirm={() =>
          void finish(
            () => m.ruleAction(ruleId, 'delete', { expect: rule?.expect ?? '' }),
            msgid('Rule #%s deleted'),
            ruleId,
          )
        }
      />
    </FormPage>
  )
}
