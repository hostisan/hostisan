import { Banner, Button, InputArea, LinkButton, Table, Text, useKumoToastManager } from '@cloudflare/kumo'
import { KeyIcon, PlusIcon, TrashIcon, WarningIcon } from '@phosphor-icons/react'
import { useState, type FormEvent } from 'react'
import { useKeyMutation, useSshKeys } from '../../api/account'
import { apiErrorText } from '../../api/errors'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { FormPage } from '../../components/FormPage'
import { FormSection } from '../../components/FormSection'
import { PageHeader } from '../../components/PageHeader'
import { TableCard } from '../../components/table'
import { useT } from '../../i18n/I18nProvider'
import { useSessionUser } from '../../session/SessionProvider'
import { DnsError, DnsLoading } from '../dns/shared'
import { interpolate } from '../../lib/interpolate'
import { shortcutTarget } from '../../shortcuts/targets'
import { useRowShortcuts } from '../../shortcuts/useRowShortcuts'

export function SshKeysPage() {
  const toast = useKumoToastManager()
  const t = useT()
  const readOnly = useSessionUser()?.readOnly ?? false
  const list = useSshKeys()
  const m = useKeyMutation()
  const keys = list.data?.keys ?? []
  const [toDelete, setToDelete] = useState('')
  const [confirmOpen, setConfirmOpen] = useState(false)
  const askDelete = (id: string) => {
    setToDelete(id)
    setConfirmOpen(true)
  }
  // An SSH key has no page of its own: Delete is its one row key.
  const rowKeys = useRowShortcuts(keys, (k) => k.ID, (k) => ({
    delete: readOnly ? undefined : () => askDelete(k.ID),
  }))
  const add = (
    <LinkButton href="/account/ssh-keys/new" variant="primary" icon={PlusIcon} {...shortcutTarget('add')}>
      {t('Add SSH key')}
    </LinkButton>
  )

  return (
    <div className="grid gap-6">
      <PageHeader
        title={t('SSH keys')}
        description={t('Public keys allowed to sign in over SSH and SFTP as this user.')}
        back={{ href: '/account', label: t('My account') }}
        actions={!readOnly && add}
      />

      {list.isPending ? (
        <DnsLoading />
      ) : list.isError ? (
        <DnsError error={list.error} />
      ) : (
        <TableCard
          rowShortcuts={rowKeys}
          noun={keys.length === 1 ? t('SSH key') : t('SSH keys')}
          filteredCount={keys.length}
          totalCount={keys.length}
          emptyState={{
            icon: <KeyIcon size={40} className="text-kumo-inactive" />,
            title: t('No SSH keys yet'),
            description: t('Add a public key to sign in over SSH without a password.'),
            action: !readOnly && add,
          }}
        >
          <Table className="whitespace-nowrap">
            <Table.Header>
              <Table.Row>
                <Table.Head sticky="left">{t('SSH ID')}</Table.Head>
                <Table.Head>{t('Fingerprint')}</Table.Head>
                {!readOnly && (
                  <Table.Head sticky="right" className="w-px">
                    <span className="sr-only">{t('Actions')}</span>
                  </Table.Head>
                )}
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {keys.map((k) => (
                <Table.Row key={k.ID} {...rowKeys.rowProps(k.ID)}>
                  <Table.Cell sticky="left">
                    <Text bold as="span">
                      {k.ID}
                    </Text>
                  </Table.Cell>
                  <Table.Cell>
                    <Text variant="mono-secondary" as="span">
                      {k.KEY}
                    </Text>
                  </Table.Cell>
                  {!readOnly && (
                    <Table.Cell sticky="right" className="text-right">
                      <Button
                        variant="ghost"
                        size="sm"
                        shape="square"
                        icon={<TrashIcon size={18} />}
                        aria-label={t('Delete %s', k.ID)}
                        onClick={() => askDelete(k.ID)}
                      />
                    </Table.Cell>
                  )}
                </Table.Row>
              ))}
            </Table.Body>
          </Table>
        </TableCard>
      )}

      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={t('Delete SSH key')}
        description={`${t('Are you sure you want to delete SSH key %s?', toDelete)} ${t('Anyone using it can no longer sign in.')}`}
        confirmLabel={t('Delete')}
        destructive
        onConfirm={() =>
          m.removeSshKey(toDelete).then(
            () => toast.add({ title: t('SSH key %s deleted', toDelete), type: 'success' }),
            (err) => toast.add({ title: t('Delete SSH key'), description: apiErrorText(err, t), type: 'error' }),
          )
        }
      />
    </div>
  )
}

export function SshKeyAddPage() {
  const toast = useKumoToastManager()
  const t = useT()
  const readOnly = useSessionUser()?.readOnly ?? false
  const m = useKeyMutation()
  const [key, setKey] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    setBusy(true)
    try {
      await m.addSshKey({ key })
      // The form is emptied, unlike Hestia's, so a second click can't add it twice.
      setKey('')
      toast.add({ title: t('SSH Key has been created successfully.'), type: 'success' })
    } catch (err) {
      setError(apiErrorText(err, t))
    } finally {
      setBusy(false)
    }
  }

  return (
    <FormPage
      title={t('Add SSH key')}
      description={t('Paste a public key. Never paste your private key.')}
      back={{ href: '/account/ssh-keys', label: t('SSH keys') }}
      submitLabel={t('Add SSH key')}
      readOnly={readOnly}
      submitDisabled={busy || key.trim() === ''}
      onSubmit={onSubmit}
    >
      {error && <Banner variant="error" icon={<WarningIcon weight="fill" />} title={error} className="whitespace-pre-line" />}
      <FormSection
        title={t('Public key')}
        description={interpolate(
          t("Usually found in %s. The comment at the end becomes the key's ID."),
          <span className="font-mono text-[0.9em]">~/.ssh/id_ed25519.pub</span>,
        )}
      >
        <InputArea
          label={t('SSH key')}
          name="v_key"
          required
          rows={10}
          value={key}
          onValueChange={setKey}
          className="font-mono"
          placeholder="ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA… ops@acme-hosting.net"
          description={t('Supported types: %s.', 'ssh-ed25519, ecdsa-sha2-nistp256, ssh-rsa')}
        />
      </FormSection>
    </FormPage>
  )
}
