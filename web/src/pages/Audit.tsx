import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Download, X } from 'lucide-react'
import { get, type AuditEvent } from '../lib/api'

export function Audit() {
  const { t } = useTranslation()
  const [filters, setFilters] = useState({
    date_from: '', date_to: '', actor_type: '', action: '', target_type: '', q: '',
  })

  const { data: actions } = useQuery({
    queryKey: ['audit-actions'],
    queryFn: () => get<{ items: string[] }>('/api/v1/audit/actions'),
  })

  const sp = new URLSearchParams()
  for (const [k, v] of Object.entries(filters)) if (v) sp.set(k, v)
  sp.set('per_page', '50')

  const { data, isLoading } = useQuery({
    queryKey: ['audit', sp.toString()],
    queryFn: () => get<{ items: AuditEvent[]; total: number }>(`/api/v1/audit/events?${sp.toString()}`),
    refetchInterval: 10_000,
  })

  const [open, setOpen] = useState<AuditEvent | null>(null)

  function clear() {
    setFilters({ date_from: '', date_to: '', actor_type: '', action: '', target_type: '', q: '' })
  }

  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold">{t('audit.title')}</h1>

      <div className="card p-3 grid grid-cols-2 md:grid-cols-6 gap-2">
        <input type="datetime-local" className="input" value={filters.date_from} onChange={(e) => setFilters({ ...filters, date_from: e.target.value })} title={t('audit.filter.date_from')} />
        <input type="datetime-local" className="input" value={filters.date_to} onChange={(e) => setFilters({ ...filters, date_to: e.target.value })} title={t('audit.filter.date_to')} />
        <select className="input" value={filters.actor_type} onChange={(e) => setFilters({ ...filters, actor_type: e.target.value })}>
          <option value="">{t('audit.filter.actor')}</option>
          <option value="user">user</option>
          <option value="agent">agent</option>
          <option value="system">system</option>
          <option value="token">token</option>
        </select>
        <select className="input" value={filters.action} onChange={(e) => setFilters({ ...filters, action: e.target.value })}>
          <option value="">{t('audit.filter.action')}</option>
          {(actions?.items || []).map((a) => <option key={a} value={a}>{a}</option>)}
        </select>
        <select className="input" value={filters.target_type} onChange={(e) => setFilters({ ...filters, target_type: e.target.value })}>
          <option value="">{t('audit.filter.target')}</option>
          <option value="agent">agent</option>
          <option value="group">group</option>
          <option value="token">token</option>
          <option value="template">template</option>
          <option value="job">job</option>
        </select>
        <input className="input" placeholder={t('audit.filter.search')} value={filters.q} onChange={(e) => setFilters({ ...filters, q: e.target.value })} />
        <button className="btn-secondary col-span-2 md:col-span-1" onClick={clear}>
          <X size={14} /> {t('audit.clear')}
        </button>
        <a href={`/api/v1/audit/export.csv?${sp.toString()}`} className="btn-secondary col-span-2 md:col-span-1">
          <Download size={14} /> {t('audit.export')}
        </a>
      </div>

      <div className="card overflow-hidden">
        <table className="table">
          <thead>
            <tr>
              <th>{t('audit.column.when')}</th>
              <th>{t('audit.column.actor')}</th>
              <th>{t('audit.column.action')}</th>
              <th>{t('audit.column.target')}</th>
              <th>{t('audit.column.ip')}</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {isLoading ? (
              <tr><td colSpan={6} className="text-center text-slate-500">{t('common.loading')}</td></tr>
            ) : !data?.items?.length ? (
              <tr><td colSpan={6} className="text-center text-slate-500">{t('audit.empty')}</td></tr>
            ) : data.items.map((e) => (
              <tr key={e.id} className="hover:bg-slate-50 cursor-pointer" onClick={() => setOpen(e)}>
                <td className="text-xs text-slate-500">{e.occurred_at}</td>
                <td><span className="badge bg-slate-100 text-slate-700 mr-1">{e.actor_type}</span> {e.actor_label}</td>
                <td><code className="text-xs">{e.action}</code></td>
                <td className="text-xs">
                  {e.target_type && <span className="badge bg-slate-100 text-slate-700 mr-1">{e.target_type}</span>}
                  {e.target_label || '—'}
                </td>
                <td className="text-xs text-slate-500">{e.ip || '—'}</td>
                <td><span className="text-xs text-brand-700">ver</span></td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {open && (
        <div className="fixed inset-0 bg-black/40 grid place-items-center z-50 p-4" onClick={() => setOpen(null)}>
          <div className="bg-white rounded-xl shadow-xl w-full max-w-2xl p-5 space-y-3" onClick={(e) => e.stopPropagation()}>
            <h2 className="text-lg font-semibold">{t('audit.detail')}</h2>
            <pre className="text-xs whitespace-pre-wrap bg-slate-50 p-3 rounded border border-slate-200">
              {JSON.stringify(open, null, 2)}
            </pre>
            <div className="flex justify-end">
              <button className="btn-secondary" onClick={() => setOpen(null)}>{t('common.close')}</button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}