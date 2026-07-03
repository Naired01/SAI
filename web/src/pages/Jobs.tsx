import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { get, type Job } from '../lib/api'
import { StatusBadge } from '../components/StatusBadge'

export function Jobs() {
  const { t } = useTranslation()
  const { data, isLoading } = useQuery({
    queryKey: ['jobs'],
    queryFn: () => get<{ items: Job[]; total: number }>('/api/v1/jobs?per_page=50'),
    refetchInterval: 5_000,
  })

  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold">{t('jobs.title')}</h1>

      <div className="card overflow-hidden">
        <table className="table">
          <thead>
            <tr>
              <th>{t('jobs.name')}</th>
              <th>Source</th>
              <th>Target</th>
              <th>{t('jobs.detail.progress')}</th>
              <th>{t('agents.column.status')}</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {isLoading ? (
              <tr><td colSpan={6} className="text-center text-slate-500">{t('common.loading')}</td></tr>
            ) : !data?.items?.length ? (
              <tr><td colSpan={6} className="text-center text-slate-500">{t('common.empty')}</td></tr>
            ) : data.items.map((j) => (
              <tr key={j.id} className="hover:bg-slate-50">
                <td className="font-medium">{j.name}</td>
                <td className="text-xs text-slate-500">{j.source}</td>
                <td className="text-xs">{j.target_type}</td>
                <td className="text-xs">{j.success_items}/{j.total_items}</td>
                <td><StatusBadge kind="job" value={j.status} /></td>
                <td><Link to={`/jobs/${j.id}`} className="text-brand-700 hover:underline text-xs">ver</Link></td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}