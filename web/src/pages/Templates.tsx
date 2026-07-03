import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'
import { Plus, Trash2, Play, Lock, Pencil } from 'lucide-react'
import { get, post, patch, del, type Template, type Group, type Agent } from '../lib/api'

export function Templates() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [search] = useSearchParams()
  const runId = search.get('run')

  const { data, isLoading } = useQuery({
    queryKey: ['templates'],
    queryFn: () => get<{ items: Template[] }>('/api/v1/templates'),
  })
  const { data: groups } = useQuery({
    queryKey: ['groups-tree'],
    queryFn: () => get<{ tree: Group[] }>('/api/v1/groups'),
  })

  const [editing, setEditing] = useState<Template | null>(null)
  const [creating, setCreating] = useState(false)
  const [running, setRunning] = useState<Template | null>(
    runId ? data?.items.find((x) => x.id === runId) || null : null
  )

  const createMut = useMutation({
    mutationFn: (b: any) => post<Template>('/api/v1/templates', b),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['templates'] }); setCreating(false) },
  })
  const updateMut = useMutation({
    mutationFn: ({ id, body }: { id: string; body: any }) => patch<Template>(`/api/v1/templates/${id}`, body),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['templates'] }); setEditing(null) },
  })
  const delMut = useMutation({
    mutationFn: (id: string) => del(`/api/v1/templates/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['templates'] }),
  })

  // agrupar por categoría
  const byCategory: Record<string, Template[]> = {}
  for (const tpl of data?.items || []) {
    byCategory[tpl.category] = byCategory[tpl.category] || []
    byCategory[tpl.category].push(tpl)
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">{t('templates.title')}</h1>
        <button onClick={() => setCreating(true)} className="btn-primary">
          <Plus size={16} /> {t('templates.new')}
        </button>
      </div>

      {isLoading ? (
        <div className="text-slate-500 dark:text-slate-400 text-sm">{t('common.loading')}</div>
      ) : !data?.items?.length ? (
        <div className="text-slate-500 dark:text-slate-400 text-sm">{t('common.empty')}</div>
      ) : (
        <div className="space-y-4">
          {Object.entries(byCategory).map(([cat, items]) => (
            <section key={cat} className="card p-4">
              <h2 className="text-sm font-semibold text-slate-700 dark:text-slate-300 mb-2 uppercase tracking-wide">{cat}</h2>
              <table className="table">
                <thead>
                  <tr>
                    <th>{t('templates.name')}</th>
                    <th>{t('templates.command')}</th>
                    <th>{t('templates.timeout')}</th>
                    <th></th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((tpl) => (
                    <tr key={tpl.id} className="hover:bg-slate-50 dark:hover:bg-slate-800/60">
                      <td>
                        <div className="font-medium">{tpl.name}</div>
                        {tpl.description && <div className="text-xs text-slate-500 dark:text-slate-400">{tpl.description}</div>}
                      </td>
                      <td className="font-mono text-xs">
                        {tpl.command} {tpl.args?.length ? ' ' + tpl.args.join(' ') : ''}
                      </td>
                      <td className="text-xs">{tpl.timeout_seconds}s</td>
                      <td>
                        <button onClick={() => setRunning(tpl)} className="btn-secondary text-xs">
                          <Play size={12} /> {t('templates.run')}
                        </button>
                      </td>
                      <td className="text-right whitespace-nowrap">
                        {tpl.is_builtin && <span title={t('templates.builtin')}><Lock size={12} className="inline mr-2 text-slate-400 dark:text-slate-500" /></span>}
                        {!tpl.is_builtin && (
                          <button
                            onClick={() => setEditing(tpl)}
                            title={t('common.edit')}
                            className="inline-flex items-center gap-1 text-xs text-brand-700 hover:underline mr-2 dark:text-brand-300"
                          >
                            <Pencil size={12} />
                            <span>{t('common.edit')}</span>
                          </button>
                        )}
                        {!tpl.is_builtin && (
                          <button
                            onClick={() => { if (confirm(t('common.confirm_delete'))) delMut.mutate(tpl.id) }}
                            title={t('common.delete')}
                            className="inline-flex items-center text-xs text-red-700 hover:underline dark:text-red-400"
                          >
                            <Trash2 size={12} />
                          </button>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </section>
          ))}
        </div>
      )}

      {(creating || editing) && (
        <TemplateModal
          title={creating ? t('templates.new') : t('templates.edit')}
          initial={editing || undefined}
          onClose={() => { setCreating(false); setEditing(null) }}
          onSubmit={(b) => {
            if (creating) createMut.mutate(b)
            else if (editing) updateMut.mutate({ id: editing.id, body: b })
          }}
        />
      )}
      {running && (
        <RunModal
          template={running}
          groups={groups?.tree || []}
          onClose={() => setRunning(null)}
          onSubmit={async (b) => {
            await post('/api/v1/templates/' + running.id + '/run', b)
            setRunning(null)
            qc.invalidateQueries({ queryKey: ['jobs'] })
          }}
        />
      )}
    </div>
  )
}

function TemplateModal({ title, initial, onClose, onSubmit }: {
  title: string
  initial?: Template
  onClose: () => void
  onSubmit: (body: any) => void
}) {
  const { t } = useTranslation()
  const [name, setName] = useState(initial?.name || '')
  const [desc, setDesc] = useState(initial?.description || '')
  const [cat, setCat] = useState(initial?.category || 'general')
  const [cmd, setCmd] = useState(initial?.command || '')
  const [args, setArgs] = useState((initial?.args || []).join(' '))
  const [timeout, setTimeout] = useState(initial?.timeout_seconds || 60)
  const [elev, setElev] = useState(initial?.requires_elevation || false)
  const [confirm, setConfirm] = useState(initial?.requires_confirm ?? true)
  const [dash, setDash] = useState(initial?.show_in_dashboard || false)

  return (
    <div className="fixed inset-0 bg-black/40 grid place-items-center z-50 p-4">
      <div className="bg-white dark:bg-slate-800 rounded-xl shadow-xl w-full max-w-lg p-5 space-y-3 max-h-[90vh] overflow-y-auto">
        <h2 className="text-lg font-semibold">{title}</h2>
        <div className="grid grid-cols-2 gap-3">
          <div className="col-span-2"><label className="label">{t('templates.name')}</label>
            <input className="input" value={name} onChange={(e) => setName(e.target.value)} /></div>
          <div><label className="label">{t('templates.category')}</label>
            <input className="input" value={cat} onChange={(e) => setCat(e.target.value)} /></div>
          <div><label className="label">{t('templates.timeout')}</label>
            <input className="input" type="number" min={1} max={86400} value={timeout} onChange={(e) => setTimeout(+e.target.value)} /></div>
          <div className="col-span-2"><label className="label">{t('templates.command')}</label>
            <input className="input font-mono" value={cmd} onChange={(e) => setCmd(e.target.value)} /></div>
          <div className="col-span-2"><label className="label">{t('templates.args')}</label>
            <input className="input font-mono" value={args} onChange={(e) => setArgs(e.target.value)} placeholder="uno dos tres" /></div>
          <div className="col-span-2"><label className="label">Descripción</label>
            <textarea className="input" rows={2} value={desc} onChange={(e) => setDesc(e.target.value)} /></div>
          <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={elev} onChange={(e) => setElev(e.target.checked)} /> {t('templates.elevation')}</label>
          <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={confirm} onChange={(e) => setConfirm(e.target.checked)} /> {t('templates.confirm')}</label>
          <label className="flex items-center gap-2 text-sm col-span-2"><input type="checkbox" checked={dash} onChange={(e) => setDash(e.target.checked)} /> {t('templates.show_in_dashboard')}</label>
        </div>
        <div className="flex justify-end gap-2 pt-2">
          <button className="btn-secondary" onClick={onClose}>{t('common.cancel')}</button>
          <button
            className="btn-primary"
            disabled={!name.trim() || !cmd.trim()}
            onClick={() => onSubmit({
              name: name.trim(),
              description: desc,
              category: cat,
              command: cmd.trim(),
              args: args.split(/\s+/).filter(Boolean),
              timeout_seconds: timeout,
              requires_elevation: elev,
              requires_confirm: confirm,
              show_in_dashboard: dash,
            })}
          >{t('common.save')}</button>
        </div>
      </div>
    </div>
  )
}

function RunModal({ template, groups, onClose, onSubmit }: {
  template: Template
  groups: Group[]
  onClose: () => void
  onSubmit: (b: any) => void | Promise<void>
}) {
  const { t } = useTranslation()
  const [name, setName] = useState(`Run: ${template.name}`)
  const [targetType, setTargetType] = useState<'all' | 'group'>('all')
  const [targetId, setTargetId] = useState('')

  return (
    <div className="fixed inset-0 bg-black/40 grid place-items-center z-50 p-4">
      <div className="bg-white dark:bg-slate-800 rounded-xl shadow-xl w-full max-w-md p-5 space-y-3">
        <h2 className="text-lg font-semibold">{t('templates.run_on.title')}</h2>
        <div className="text-sm text-slate-600 dark:text-slate-300">{template.name}</div>
        <div>
          <label className="label">{t('templates.run_on.name')}</label>
          <input className="input" value={name} onChange={(e) => setName(e.target.value)} />
        </div>
        <div>
          <label className="label">{t('templates.run_on.target')}</label>
          <select className="input" value={targetType} onChange={(e) => setTargetType(e.target.value as any)}>
            <option value="all">{t('templates.run_on.target.all')}</option>
            <option value="group">{t('templates.run_on.target.group')}</option>
          </select>
        </div>
        {targetType === 'group' && (
          <div>
            <label className="label">{t('groups.title')}</label>
            <select className="input" value={targetId} onChange={(e) => setTargetId(e.target.value)}>
              <option value="">—</option>
              {groups.map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}
            </select>
          </div>
        )}
        <div className="text-xs text-amber-700 bg-amber-50 border border-amber-200 rounded px-2 py-1 dark:bg-amber-950/40 dark:border-amber-900 dark:text-amber-300">
          {t('jobs.phase_notice')}
        </div>
        <div className="flex justify-end gap-2 pt-2">
          <button className="btn-secondary" onClick={onClose}>{t('common.cancel')}</button>
          <button
            className="btn-primary"
            disabled={!name.trim() || (targetType === 'group' && !targetId)}
            onClick={() => onSubmit({ name, target_type: targetType, target_id: targetType === 'group' ? targetId : null })}
          >{t('templates.run_on.submit')}</button>
        </div>
      </div>
    </div>
  )
}