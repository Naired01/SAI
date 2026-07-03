import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link, useSearchParams, useNavigate } from 'react-router-dom'
import { Search, FolderTree, KeyRound } from 'lucide-react'
import { get, type Agent, type Group } from '../lib/api'
import { StatusBadge } from '../components/StatusBadge'

export function Agents() {
  const { t } = useTranslation()
  const [search, setSearch] = useSearchParams()
  const nav = useNavigate()
  const groupId = search.get('group_id') || ''
  const ungrouped = search.get('ungrouped') === 'true'
  const status = search.get('status') || ''
  const q = search.get('q') || ''

  const { data: treeData } = useQuery({
    queryKey: ['groups-tree'],
    queryFn: () => get<{ tree: Group[]; ungrouped_count: number }>('/api/v1/groups'),
  })

  const { data, isLoading } = useQuery({
    queryKey: ['agents', { groupId, ungrouped, status, q }],
    queryFn: () => {
      const sp = new URLSearchParams()
      if (groupId) sp.set('group_id', groupId)
      if (ungrouped) sp.set('ungrouped', 'true')
      if (status) sp.set('status', status)
      if (q) sp.set('q', q)
      sp.set('per_page', '50')
      return get<{ items: Agent[]; total: number }>(`/api/v1/agents?${sp.toString()}`)
    },
  })

  function setParam(k: string, v: string | null) {
    const sp = new URLSearchParams(search)
    if (v === null || v === '') sp.delete(k)
    else sp.set(k, v)
    setSearch(sp)
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">{t('agents.title')}</h1>
        <button onClick={() => nav('/tokens')} className="btn-primary">
          <KeyRound size={16} /> {t('tokens.title')}
        </button>
      </div>

      {/* Empty state cuando no hay agentes */}
      {!isLoading && data?.items?.length === 0 && (
        <div className="card p-8 text-center">
          <div className="text-base font-medium text-slate-700 mb-1">
            Sin agentes conectados todavía
          </div>
          <p className="text-sm text-slate-500 mb-4">
            Para enrolar tu primer equipo: creá un enrollment token en{' '}
            <Link to="/tokens" className="text-brand-700 hover:underline">Tokens</Link>,
            copiá la URL de descarga, y ejecutá el script de instalación en el equipo destino.
          </p>
          <button onClick={() => nav('/tokens')} className="btn-primary">
            <KeyRound size={16} /> Crear enrollment token
          </button>
        </div>
      )}

      <div className="grid grid-cols-1 md:grid-cols-[260px_1fr] gap-4">
        {/* Sidebar: groups */}
        <aside className="card p-3 h-fit">
          <div className="flex items-center gap-2 mb-2 text-sm font-medium text-slate-700">
            <FolderTree size={16} /> {t('groups.title')}
          </div>
          <div className="space-y-1">
            <button
              onClick={() => { setParam('group_id', null); setParam('ungrouped', null) }}
              className={`w-full text-left text-sm px-2 py-1 rounded ${
                !groupId && !ungrouped ? 'bg-brand-50 text-brand-700 font-medium' : 'hover:bg-slate-100'
              }`}
            >
              {t('agents.filter.all')}
            </button>
            <button
              onClick={() => { setParam('group_id', null); setParam('ungrouped', 'true') }}
              className={`w-full text-left text-sm px-2 py-1 rounded ${
                ungrouped ? 'bg-brand-50 text-brand-700 font-medium' : 'hover:bg-slate-100'
              }`}
            >
              {t('agents.ungrouped')} ({treeData?.ungrouped_count ?? 0})
            </button>
            <GroupTree
              nodes={treeData?.tree || []}
              selectedId={groupId}
              onSelect={(id) => { setParam('ungrouped', null); setParam('group_id', id) }}
            />
          </div>
        </aside>

        {/* List */}
        <section className="space-y-3">
          <div className="card p-3 flex flex-wrap items-center gap-2">
            <div className="relative flex-1 min-w-[200px]">
              <Search size={14} className="absolute left-3 top-2.5 text-slate-400" />
              <input
                className="input pl-8"
                placeholder={t('agents.search_placeholder')}
                defaultValue={q}
                onKeyDown={(e) => { if (e.key === 'Enter') setParam('q', (e.target as HTMLInputElement).value) }}
              />
            </div>
            <select
              className="input w-auto"
              value={status}
              onChange={(e) => setParam('status', e.target.value)}
            >
              <option value="">{t('agents.filter.all')}</option>
              <option value="online">{t('common.online')}</option>
              <option value="offline">{t('common.offline')}</option>
            </select>
          </div>

          <div className="card overflow-hidden">
            <table className="table">
              <thead>
                <tr>
                  <th>{t('agents.column.hostname')}</th>
                  <th>{t('agents.column.os')}</th>
                  <th>{t('agents.column.version')}</th>
                  <th>{t('agents.column.last_seen')}</th>
                  <th>{t('agents.column.status')}</th>
                </tr>
              </thead>
              <tbody>
                {isLoading ? (
                  <tr><td colSpan={5} className="text-center text-slate-500">{t('common.loading')}</td></tr>
                ) : !data?.items?.length ? (
                  <tr><td colSpan={5} className="text-center text-slate-500">{t('common.empty')}</td></tr>
                ) : data.items.map((a) => (
                  <tr key={a.id} className="hover:bg-slate-50">
                    <td>
                      <Link to={`/agents/${a.id}`} className="text-brand-700 hover:underline">
                        {a.hostname}
                      </Link>
                    </td>
                    <td className="text-slate-600">{a.os} {a.arch && <span className="text-slate-400">/ {a.arch}</span>}</td>
                    <td className="text-slate-500 text-xs">{a.agent_version || '—'}</td>
                    <td className="text-slate-500 text-xs">{a.last_seen_at || '—'}</td>
                    <td><StatusBadge kind="agent" value={a.online ? 'online' : 'offline'} /></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      </div>
    </div>
  )
}

function GroupTree({ nodes, selectedId, onSelect, depth = 0 }: {
  nodes: Group[]
  selectedId: string
  onSelect: (id: string) => void
  depth?: number
}) {
  return (
    <>
      {nodes.map((n) => (
        <div key={n.id}>
          <button
            onClick={() => onSelect(n.id)}
            style={{ paddingLeft: `${depth * 12 + 8}px` }}
            className={`w-full text-left text-sm px-2 py-1 rounded ${
              selectedId === n.id ? 'bg-brand-50 text-brand-700 font-medium' : 'hover:bg-slate-100'
            }`}
          >
            {n.name} <span className="text-slate-400 text-xs">({n.member_count})</span>
          </button>
          {n.children && n.children.length > 0 && (
            <GroupTree nodes={n.children} selectedId={selectedId} onSelect={onSelect} depth={depth + 1} />
          )}
        </div>
      ))}
    </>
  )
}