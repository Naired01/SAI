import { useParams } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { get, post, type Job, type JobItem } from '../lib/api'
import { StatusBadge } from '../components/StatusBadge'
import { Download, X } from 'lucide-react'

export function JobDetail() {
  const { id } = useParams<{ id: string }>()
  const { t } = useTranslation()
  const qc = useQueryClient()

  const { data: job } = useQuery({
    queryKey: ['job', id],
    queryFn: () => get<Job>(`/api/v1/jobs/${id}`),
    refetchInterval: 5_000,
  })
  const { data: items } = useQuery({
    queryKey: ['job-items', id],
    queryFn: () => get<{ items: JobItem[]; total: number }>(`/api/v1/jobs/${id}/items?per_page=200`),
    refetchInterval: 5_000,
  })

  const cancelMut = useMutation({
    mutationFn: () => post(`/api/v1/jobs/${id}/cancel`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['job', id] }),
  })

  if (!job) return <div className="text-slate-500 text-sm">{t('common.loading')}</div>

  const canCancel = ['pending', 'dispatching', 'running'].includes(job.status)

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3 flex-wrap">
        <h1 className="text-2xl font-semibold">{job.name}</h1>
        <StatusBadge kind="job" value={job.status} />
        <div className="ml-auto flex gap-2">
          <a href={`/api/v1/jobs/${id}/export.csv`} className="btn-secondary">
            <Download size={14} /> {t('jobs.export.csv')}
          </a>
          {canCancel && (
            <button
              onClick={() => { if (confirm(t('jobs.cancel.confirm'))) cancelMut.mutate() }}
              className="btn-danger"
            >
              <X size={14} /> {t('jobs.cancel')}
            </button>
          )}
        </div>
      </div>

      <div className="card p-4 grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
        <Field label="Source" value={job.source} />
        <Field label="Target" value={job.target_type} />
        <Field label={t('jobs.detail.progress')} value={`${job.success_items}/${job.total_items} (fallos: ${job.failed_items})`} />
        <Field label="Created" value={job.created_at} />
      </div>

      <div className="card overflow-hidden">
        <h2 className="font-semibold p-3 border-b border-slate-200">{t('jobs.detail.items')}</h2>
        <table className="table">
          <thead>
            <tr>
              <th>Agent</th>
              <th>SO</th>
              <th>Status</th>
              <th>Exit</th>
              <th>Started</th>
              <th>Completed</th>
              <th>Error</th>
            </tr>
          </thead>
          <tbody>
            {!items?.items?.length ? (
              <tr><td colSpan={7} className="text-center text-slate-500">{t('jobs.items.empty')}</td></tr>
            ) : items.items.map((it) => (
              <tr key={it.id} className="hover:bg-slate-50">
                <td className="font-mono text-xs">{it.agent_hostname || it.agent_id}</td>
                <td className="text-xs">{it.agent_os}</td>
                <td><span className="badge bg-slate-100 text-slate-700">{it.status}</span></td>
                <td className="text-xs">{it.exit_code ?? '—'}</td>
                <td className="text-xs text-slate-500">{it.started_at || '—'}</td>
                <td className="text-xs text-slate-500">{it.completed_at || '—'}</td>
                <td className="text-xs text-red-700 truncate max-w-[200px]">{it.error_msg}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="text-xs text-slate-500">{label}</div>
      <div className="font-medium">{value}</div>
    </div>
  )
}