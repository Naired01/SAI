import { useTranslation } from 'react-i18next'

type Kind = 'job' | 'token' | 'agent'

export function StatusBadge({ kind, value }: { kind: Kind; value: string }) {
  const { t } = useTranslation()
  let cls = 'bg-slate-100 text-slate-700 dark:bg-slate-700 dark:text-slate-200'
  if (kind === 'job') {
    switch (value) {
      case 'pending': cls = 'bg-slate-100 text-slate-700 dark:bg-slate-700 dark:text-slate-200'; break
      case 'dispatching':
      case 'running': cls = 'bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300'; break
      case 'completed': cls = 'bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-300'; break
      case 'failed': cls = 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300'; break
      case 'partial': cls = 'bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-300'; break
      case 'cancelled': cls = 'bg-slate-200 text-slate-600 dark:bg-slate-700 dark:text-slate-300'; break
    }
    return <span className={`badge ${cls}`}>{t(`jobs.status.${value}`, value)}</span>
  }
  if (kind === 'token') {
    if (value === 'active') cls = 'bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-300'
    else if (value === 'exhausted') cls = 'bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-300'
    else if (value === 'expired') cls = 'bg-slate-200 text-slate-600 dark:bg-slate-700 dark:text-slate-300'
    else if (value === 'revoked') cls = 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300'
    return <span className={`badge ${cls}`}>{t(`tokens.status.${value}`, value)}</span>
  }
  if (kind === 'agent') {
    if (value === 'online') cls = 'bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-300'
    else if (value === 'offline') cls = 'bg-slate-200 text-slate-600 dark:bg-slate-700 dark:text-slate-300'
    else if (value === 'problem') cls = 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300'
    return <span className={`badge ${cls}`}>{t(`common.${value}`, value)}</span>
  }
  return <span className={`badge ${cls}`}>{value}</span>
}