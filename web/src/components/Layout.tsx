import { Link, NavLink, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import {
  LayoutDashboard, Monitor, FolderTree, ScrollText, History,
  ListChecks, KeyRound, LogOut, Languages, Sun, Moon, Monitor as MonitorIcon,
} from 'lucide-react'
import { useAuth } from '../lib/auth'
import { useState } from 'react'
import { useTheme, type ThemeMode } from '../lib/theme'

export function Layout({ children }: { children: React.ReactNode }) {
  const { t, i18n } = useTranslation()
  const { user, logout } = useAuth()
  const { theme, cycleTheme } = useTheme()
  const nav = useNavigate()
  const [lang, setLang] = useState(i18n.language)

  function toggleLang() {
    const next = lang === 'es' ? 'en' : 'es'
    i18n.changeLanguage(next)
    setLang(next)
  }

  async function onLogout() {
    await logout()
    nav('/login')
  }

  const linkCls = ({ isActive }: { isActive: boolean }) =>
    `flex items-center gap-2 px-3 py-2 rounded-lg text-sm ${
      isActive
        ? 'bg-brand-50 text-brand-700 font-semibold dark:bg-brand-900/30 dark:text-brand-300'
        : 'text-slate-700 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800'
    }`

  const themeIcon = (m: ThemeMode) => {
    if (m === 'light') return <Sun size={18} />
    if (m === 'dark') return <Moon size={18} />
    return <MonitorIcon size={18} />
  }

  return (
    <div className="min-h-full flex">
      {/* Sidebar */}
      <aside className="w-60 shrink-0 bg-white border-r border-slate-200 flex flex-col dark:bg-slate-800 dark:border-slate-700">
        <div className="px-5 py-4 border-b border-slate-200 dark:border-slate-700">
          <Link to="/" className="block">
            <div className="text-xl font-bold text-brand-700 dark:text-brand-400">SAI</div>
            <div className="text-xs text-slate-500 dark:text-slate-400">{t('app.tagline')}</div>
          </Link>
        </div>
        <nav className="flex-1 p-3 space-y-1">
          <NavLink to="/" end className={linkCls}>
            <LayoutDashboard size={18} /> {t('nav.dashboard')}
          </NavLink>
          <NavLink to="/agents" className={linkCls}>
            <Monitor size={18} /> {t('nav.agents')}
          </NavLink>
          <NavLink to="/groups" className={linkCls}>
            <FolderTree size={18} /> {t('nav.groups')}
          </NavLink>
          <NavLink to="/templates" className={linkCls}>
            <ScrollText size={18} /> {t('nav.templates')}
          </NavLink>
          <NavLink to="/jobs" className={linkCls}>
            <ListChecks size={18} /> {t('nav.jobs')}
          </NavLink>
          <NavLink to="/tokens" className={linkCls}>
            <KeyRound size={18} /> {t('tokens.title')}
          </NavLink>
          <NavLink to="/audit" className={linkCls}>
            <History size={18} /> {t('nav.audit')}
          </NavLink>
        </nav>
        <div className="p-3 border-t border-slate-200 space-y-1 dark:border-slate-700">
          <button
            onClick={cycleTheme}
            title={t('theme.toggle')}
            aria-label={t('theme.toggle')}
            className="w-full flex items-center gap-2 px-3 py-2 rounded-lg text-sm text-slate-700 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-700"
          >
            {themeIcon(theme)} <span className="truncate">{t(`theme.${theme}`)}</span>
          </button>
          <button onClick={toggleLang} className="w-full flex items-center gap-2 px-3 py-2 rounded-lg text-sm text-slate-700 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-700">
            <Languages size={18} /> {lang.toUpperCase()}
          </button>
          <div className="px-3 py-2 text-xs text-slate-500 truncate dark:text-slate-400" title={user?.email}>{user?.email}</div>
          <button onClick={onLogout} className="w-full flex items-center gap-2 px-3 py-2 rounded-lg text-sm text-red-700 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-950/40">
            <LogOut size={18} /> {t('nav.logout')}
          </button>
        </div>
      </aside>

      {/* Main */}
      <main className="flex-1 min-w-0 overflow-auto">
        <div className="max-w-7xl mx-auto p-6">{children}</div>
      </main>
    </div>
  )
}