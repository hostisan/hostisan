import { Banner, Button, ClipboardText, Dialog, Input, Loader, Text, useKumoToastManager } from '@cloudflare/kumo'
import { WarningIcon } from '@phosphor-icons/react'
import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useAccountMutation } from '../../api/account'
import { apiErrorText } from '../../api/errors'
import type { TwoFASetup } from '../../api/types.gen'
import { PasswordInput } from '../../components/PasswordInput'
import { useT } from '../../i18n/I18nProvider'

function ErrorLine({ text }: { text: string }) {
  if (!text) return null
  return <Banner variant="error" icon={<WarningIcon weight="fill" />} title={text} className="whitespace-pre-line" />
}

/**
 * Turns two-factor authentication on for your own account. Opening it asks the server
 * for a new secret; nothing is saved until you enter a valid code and your password.
 * Always render it and toggle `open`.
 */
export function TwoFactorDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const t = useT()
  const m = useAccountMutation()
  const toast = useKumoToastManager()
  const [setup, setSetup] = useState<TwoFASetup | null>(null)
  const [code, setCode] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const started = useRef(false)

  useEffect(() => {
    if (!open) {
      started.current = false
      setSetup(null)
      setCode('')
      setPassword('')
      setError('')
      return
    }
    if (started.current) return
    started.current = true
    m.twofaSetup().then(setSetup, (err) => setError(apiErrorText(err, t)))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const confirm = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    setBusy(true)
    try {
      await m.twofaConfirm({ code: code.trim(), currentPassword: password })
      toast.add({ title: t('Two-factor authentication enabled.'), type: 'success' })
      onOpenChange(false)
    } catch (err) {
      setError(apiErrorText(err, t))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog className="grid gap-4 px-6 py-5">
        <div className="grid gap-1.5">
          <Dialog.Title className="text-lg font-semibold">{t('Set up two-factor authentication')}</Dialog.Title>
          <Dialog.Description className="text-kumo-subtle">
            {t(
              'Scan the code with your authenticator app, then enter the 6-digit code it shows. Two-factor authentication is only turned on once the code is confirmed.',
            )}
          </Dialog.Description>
        </div>
        <form onSubmit={confirm} className="grid gap-4">
          {!setup && !error && (
            <div className="flex justify-center py-6">
              <Loader />
            </div>
          )}
          {setup && (
            <>
              <div className="flex justify-center">
                <img
                  src={setup.qr}
                  alt={t('2FA QR code')}
                  width={176}
                  height={176}
                  className="rounded-lg bg-white p-2 ring ring-kumo-line"
                />
              </div>
              <div className="grid gap-1.5">
                <Text variant="secondary" size="sm">
                  {t('Or enter this secret key by hand')}
                </Text>
                <ClipboardText text={setup.secret} size="base" />
              </div>
              <Input
                label={t('Code from your app')}
                inputMode="numeric"
                autoComplete="one-time-code"
                maxLength={6}
                value={code}
                onChange={(e) => setCode(e.target.value.replace(/\D/g, ''))}
                required
              />
              <PasswordInput
                label={t('Current password')}
                isNew={false}
                value={password}
                onChange={setPassword}
                required
              />
            </>
          )}
          <ErrorLine text={error} />
          <div className="flex justify-end gap-2">
            <Dialog.Close render={(props) => <Button {...props}>{t('Cancel')}</Button>} />
            <Button type="submit" variant="primary" disabled={!setup || busy || code.length !== 6 || password === ''}>
              {t('Turn on')}
            </Button>
          </div>
        </form>
      </Dialog>
    </Dialog.Root>
  )
}

/**
 * Turns two-factor authentication off. Your own account needs the current password
 * (`needsPassword`); an administrator turning off another user's does not.
 */
export function TwoFactorOffDialog({
  open,
  onOpenChange,
  username,
  needsPassword,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** The account whose 2FA is turned off; used for the admin route. */
  username: string
  needsPassword: boolean
}) {
  const t = useT()
  const m = useAccountMutation()
  const toast = useKumoToastManager()
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!open) {
      setPassword('')
      setError('')
    }
  }, [open])

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    setBusy(true)
    try {
      if (needsPassword) await m.twofaDisable({ currentPassword: password })
      else await m.userTwofaDisable(username)
      toast.add({ title: t('Two-factor authentication disabled.'), type: 'success' })
      onOpenChange(false)
    } catch (err) {
      setError(apiErrorText(err, t))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog className="grid gap-4 px-6 py-5">
        <div className="grid gap-1.5">
          <Dialog.Title className="text-lg font-semibold">{t('Turn off two-factor authentication')}</Dialog.Title>
          <Dialog.Description className="text-kumo-subtle">
            {needsPassword
              ? t('Enter your current password to confirm. Signing in will need only your password.')
              : t('Signing in as %s will need only a password.', username)}
          </Dialog.Description>
        </div>
        <form onSubmit={submit} className="grid gap-4">
          {needsPassword && (
            <PasswordInput label={t('Current password')} isNew={false} value={password} onChange={setPassword} required />
          )}
          <ErrorLine text={error} />
          <div className="flex justify-end gap-2">
            <Dialog.Close render={(props) => <Button {...props}>{t('Cancel')}</Button>} />
            <Button type="submit" variant="destructive" disabled={busy || (needsPassword && password === '')}>
              {t('Turn off')}
            </Button>
          </div>
        </form>
      </Dialog>
    </Dialog.Root>
  )
}
