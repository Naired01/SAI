import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Plus, Trash2, FolderTree, Pencil } from 'lucide-react'
import { get, post, patch, del, type Group } from '../lib/api'

const COLORS = ['#327bff', '#10b981', '#f59e0b', '#ef4444', '#8b5cf6', '#ec4899', '#0ea5e9', '#22c55e']

export function Groups() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [edit, setEdit] = useState<Group | null>(null)

  const { data, isLoading } = useQuery({
    queryKey: ['groups-tree'],
    queryFn: () => get<{ tree: Group[] }>('/api/v1/groups'),
  })

  const createMut = useMutation({
    mutationFn: (body: { name: string; parent_id?: string; description?: string; color?: string }) =>
      post<Group>('/api/v1/groups', body),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['groups-tree'] }); setCreating(false) },
  })
  const updateMut = useMutation({
    mutationFn: ({ id, body }: { id: string; body: any }) => patch<Group>(`/api/v1/groups/${id}`, body),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['groups-tree'] }); setEdit(null) },
  })
  const delMut = useMutation({
    mutationFn: (id: string) => del<{ ok: boolean }>(`/api/v1/groups/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['groups-tree'] }),
  })

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">{t('groups.title')}</h1>
        <button onClick={() => setCreating(true)} className="btn-primary">
          <Plus size={16} /> {t('groups.new')}
        </button>
      </div>

      <div className="card p-4">
        {isLoading ? (
          <div className="text-sm text-slate-500 dark:text-slate-400">{t('common.loading')}</div>
        ) : !data?.tree?.length ? (
          <div className="text-sm text-slate-500 dark:text-slate-400">{t('groups.empty')}</div>
        ) : (
          <GroupList
            nodes={data.tree}
            depth={0}
            onEdit={setEdit}
            onDelete={(g) => {
              if (confirm(t('groups.confirm_delete'))) delMut.mutate(g.id)
            }}
          />
        )}
      </div>

      {creating && (
        <GroupModal
          title={t('groups.new')}
          groups={data?.tree || []}
          onClose={() => setCreating(false)}
          onSubmit={(body) => createMut.mutate(body)}
        />
      )}
      {edit && (
        <GroupModal
          title={t('groups.edit')}
          groups={data?.tree || []}
          initial={edit}
          onClose={() => setEdit(null)}
          onSubmit={(body) => updateMut.mutate({ id: edit.id, body })}
        />
      )}
    </div>
  )
}

function GroupList({ nodes, depth, onEdit, onDelete }: {
  nodes: Group[]
  depth: number
  onEdit: (g: Group) => void
  onDelete: (g: Group) => void
}) {
  const { t } = useTranslation()
  return (
    <ul className="space-y-1">
      {nodes.map((g) => (
        <li key={g.id}>
          <div
            style={{ paddingLeft: `${depth * 16 + 8}px` }}
            className="flex items-center gap-2 py-1.5 px-2 rounded hover:bg-slate-50 dark:hover:bg-slate-800"
          >
            <FolderTree size={14} style={{ color: g.color || '#64748b' }} />
            <span className="font-medium">{g.name}</span>
            <span className="text-xs text-slate-400 dark:text-slate-500">({g.member_count})</span>
            <div className="ml-auto flex gap-2">
              <button
                onClick={() => onEdit(g)}
                title={t('common.edit')}
                className="inline-flex items-center gap-1 text-xs text-brand-700 hover:underline dark:text-brand-300"
              >
                <Pencil size={12} />
                <span>{t('common.edit')}</span>
              </button>
              <button
                onClick={() => onDelete(g)}
                title={t('common.delete')}
                className="inline-flex items-center gap-1 text-xs text-red-700 hover:underline dark:text-red-400"
              >
                <Trash2 size={12} />
              </button>
            </div>
          </div>
          {g.children && g.children.length > 0 && (
            <GroupList nodes={g.children} depth={depth + 1} onEdit={onEdit} onDelete={onDelete} />
          )}
        </li>
      ))}
    </ul>
  )
}

function GroupModal({ title, groups, initial, onClose, onSubmit }: {
  title: string
  groups: Group[]
  initial?: Group
  onClose: () => void
  onSubmit: (body: any) => void
}) {
  const { t } = useTranslation()
  const [name, setName] = useState(initial?.name || '')
  const [parent, setParent] = useState(initial?.parent_id || '')
  const [color, setColor] = useState(initial?.color || COLORS[0])
  const [desc, setDesc] = useState(initial?.description || '')

  function flat(g: Group[], out: Group[] = []): Group[] {
    for (const x of g) {
      if (!initial || x.id !== initial.id) out.push(x)
      if (x.children) flat(x.children, out)
    }
    return out
  }

  return (
    <div className="fixed inset-0 bg-black/40 grid place-items-center z-50 p-4">
      <div className="bg-white dark:bg-slate-800 rounded-xl shadow-xl w-full max-w-md p-5 space-y-3">
        <h2 className="text-lg font-semibold">{title}</h2>
        <div>
          <label className="label">{t('groups.name')}</label>
          <input className="input" value={name} onChange={(e) => setName(e.target.value)} />
        </div>
        <div>
          <label className="label">{t('groups.parent')}</label>
          <select className="input" value={parent} onChange={(e) => setParent(e.target.value)}>
            <option value="">{t('groups.parent.none')}</option>
            {flat(groups).map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}
          </select>
        </div>
        <div>
          <label className="label">{t('groups.color')}</label>
          <div className="flex gap-2">
            {COLORS.map((c) => (
              <button
                key={c}
                type="button"
                onClick={() => setColor(c)}
                style={{ background: c }}
                className={`w-7 h-7 rounded-full border-2 ${color === c ? 'border-slate-900 dark:border-white' : 'border-transparent'}`}
              />
            ))}
          </div>
        </div>
        <div>
          <label className="label">{t('groups.description')}</label>
          <textarea className="input" rows={2} value={desc} onChange={(e) => setDesc(e.target.value)} />
        </div>
        <div className="flex justify-end gap-2 pt-2">
          <button className="btn-secondary" onClick={onClose}>{t('common.cancel')}</button>
          <button
            className="btn-primary"
            disabled={!name.trim()}
            onClick={() => onSubmit({ name: name.trim(), parent_id: parent || null, color, description: desc })}
          >
            {t('common.save')}
          </button>
        </div>
      </div>
    </div>
  )
}