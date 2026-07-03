import { useTranslation } from 'react-i18next'

type Kind = 'job' | 'token' | 'agent'

export function StatusBadge({ kind, value }: { kind: Kind; value: string }) {
  const { t } = useTranslation()
  let cls = 'bg-slate-100 text-slate-700'
  if (kind === 'job') {
    switch (value) {
      case 'pending': cls = 'bg-slate-100 text-slate-700'; break
      case 'dispatching':
      case 'running': cls = 'bg-blue-100 text-blue-700'; break
      case 'completed': cls = 'bg-green-100 text-green-700'; break
      case 'failed': cls = 'bg-red-100 text-red-700'; break
      case 'partial': cls = 'bg-amber-100 text-amber-800'; break
      case 'cancelled': cls = 'bg-slate-200 text-slate-600'; break
    }
    return <span className={`badge ${cls}`}>{t(`jobs.status.${value}`, value)}</span>
  }
  if (kind === 'token') {
    if (value === 'active') cls = 'bg-green-100 text-green-700'
    else if (value === 'exhausted') cls = 'bg-amber-100 text-amber-800'
    else if (value === 'expired') cls = 'bg-slate-200 text-slate-600'
    else if (value === 'revoked') cls = 'bg-red-100 text-red-700'
    return <span className={`badge ${cls}`}>{t(`tokens.status.${value}`, value)}</span>
  }
  if (kind === 'agent') {
    if (value === 'online') cls = 'bg-green-100 text-green-700'
    else if (value === 'offline') cls = 'bg-slate-200 text-slate-600'
    else if (value === 'problem') cls = 'bg-red-100 text-red-700'
    return <span className={`badge ${cls}`}>{t(`common.${value}`, value)}</span>
  }
  return <span className={`badge ${cls}`}>{value}</span>
}