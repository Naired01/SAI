import { useParams } from 'react-router-dom'
import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { get, post, type Job, type JobItem } from '../lib/api'
import { StatusBadge } from '../components/StatusBadge'
import { Download, X, FileText, AlertTriangle, CheckCircle2, Copy } from 'lucide-react'

export function JobDetail() {
  const { id } = useParams<{ id: string }>()
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [openItem, setOpenItem] = useState<JobItem | null>(null)

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

  if (!job) return <div className="text-slate-500 dark:text-slate-400 text-sm">{t('common.loading')}</div>

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
        <h2 className="font-semibold p-3 border-b border-slate-200 dark:border-slate-700">{t('jobs.detail.items')}</h2>
        <table className="table">
          <thead>
            <tr>
              <th>Agent</th>
              <th>SO</th>
              <th>Status</th>
              <th>Exit</th>
              <th>Duration</th>
              <th>Output</th>
              <th>Error</th>
            </tr>
          </thead>
          <tbody>
            {!items?.items?.length ? (
              <tr><td colSpan={7} className="text-center text-slate-500 dark:text-slate-400">{t('jobs.items.empty')}</td></tr>
            ) : items.items.map((it) => (
              <tr key={it.id} className="hover:bg-slate-50 dark:hover:bg-slate-700/40">
                <td className="font-mono text-xs">{it.agent_hostname || it.agent_id}</td>
                <td className="text-xs">{it.agent_os}</td>
                <td><StatusBadge kind="item" value={it.status} /></td>
                <td className="text-xs font-mono">{it.exit_code ?? '—'}</td>
                <td className="text-xs text-slate-500 dark:text-slate-400">{formatDuration(it.started_at, it.completed_at)}</td>
                <td className="text-xs">
                  <button
                    onClick={() => setOpenItem(it)}
                    disabled={!hasOutput(it)}
                    className={hasOutput(it) ? 'btn-secondary px-2 py-1 text-xs inline-flex items-center gap-1' : 'text-slate-400 dark:text-slate-600 cursor-not-allowed'}
                    title={hasOutput(it) ? t('jobs.detail.view_output') : t('jobs.detail.no_output')}
                  >
                    <FileText size={12} />
                    {t('jobs.detail.view_output')}
                  </button>
                </td>
                <td className="text-xs text-red-700 dark:text-red-400 truncate max-w-[200px]">{it.error_msg}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {openItem && (
        <OutputDrawer item={openItem} onClose={() => setOpenItem(null)} />
      )}
    </div>
  )
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="text-xs text-slate-500 dark:text-slate-400">{label}</div>
      <div className="font-medium">{value}</div>
    </div>
  )
}

function hasOutput(it: JobItem): boolean {
  return Boolean((it.stdout && it.stdout.length > 0) || (it.stderr && it.stderr.length > 0))
}

function formatDuration(start?: string, end?: string): string {
  if (!start) return '—'
  const s = new Date(start).getTime()
  const e = end ? new Date(end).getTime() : Date.now()
  if (Number.isNaN(s) || Number.isNaN(e)) return '—'
  const ms = Math.max(0, e - s)
  if (ms < 1000) return `${ms}ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`
  const mins = Math.floor(ms / 60_000)
  const secs = Math.floor((ms % 60_000) / 1000)
  return `${mins}m${secs}s`
}

// OutputDrawer muestra stdout y stderr con copy-to-clipboard. Modal
// liviano sobre la pagina: backdrop + panel derecho.
function OutputDrawer({ item, onClose }: { item: JobItem; onClose: () => void }) {
  const { t } = useTranslation()
  const stdout = item.stdout || ''
  const stderr = item.stderr || ''
  const isError = item.exit_code !== undefined && item.exit_code !== 0
  const isTimeout = item.error_msg === 'timeout'

  return (
    <div className="fixed inset-0 z-50 flex" onClick={onClose}>
      <div className="flex-1 bg-slate-900/40 dark:bg-slate-900/60" />
      <div
        className="w-full max-w-3xl bg-white dark:bg-slate-800 border-l border-slate-200 dark:border-slate-700 flex flex-col shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between p-4 border-b border-slate-200 dark:border-slate-700">
          <div className="flex items-center gap-2">
            {isError ? (
              isTimeout
                ? <AlertTriangle size={18} className="text-amber-600" />
                : <X size={18} className="text-red-600" />
            ) : (
              <CheckCircle2 size={18} className="text-emerald-600" />
            )}
            <h3 className="font-semibold">
              {item.agent_hostname || item.agent_id}
            </h3>
            <span className="text-xs text-slate-500 dark:text-slate-400">
              exit={item.exit_code ?? '—'} · {formatDuration(item.started_at, item.completed_at)}
            </span>
          </div>
          <button onClick={onClose} className="text-slate-500 hover:text-slate-900 dark:hover:text-white text-xl leading-none">
            ×
          </button>
        </div>

        {item.error_msg && (
          <div className={`px-4 py-2 text-sm border-b border-slate-200 dark:border-slate-700 ${
            isTimeout ? 'bg-amber-50 dark:bg-amber-900/20 text-amber-800 dark:text-amber-200' : 'bg-red-50 dark:bg-red-900/20 text-red-800 dark:text-red-200'
          }`}>
            {item.error_msg}
          </div>
        )}

        <div className="flex-1 overflow-y-auto p-4 space-y-4">
          <OutputBlock label={t('jobs.detail.stdout')} text={stdout} />
          <OutputBlock label={t('jobs.detail.stderr')} text={stderr} />
        </div>
      </div>
    </div>
  )
}

function OutputBlock({ label, text }: { label: string; text: string }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  const copy = async () => {
    if (!text) return
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      // fallback: select + execCommand (legacy)
      const ta = document.createElement('textarea')
      ta.value = text
      document.body.appendChild(ta)
      ta.select()
      try { document.execCommand('copy') } catch {}
      document.body.removeChild(ta)
    }
  }
  return (
    <div>
      <div className="flex items-center justify-between mb-1">
        <div className="text-xs font-semibold uppercase tracking-wide text-slate-500 dark:text-slate-400">
          {label} {text && <span className="text-slate-400 dark:text-slate-500">({text.length} chars)</span>}
        </div>
        <button
          onClick={copy}
          disabled={!text}
          className={text ? 'btn-secondary px-2 py-0.5 text-xs inline-flex items-center gap-1' : 'text-slate-400 cursor-not-allowed'}
        >
          <Copy size={11} /> {copied ? t('jobs.detail.copied') : t('jobs.detail.copy')}
        </button>
      </div>
      <pre className={`text-xs font-mono p-3 rounded border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-900 overflow-x-auto whitespace-pre-wrap break-all ${
        text ? '' : 'text-slate-400 dark:text-slate-600 italic'
      }`}>
        {text || '(empty)'}
      </pre>
    </div>
  )
}