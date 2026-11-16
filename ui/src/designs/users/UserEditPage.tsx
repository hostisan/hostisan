import { Banner, Button, Empty, LinkButton, useKumoToastManager } from '@cloudflare/kumo'
import { ClockCounterClockwiseIcon, KeyIcon, LockKeyIcon, PauseCircleIcon, UserIcon } from '@phosphor-icons/react'
import { useState } from 'react'
import { useParams } from 'react-router'
import { useAccountOptions, useUser } from '../../api/account'
import { apiErrorText } from '../../api/errors'
import { useUserMutation } from '../../api/users'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { FormPage } from '../../components/FormPage'
import { PageHeader } from '../../components/PageHeader'
import { useT } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { DnsError, DnsLoading } from '../dns/shared'
import { UserForm } from './UserForm'

const back = { href: '/users', label: msgid('Users') }

export function UserAddPage() {
  const t = useT()
  const options = useAccountOptions()
  const backLink = { ...back, label: t(back.label) }
  if (options.error || !options.data) {
    return (
      <FormPage title={t('Add user')} back={backLink} readOnly>
        {options.error ? <DnsError error={options.error} /> : <DnsLoading />}
      </FormPage>
    )
  }
  return (
    <UserForm
      mode="add"
      options={options.data}
      title={t('Add user')}
      description={t('Create a hosting account and assign it a package.')}
      back={backLink}
    />
  )
}

/** Links Hestia shows in the edit user toolbar. */
export function UserToolLinks({ logs = true, accessKeys = true }: { logs?: boolean; accessKeys?: boolean }) {
  const t = useT()
  return (
    <>
      <LinkButton href="/account/ssh-keys" variant="secondary" icon={KeyIcon}>
        {t('Manage SSH keys')}
      </LinkButton>
      {logs && (
        <LinkButton href="/logs" variant="secondary" icon={ClockCounterClockwiseIcon}>
          {t('Logs')}
        </LinkButton>
      )}
      {accessKeys && (
        <LinkButton href="/account/access-keys" variant="secondary" icon={LockKeyIcon}>
          {t('Access keys')}
        </LinkButton>
      )}
    </>
  )
}

export function UserEditPage() {
  const { user: username = '' } = useParams()
  const t = useT()
  const view = useUser(username)
  const options = useAccountOptions()
  const users = useUserMutation()
  const toast = useKumoToastManager()
  const [confirmOpen, setConfirmOpen] = useState(false)
  const backLink = { ...back, label: t(back.label) }

  const failed = view.error ?? options.error
  if (failed || !view.data || !options.data) {
    const missing = failed && (failed as { status?: number }).status === 404
    if (missing) {
      return (
        <div className="grid gap-6">
          <PageHeader title={t('User not found')} back={backLink} />
          <Empty
            icon={<UserIcon size={32} />}
            title={t('There\'s no user named “%s”', username)}
            description={t('It may have been deleted. Go back to the user list to pick another account.')}
            contents={<LinkButton href="/users">{t('Back to users')}</LinkButton>}
          />
        </div>
      )
    }
    return (
      <FormPage title={t('Edit user %s', username)} back={backLink} readOnly>
        {failed ? <DnsError error={failed} /> : <DnsLoading />}
      </FormPage>
    )
  }
  const user = view.data.user
  const suspended = user.SUSPENDED === 'yes'

  const unsuspend = async () => {
    try {
      await users.unsuspend(username)
      toast.add({ title: t('%s unsuspended', username), type: 'success' })
    } catch (err) {
      toast.add({ title: apiErrorText(err, t), type: 'error' })
    }
  }

  return (
    <>
      <UserForm
        key={username}
        mode="edit"
        options={options.data}
        view={view.data}
        title={t('Edit user %s', username)}
        description={t('%s · created %s', user.NAME, user.DATE)}
        back={backLink}
        banner={
          suspended && (
            <Banner
              variant="alert"
              icon={<PauseCircleIcon weight="fill" />}
              title={t('This user is suspended')}
              description={t('Their websites, mail, databases and cron jobs are disabled until the account is unsuspended.')}
              action={
                <Button size="sm" type="button" onClick={() => setConfirmOpen(true)}>
                  {t('Unsuspend')}
                </Button>
              }
            />
          )
        }
      />
      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={t('Unsuspend user')}
        description={t('Are you sure you want to unsuspend user %s?', username)}
        confirmLabel={t('Unsuspend')}
        onConfirm={() => void unsuspend()}
      />
    </>
  )
}
