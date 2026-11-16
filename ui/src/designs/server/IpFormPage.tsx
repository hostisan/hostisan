import { Button, Checkbox, Input, Select, Text, useKumoToastManager } from '@cloudflare/kumo'
import { TrashIcon } from '@phosphor-icons/react'
import { useRef, useState, type FormEvent, type ReactNode } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { useIp, useIpOptions, useServerMutation } from '../../api/server'
import type { IPOptions, IPRow, JobRef, JobStatus } from '../../api/types.gen'
import { Optional } from '../../components/fields'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { FormPage } from '../../components/FormPage'
import type { FormGuard } from '../../unsaved/types'
import { FormSection } from '../../components/FormSection'
import { useT, type Translate } from '../../i18n/I18nProvider'
import { DnsError, DnsLoading } from '../dns/shared'
import { ErrorNote, JobPanel } from './parts'

const backHref = '/server/ip'

/** The create-success message: its {...} links to the edit page. */
function createdMessage(t: Translate, ip: string): ReactNode {
  const parts = t('IP address {%s} has been created successfully.').split(/(\{[^}]*\})/)
  return (
    <>
      {parts.map((part, i) =>
        /^\{[^}]*\}$/.test(part) ? (
          <Link key={i} to={`/server/ip/${encodeURIComponent(ip)}`} className="underline">
            {ip}
          </Link>
        ) : (
          part
        ),
      )}
    </>
  )
}

export function IpFormPage({ mode }: { mode: 'add' | 'edit' }) {
  const params = useParams()
  const ip = mode === 'edit' ? decodeURIComponent(params.ip ?? '') : ''
  const options = useIpOptions()
  const row = useIp(ip)
  const t = useT()
  const title = mode === 'add' ? t('Add IP address') : t('Edit IP address')
  const back = { href: backHref, label: t('IP addresses') }
  const failed = options.error ?? row.error
  if (failed || !options.data || (mode === 'edit' && !row.data)) {
    return (
      <FormPage title={title} back={back} readOnly>
        {failed ? <DnsError error={failed} /> : <DnsLoading />}
      </FormPage>
    )
  }
  return <IpForm mode={mode} options={options.data} row={row.data} />
}

