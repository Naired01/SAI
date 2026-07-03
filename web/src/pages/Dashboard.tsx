import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import {
  Activity, WifiOff, AlertTriangle, KeyRound, ListChecks, Play,
} from 'lucide-react'
import { get, type DashboardSummary } from '../lib/api'
import { StatusBadge } from '../components/StatusBadge'

export function Dashboard() {
  const { t } = useTranslation()
  const { data, isLoading } = useQuery({
    queryKey: ['dashboard-summary'],
    queryFn: () => get<DashboardSummary>('/api/v1/dashboard/summary'),
    refetchInterval: 15_000,
  })

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold">{t('dashboard.title')}</h1>

      {/* KPIs */}
      <div className="grid grid-cols-2 md:grid-cols-5 gap-4">
        <Kpi icon={<WifiOff size={18} />} label={t('dashboard.kpi.online')} value={data?.kpis.agents_online} tone="green" loading={isLoading} />
        <Kpi icon={<WifiOff size={18} />} label={t('dashboard.kpi.offline')} value={data?.kpis.agents_offline} tone="gray" loading={isLoading} />
        <Kpi icon={<AlertTriangle size={18} />} label={t('dashboard.kpi.problem')} value={data?.kpis.agents_problem} tone="red" loading={isLoading} />
        <Kpi icon={<KeyRound size={18} />} label={t('dashboard.kpi.tokens')} value={data?.kpis.active_tokens} tone="blue" loading={isLoading} />
        <Kpi icon={<Activity size={18} />} label={t('dashboard.kpi.running_jobs')} value={data?.kpis.running_jobs} tone="purple" loading={isLoading} />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        {/* Problem agents */}
        <section className="card p-4">
          <h2 className="font-semibold mb-3 flex items-center gap-2">
            <AlertTriangle size={16} className="text-red-600" /> {t('dashboard.problem.title')}
          </h2>
          {isLoading ? (
            <div className="text-sm text-slate-500">{t('common.loading')}</div>
          ) : !data?.problem_agents?.length ? (
            <div className="text-sm text-slate-500">{t('dashboard.problem.empty')}</div>
          ) : (
            <table className="table">
              <thead>
                <tr>
                  <th>{t('agents.column.hostname')}</th>
                  <th>{t('agents.column.os')}</th>
                  <th>{t('dashboard.problem.last_seen')}</th>
                </tr>
              </thead>
              <tbody>
                {data.problem_agents.map((a) => (
                  <tr key={a.id}>
                    <td>
                      <Link to={`/agents/${a.id}`} className="text-brand-700 hover:underline">
                        {a.hostname}
                      </Link>
                    </td>
                    <td className="text-slate-600">{a.os}</td>
                    <td className="text-slate-500 text-xs">{a.last_seen_at || '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </section>

        {/* Quick actions */}
        <section className="card p-4">
          <h2 className="font-semibold mb-3 flex items-center gap-2">
            <Play size={16} className="text-brand-700" /> {t('dashboard.quick.title')}
          </h2>
          {isLoading ? (
            <div className="text-sm text-slate-500">{t('common.loading')}</div>
          ) : !data?.quick_actions?.length ? (
            <div className="text-sm text-slate-500">{t('dashboard.quick.empty')}</div>
          ) : (
            <div className="flex flex-wrap gap-2">
              {data.quick_actions.map((tpl) => (
                <Link
                  key={tpl.id}
                  to={`/templates?run=${tpl.id}`}
                  className="btn-secondary"
                  title={tpl.description}
                >
                  <Play size={14} /> {tpl.name}
                </Link>
              ))}
            </div>
          )}
        </section>
      </div>

      {/* Recent jobs */}
      <section className="card p-4">
        <h2 className="font-semibold mb-3 flex items-center gap-2">
          <ListChecks size={16} /> {t('dashboard.recent_jobs')}
        </h2>
        {isLoading ? (
          <div className="text-sm text-slate-500">{t('common.loading')}</div>
        ) : !data?.recent_jobs?.length ? (
          <div className="text-sm text-slate-500">{t('dashboard.recent_jobs.empty')}</div>
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th>{t('jobs.name')}</th>
                <th>{t('common.actions')}</th>
                <th>{t('jobs.detail.progress')}</th>
                <th>{t('agents.column.status')}</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {data.recent_jobs.map((j) => (
                <tr key={j.id}>
                  <td>{j.name}</td>
                  <td>{j.target_type}</td>
                  <td className="text-xs text-slate-600">
                    {j.success_items}/{j.total_items}
                  </td>
                  <td><StatusBadge kind="job" value={j.status} /></td>
                  <td>
                    <Link to={`/jobs/${j.id}`} className="text-brand-700 hover:underline text-xs">ver</Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </div>
  )
}

function Kpi({ icon, label, value, tone, loading }: {
  icon: React.ReactNode
  label: string
  value?: number
  tone: 'green' | 'gray' | 'red' | 'blue' | 'purple'
  loading?: boolean
}) {
  const tones: Record<string, string> = {
    green: 'text-green-700 bg-green-50',
    gray: 'text-slate-700 bg-slate-100',
    red: 'text-red-700 bg-red-50',
    blue: 'text-brand-700 bg-brand-50',
    purple: 'text-purple-700 bg-purple-50',
  }
  return (
    <div className="card p-4 flex items-center gap-3">
      <div className={`w-10 h-10 rounded-lg grid place-items-center ${tones[tone]}`}>{icon}</div>
      <div>
        <div className="text-xs text-slate-500">{label}</div>
        <div className="text-2xl font-semibold tabular-nums">{loading ? '…' : (value ?? 0)}</div>
      </div>
    </div>
  )
}