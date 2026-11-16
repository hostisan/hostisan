import { useAccount, useAccountOptions } from '../../api/account'
import { FormPage } from '../../components/FormPage'
import { useT } from '../../i18n/I18nProvider'
import { useSessionUser } from '../../session/SessionProvider'
import { DnsError, DnsLoading } from '../dns/shared'
import { UserToolLinks } from '../users/UserEditPage'
import { UserForm } from '../users/UserForm'

export function AccountPage() {
  const t = useT()
  const heading = {
    title: t('My account'),
    description: t('Your contact details, password, sign-in security and panel preferences.'),
  }
  const view = useAccount()
  const options = useAccountOptions()
  const user = useSessionUser()
  const back = { href: user?.landing || '/web', label: t('Dashboard') }
  const failed = view.error ?? options.error
  if (failed || !view.data || !options.data) {
    return (
      <FormPage {...heading} back={back} readOnly>
        {failed ? <DnsError error={failed} /> : <DnsLoading />}
      </FormPage>
    )
  }
  return (
    <UserForm
      key={view.data.user.USER}
      mode="account"
      options={options.data}
      view={view.data}
      {...heading}
      back={back}
      actions={<UserToolLinks logs={options.data.logsVisible} accessKeys={options.data.apiAccess} />}
    />
  )
}