function IpForm({ mode, options, row }: { mode: 'add' | 'edit'; options: IPOptions; row?: IPRow }) {
  const t = useT()
  const m = useServerMutation()
  const toast = useKumoToastManager()
  const navigate = useNavigate()
  const back = { href: backHref, label: t('IP addresses') }

  const [ip, setIp] = useState(row?.ip ?? '')
  const [netmask, setNetmask] = useState(row?.netmask ?? '255.255.255.0')
  const [iface, setIface] = useState(row?.interface ?? options.interfaces[0] ?? '')
  const [shared, setShared] = useState(row ? row.status === 'shared' : true)
  const [owner, setOwner] = useState(row?.owner || options.rootUser)
  const [name, setName] = useState(row?.name ?? '')
  const [nat, setNat] = useState(row?.nat ?? '')
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)
  const [jobId, setJobId] = useState('')
  const [deleteOpen, setDeleteOpen] = useState(false)
  // What to do when the running job finishes successfully
  const [after, setAfter] = useState<() => void>(() => () => {})

  const body = {
    ip,
    netmask,
    interface: iface,
    shared,
    // A shared address belongs to the root user (decision 45)
    owner: shared ? options.rootUser : owner,
    name: name.trim(),
    nat: nat.trim(),
  }

  // The guard of the save in progress: a job that fails re-arms it.
  const saving = useRef<FormGuard | null>(null)
  const follow = (call: () => Promise<JobRef>, done: () => void) => {
    setError(null)
    setBusy(true)
    call().then(
      (ref) => {
        setAfter(() => done)
        setJobId(ref.jobId)
      },
      (err) => {
        setError(err)
        setBusy(false)
      },
    )
  }
  const onJobDone = (status: JobStatus) => {
    setBusy(false)
    if (status.state === 'done') after()
    else saving.current?.rearm()
  }

  const onSubmit = async (e: FormEvent, guard: FormGuard) => {
    void e
    saving.current = guard
    if (mode === 'add') {
      follow(
        () => m.addIp(body),
        () => {
          toast.add({ title: createdMessage(t, body.ip), type: 'success' })
          navigate(back.href)
        },
      )
      return
    }
    setError(null)
    setBusy(true)
    try {
      await m.editIp(row?.ip ?? ip, body)
      toast.add({ title: t('Changes have been saved.'), type: 'success' })
      navigate(back.href)
    } catch (err) {
      setError(err)
      setBusy(false)
    }
  }

  return (
    <FormPage
      title={mode === 'add' ? t('Add IP address') : t('Edit IP address')}
      description={
        mode === 'edit' ? (
          <span className="font-mono text-[0.9em]">{row?.ip}</span>
        ) : (
          t('Bring up a new address on a network interface.')
        )
      }
      back={back}
      submitLabel={mode === 'add' ? t('Add IP address') : t('Save')}
      onSubmit={onSubmit}
      submitDisabled={busy}
      actions={
        mode === 'edit' && (
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
      <ErrorNote error={error} />
      <JobPanel jobId={jobId} onDone={onJobDone} />

      <FormSection
        title={t('Address')}
        description={
          mode === 'edit' ? t("The address and interface can't be changed. Delete and re-add it instead.") : undefined
        }
      >
        <Input
          label={t('IP address')}
          value={ip}
          onChange={(e) => setIp(e.target.value)}
          disabled={mode === 'edit'}
          placeholder="203.0.113.26"
          className="font-mono"
          required
        />
        <div className="grid gap-4 sm:grid-cols-2">
          <Input
            label={t('Netmask')}
            value={netmask}
            onChange={(e) => setNetmask(e.target.value)}
            disabled={mode === 'edit'}
            className="font-mono"
            required
          />
          {mode === 'add' ? (
            <Select
              label={t('Interface')}
              value={iface}
              onValueChange={(v) => setIface(String(v))}
              items={Object.fromEntries(options.interfaces.map((i) => [i, i]))}
              className="w-full"
            />
          ) : (
            <Input label={t('Interface')} value={iface} disabled className="font-mono" />
          )}
        </div>
      </FormSection>

      <FormSection
        title={t('Assignment')}
        description={t('Shared addresses can be used by every user. A dedicated address belongs to one user.')}
      >
        <Checkbox label={t('Shared')} checked={shared} onCheckedChange={(v) => setShared(v === true)} />
        {!shared && (
          <Select
            label={t('Assigned user')}
            value={owner}
            onValueChange={(v) => setOwner(String(v))}
            items={Object.fromEntries(options.users.map((u) => [u, u]))}
            className="w-full"
          />
        )}
        <Input
          label={
            <>
              {t('Assigned domain')} <Optional />
            </>
          }
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="acme-bakery.com"
        />
        <Input
          label={
            <>
              {t('NAT IP association')} <Optional />
            </>
          }
          value={nat}
          onChange={(e) => setNat(e.target.value)}
          placeholder="10.0.0.26"
          className="font-mono"
          description={t('The public address when this server sits behind NAT.')}
        />
        {mode === 'edit' && row && row.users && (
          <Text variant="secondary" size="sm">
            {row.webDomains === '1'
              ? t('Used by %s on 1 web domain.', row.users.split(',').join(', '))
              : t('Used by %s on %s web domains.', row.users.split(',').join(', '), row.webDomains)}
          </Text>
        )}
      </FormSection>

      <ConfirmDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        title={t('Delete IP address?')}
        description={t('Are you sure you want to delete IP address %s?', row?.ip ?? ip)}
        confirmLabel={t('Delete')}
        destructive
        onConfirm={() =>
          follow(
            () => m.deleteIp(row?.ip ?? ip),
            () => navigate(back.href),
          )
        }
      />
    </FormPage>
  )
}
