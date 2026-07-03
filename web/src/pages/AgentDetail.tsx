import { useState } from 'react'
import { useParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { get, type Agent, type AuditEvent } from '../lib/api'

const TABS = ['info', 'hardware', 'software', 'commands', 'terminal', 'events', 'audit'] as const
type Tab = typeof TABS[number]

export function AgentDetail() {
  const { id } = useParams<{ id: string }>()
  const { t } = useTranslation()
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

  if (isLoading) return <div className="text-slate-500">{t('common.loading')}</div>
  if (!agent) return <div className="text-slate-500">{t('common.empty')}</div>

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3">
        <h1 className="text-2xl font-semibold">{agent.hostname}</h1>
        <span className="text-xs text-slate-500">{agent.os} {agent.arch}</span>
      </div>

      {/* Tabs */}
      <div className="border-b border-slate-200 flex gap-1 overflow-x-auto">
        {TABS.map((k) => (
          <button
            key={k}
            onClick={() => setTab(k)}
            className={`px-3 py-2 text-sm border-b-2 -mb-px whitespace-nowrap ${
              tab === k ? 'border-brand-600 text-brand-700 font-medium' : 'border-transparent text-slate-600 hover:text-slate-900'
            }`}
          >
            {t(`agents.detail.${k}`)}
          </button>
        ))}
      </div>

      {tab === 'info' && <InfoTab agent={agent} />}
      {tab === 'hardware' && <ComingSoon feature={t('agents.detail.hardware')} />}
      {tab === 'software' && <ComingSoon feature={t('agents.detail.software')} />}
      {tab === 'commands' && <ComingSoon feature={t('agents.detail.commands')} />}
      {tab === 'terminal' && <ComingSoon feature={t('agents.detail.terminal')} />}
      {tab === 'events' && (
        <div className="card p-4">
          {events?.items?.length ? (
            <pre className="text-xs whitespace-pre-wrap">{JSON.stringify(events.items, null, 2)}</pre>
          ) : (
            <div className="text-slate-500 text-sm">{t('common.empty')}</div>
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
                  <tr key={e.id}>
                    <td className="text-xs text-slate-500">{e.occurred_at}</td>
                    <td>{e.actor_label}</td>
                    <td><code className="text-xs">{e.action}</code></td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : (
            <div className="text-slate-500 text-sm">{t('common.empty')}</div>
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
      <div className="text-xs text-slate-500">{label}</div>
      <div className="text-sm font-medium break-words">{value}</div>
    </div>
  )
}

function ComingSoon({ feature }: { feature: string }) {
  const { t } = useTranslation()
  return (
    <div className="card p-8 text-center text-slate-500">
      <div className="text-base font-medium">{feature}</div>
      <div className="text-sm mt-1">{t('agents.detail.coming_soon')}</div>
    </div>
  )
}