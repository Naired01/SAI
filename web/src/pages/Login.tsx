import { useState } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../lib/auth'
import { ApiException } from '../lib/api'
import { Lock } from 'lucide-react'

export function Login() {
  const { t } = useTranslation()
  const { login } = useAuth()
  const nav = useNavigate()
  const loc = useLocation() as { state?: { from?: { pathname?: string } } }
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [err, setErr] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault()
    setErr(null)
    setBusy(true)
    try {
      await login(email.trim(), password)
      const dest = loc.state?.from?.pathname || '/'
      nav(dest, { replace: true })
    } catch (ex) {
      if (ex instanceof ApiException && ex.status === 401) {
        setErr(t('auth.login.invalid_credentials'))
      } else {
        setErr(t('auth.login.error'))
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="min-h-screen grid place-items-center bg-slate-100">
      <form
        onSubmit={onSubmit}
        className="card w-full max-w-sm p-6 space-y-4"
      >
        <div className="text-center">
          <div className="inline-flex items-center justify-center w-12 h-12 rounded-xl bg-brand-600 text-white mb-3">
            <Lock size={22} />
          </div>
          <h1 className="text-xl font-semibold">SAI</h1>
          <p className="text-sm text-slate-500">{t('auth.login.title')}</p>
        </div>

        <div>
          <label className="label">{t('auth.login.email')}</label>
          <input
            className="input"
            type="email"
            autoFocus
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
        </div>
        <div>
          <label className="label">{t('auth.login.password')}</label>
          <input
            className="input"
            type="password"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </div>

        {err && (
          <div className="text-sm text-red-700 bg-red-50 border border-red-200 rounded-lg px-3 py-2">
            {err}
          </div>
        )}

        <button className="btn-primary w-full justify-center" disabled={busy} type="submit">
          {busy ? t('common.loading') : t('auth.login.submit')}
        </button>
      </form>
    </div>
  )
}