import { createContext, useContext, useEffect, useState } from 'react'

export type ThemeMode = 'system' | 'light' | 'dark'

const STORAGE_KEY = 'sai:theme'

type Ctx = {
  theme: ThemeMode
  effective: 'light' | 'dark'
  setTheme: (m: ThemeMode) => void
  cycleTheme: () => void
}

const ThemeCtx = createContext<Ctx | null>(null)

function readStored(): ThemeMode {
  try {
    const v = localStorage.getItem(STORAGE_KEY)
    if (v === 'light' || v === 'dark' || v === 'system') return v
  } catch {}
  return 'system'
}

function systemPrefersDark(): boolean {
  return typeof window !== 'undefined'
    && typeof window.matchMedia === 'function'
    && window.matchMedia('(prefers-color-scheme: dark)').matches
}

function applyClass(effective: 'light' | 'dark') {
  if (typeof document === 'undefined') return
  const root = document.documentElement
  if (effective === 'dark') root.classList.add('dark')
  else root.classList.remove('dark')
}

export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const [theme, setThemeState] = useState<ThemeMode>(() => readStored())

  const systemDark = useSystemDark(theme === 'system')
  const effective: 'light' | 'dark' = theme === 'dark' || (theme === 'system' && systemDark) ? 'dark' : 'light'

  useEffect(() => {
    applyClass(effective)
  }, [effective])

  function setTheme(m: ThemeMode) {
    setThemeState(m)
    try { localStorage.setItem(STORAGE_KEY, m) } catch {}
  }

  function cycleTheme() {
    const order: ThemeMode[] = ['system', 'light', 'dark']
    const next = order[(order.indexOf(theme) + 1) % order.length]
    setTheme(next)
  }

  return (
    <ThemeCtx.Provider value={{ theme, effective, setTheme, cycleTheme }}>
      {children}
    </ThemeCtx.Provider>
  )
}

function useSystemDark(enabled: boolean): boolean {
  const [dark, setDark] = useState<boolean>(() => systemPrefersDark())
  useEffect(() => {
    if (!enabled || typeof window === 'undefined' || typeof window.matchMedia !== 'function') return
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const onChange = () => setDark(mq.matches)
    if (mq.addEventListener) mq.addEventListener('change', onChange)
    else if ((mq as any).addListener) (mq as any).addListener(onChange)
    setDark(mq.matches)
    return () => {
      if (mq.removeEventListener) mq.removeEventListener('change', onChange)
      else if ((mq as any).removeListener) (mq as any).removeListener(onChange)
    }
  }, [enabled])
  return dark
}

export function useTheme(): Ctx {
  const ctx = useContext(ThemeCtx)
  if (!ctx) throw new Error('useTheme must be inside ThemeProvider')
  return ctx
}
