import { createContext, useContext, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

type AuthState = {
  user: { id: string; email: string; role: string } | null
  csrf: string | null
  loading: boolean
  refresh: () => Promise<void>
  login: (email: string, password: string) => Promise<void>
  logout: () => Promise<void>
  setCsrf: (t: string) => void
}

const Ctx = createContext<AuthState | null>(null)

import { api, setCsrf as setCsrfApi, type Me } from './api'

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<AuthState['user']>(null)
  const [csrf, setCsrfState] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  async function refresh() {
    try {
      const me = await api<Me>('/api/v1/auth/me')
      setUser(me)
      const csr = await api<{ csrf: string }>('/api/v1/auth/csrf')
      setCsrfState(csr.csrf)
      setCsrfApi(csr.csrf)
    } catch {
      setUser(null)
      setCsrfState(null)
      setCsrfApi(null)
    } finally {
      setLoading(false)
    }
  }

  async function login(email: string, password: string) {
    const r = await api<{ user: Me; csrf: string }>('/api/v1/auth/login', {
      method: 'POST',
      body: JSON.stringify({ email, password }),
    })
    setUser(r.user)
    setCsrfState(r.csrf)
    setCsrfApi(r.csrf)
  }

  async function logout() {
    try {
      await api('/api/v1/auth/logout', { method: 'POST' })
    } finally {
      setUser(null)
      setCsrfState(null)
      setCsrfApi(null)
    }
  }

  useEffect(() => {
    refresh()
  }, [])

  return (
    <Ctx.Provider value={{ user, csrf, loading, refresh, login, logout, setCsrf: setCsrfState }}>
      {children}
    </Ctx.Provider>
  )
}

export function useAuth() {
  const ctx = useContext(Ctx)
  if (!ctx) throw new Error('useAuth must be inside AuthProvider')
  return ctx
}

// hook re-export para evitar warning de useTranslation no usado
export function useT() {
  return useTranslation()
}