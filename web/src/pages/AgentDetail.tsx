import { useState } from 'react'
import { useParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { RefreshCw } from 'lucide-react'
import {
  get,
  refreshInventory,
  getInventory,
  type Agent,
  type AuditEvent,
  type InventorySnapshot,
} from '../lib/api'
import { InventoryHardware } from '../components/InventoryHardware'
import { InventorySoftware } from '../components/InventorySoftware'
import { StatusBadge } from '../components/StatusBadge'
import { formatRelativeFromNow } from '../lib/format'
import { Link } from 'react-router-dom'

const TABS = ['info', 'hardware', 'software', 'commands', 'terminal', 'events', 'audit'] as const
type Tab = typeof TABS[number]

type AgentJobRow = {
  id: string
  job_id: string
  job_name: string
  status: string
  exit_code?: number
  error_msg?: string
  started_at?: string
  completed_at?: string
}

export function AgentDetail() {
  const { id } = useParams<{ id: string }>()
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [tab, setTab] = useState<Tab>('info')

  const { data: agent, isLoading } = useQuery({
    queryKey: ['agent', id],
    queryFn: () => get<Agent>(`/api/v1/agents/${id}`),
    enabled: !!id,
    refetchInterval: 15_000,
  })

  const { data: events } = useQuery({
    queryKey: ['agent-events', id],
    queryFn: () => get<{ items: any[] }>(`/api/v1/agents/${id}/events?limit=50`),
    enabled: !!id && tab === 'events',
  })

  const { data: audit } = useQuery({
    queryKey: ['agent-audit', id],
    queryFn: () => get<{ items: AuditEvent[] }>(`/api/v1/audit/events?target_type=agent&q=${id}&per_page=50`),
    enabled: !!id && tab === 'audit',
  })

  const { data: inventory, isFetching: inventoryLoading, error: inventoryError } = useQuery({
    queryKey: ['inventory', id],
    queryFn: () => getInventory(id!),
    enabled: !!id && tab === 'hardware',
    retry: false,
  })

  const refreshMut = useMutation({
    mutationFn: () => refreshInventory(id!),
    onSuccess: () => {
      // Refetch inventory tras 3s para darle tiempo al agente de responder.
      setTimeout(() => {
        qc.invalidateQueries({ queryKey: ['inventory', id] })
        qc.invalidateQueries({ queryKey: ['agent', id] })
      }, 3000)
    },
  })

  if (isLoading) return <div className="text-slate-500 dark:text-slate-400">{t('common.loading')}</div>
  if (!agent) return <div className="text-slate-500 dark:text-slate-400">{t('common.empty')}</div>

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3 flex-wrap">
        <h1 className="text-2xl font-semibold">{agent.hostname}</h1>
        <span className="text-xs text-slate-500 dark:text-slate-400">{agent.os} {agent.arch}</span>
        {(agent as any).last_inventory_at && (
          <span className="text-xs text-slate-500 dark:text-slate-400">
            {t('inventory.last')}: {formatRelativeFromNow((agent as any).last_inventory_at)}
          </span>
        )}
        <button
          onClick={() => refreshMut.mutate()}
          disabled={refreshMut.isPending}
          className="ml-auto inline-flex items-center gap-1 px-3 py-1 text-sm rounded border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-800 hover:bg-slate-50 dark:hover:bg-slate-700 disabled:opacity-50"
          title={t('inventory.refresh.title')}
        >
          <RefreshCw size={14} className={refreshMut.isPending ? 'animate-spin' : ''} />
          {t('inventory.refresh.button')}
        </button>
        {refreshMut.data && (
          <span className={`text-xs ${refreshMut.data.delivered ? 'text-emerald-600' : 'text-amber-600'}`}>
            {refreshMut.data.delivered
              ? t('inventory.refresh.sent')
              : t('inventory.refresh.queued')}
          </span>
        )}
      </div>

      {/* Tabs */}
      <div className="border-b border-slate-200 flex gap-1 overflow-x-auto dark:border-slate-700">
        {TABS.map((k) => (
          <button
            key={k}
            onClick={() => setTab(k)}
            className={`px-3 py-2 text-sm border-b-2 -mb-px whitespace-nowrap ${
              tab === k
                ? 'border-brand-600 text-brand-700 font-medium dark:border-brand-400 dark:text-brand-300'
                : 'border-transparent text-slate-600 hover:text-slate-900 dark:text-slate-400 dark:hover:text-slate-100'
            }`}
          >
            {t(`agents.detail.${k}`)}
          </button>
        ))}
      </div>

      {tab === 'info' && <InfoTab agent={agent} />}
      {tab === 'hardware' && (
        <HardwareTab
          inventory={inventory}
          loading={inventoryLoading}
          error={inventoryError}
        />
      )}
      {tab === 'software' && <SoftwareTab inventory={inventory} loading={inventoryLoading} />}
      {tab === 'commands' && <CommandsTab agentId={id} />}
      {tab === 'terminal' && <ComingSoon feature={t('agents.detail.terminal')} />}
      {tab === 'terminal' && <ComingSoon feature={t('agents.detail.terminal')} />}
      {tab === 'events' && (
        <div className="card p-4">
          {events?.items?.length ? (
            <pre className="text-xs whitespace-pre-wrap">{JSON.stringify(events.items, null, 2)}</pre>
          ) : (
            <div className="text-slate-500 dark:text-slate-400 text-sm">{t('common.empty')}</div>
          )}
        </div>
      )}
      {tab === 'audit' && (
        <div className="card p-4">
          {audit?.items?.length ? (
            <table className="table">
              <thead>
                <tr><th>{t('audit.column.when')}</th><th>{t('audit.column.actor')}</th><th>{t('audit.column.action')}</th></tr>
              </thead>
              <tbody>
                {audit.items.map((e) => (
                  <tr key={e.id} className="hover:bg-slate-50 dark:hover:bg-slate-700/40">
                    <td className="text-xs text-slate-500 dark:text-slate-400">{e.occurred_at}</td>
                    <td>{e.actor_label}</td>
                    <td><code className="text-xs">{e.action}</code></td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : (
            <div className="text-slate-500 dark:text-slate-400 text-sm">{t('common.empty')}</div>
          )}
        </div>
      )}
    </div>
  )
}

function InfoTab({ agent }: { agent: Agent }) {
  const { t } = useTranslation()
  return (
    <div className="card p-4 grid grid-cols-2 gap-4 max-w-2xl">
      <Field label={t('agents.detail.hostname')} value={agent.hostname} />
      <Field label={t('agents.detail.os')} value={`${agent.os} ${agent.os_version || ''}`} />
      <Field label={t('agents.detail.arch')} value={agent.arch || '—'} />
      <Field label={t('agents.detail.version')} value={agent.agent_version || '—'} />
      <Field label={t('agents.detail.first_seen')} value={agent.first_seen_at} />
      <Field label={t('agents.detail.last_seen')} value={agent.last_seen_at || '—'} />
      <Field label={t('agents.detail.visibility')} value={agent.visibility} />
    </div>
  )
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="text-xs text-slate-500 dark:text-slate-400">{label}</div>
      <div className="text-sm font-medium break-words">{value}</div>
    </div>
  )
}

function HardwareTab({
  inventory,
  loading,
  error,
}: {
  inventory: InventorySnapshot | undefined
  loading: boolean
  error: unknown
}) {
  const { t } = useTranslation()
  if (loading) {
    return <div className="card p-4 text-slate-500 dark:text-slate-400">{t('common.loading')}</div>
  }
  if (error || !inventory) {
    return (
      <div className="card p-8 text-center space-y-3">
        <div className="text-slate-500 dark:text-slate-400 text-sm">
          {t('inventory.empty.title')}
        </div>
        <div className="text-slate-400 dark:text-slate-500 text-xs">
          {t('inventory.empty.hint')}
        </div>
      </div>
    )
  }
  return (
    <InventoryHardware
      hardware={inventory.hardware}
      collectedAt={inventory.received_at}
      agentVersion={inventory.agent_version}
    />
  )
}

function SoftwareTab({
  inventory,
  loading,
}: {
  inventory: InventorySnapshot | undefined
  loading: boolean
}) {
  const { t } = useTranslation()
  if (loading) {
    return <div className="card p-4 text-slate-500 dark:text-slate-400">{t('common.loading')}</div>
  }
  return <InventorySoftware software={inventory?.software} />
}

function ComingSoon({ feature }: { feature: string }) {
  const { t } = useTranslation()
  return (
    <div className="card p-8 text-center text-slate-500 dark:text-slate-400">
      <div className="text-base font-medium">{feature}</div>
      <div className="text-sm mt-1">{t('agents.detail.coming_soon')}</div>
    </div>
  )
}

// CommandsTab muestra los ultimos 20 job_items del agente con link
// al detalle del job. Sustituye al placeholder ComingSoon de la tab
// "commands" que existia desde Fase 1.
function CommandsTab({ agentId }: { agentId?: string }) {
  const { t } = useTranslation()
  const { data, isLoading } = useQuery({
    queryKey: ['agent-jobs', agentId],
    queryFn: () => get<{ items: AgentJobRow[]; total: number }>(`/api/v1/agents/${agentId}/jobs?limit=20`),
    enabled: !!agentId,
    refetchInterval: 10_000,
  })
  if (!agentId) return null
  if (isLoading) return <div className="card p-4 text-slate-500 dark:text-slate-400 text-sm">{t('common.loading')}</div>
  const items = data?.items || []
  if (!items.length) {
    return (
      <div className="card p-8 text-center text-slate-500 dark:text-slate-400">
        <div className="text-sm">{t('agents.detail.no_jobs')}</div>
        <Link to="/jobs" className="text-xs text-brand-600 hover:underline mt-2 inline-block">
          {t('nav.jobs')} →
        </Link>
      </div>
    )
  }
  return (
    <div className="card overflow-hidden">
      <h2 className="font-semibold p-3 border-b border-slate-200 dark:border-slate-700 flex items-center justify-between">
        <span>{t('agents.detail.recent_jobs')}</span>
        <Link to="/jobs" className="text-xs text-brand-600 hover:underline">{t('jobs.title')} →</Link>
      </h2>
      <table className="table">
        <thead>
          <tr>
            <th>Job</th>
            <th>Status</th>
            <th>Exit</th>
            <th>Started</th>
            <th>Error</th>
          </tr>
        </thead>
        <tbody>
          {items.map((it) => (
            <tr key={it.id} className="hover:bg-slate-50 dark:hover:bg-slate-700/40">
              <td className="text-sm">
                <Link to={`/jobs/${it.job_id}`} className="text-brand-600 hover:underline">{it.job_name}</Link>
              </td>
              <td><StatusBadge kind="item" value={it.status} /></td>
              <td className="text-xs font-mono">{it.exit_code ?? '—'}</td>
              <td className="text-xs text-slate-500 dark:text-slate-400">{it.started_at ? formatRelativeFromNow(it.started_at) : '—'}</td>
              <td className="text-xs text-red-700 dark:text-red-400 truncate max-w-[200px]">{it.error_msg}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
