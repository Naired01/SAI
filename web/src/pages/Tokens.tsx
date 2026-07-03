import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Plus, Copy, Check, Trash2 } from 'lucide-react'
import { get, post, del, type Token } from '../lib/api'
import { StatusBadge } from '../components/StatusBadge'

export function Tokens() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data, isLoading } = useQuery({
    queryKey: ['tokens'],
    queryFn: () => get<{ items: Token[] }>('/api/v1/tokens'),
  })

  const [creating, setCreating] = useState(false)
  const [justCreated, setJustCreated] = useState<{ plain: string; download_url: string; token: Token } | null>(null)
  const [copied, setCopied] = useState(false)

  const createMut = useMutation({
    mutationFn: (b: any) => post<{ token: Token; plain: string; download_url: string }>('/api/v1/tokens', b),
    onSuccess: (r) => {
      setJustCreated(r)
      setCreating(false)
      qc.invalidateQueries({ queryKey: ['tokens'] })
    },
  })
  const revokeMut = useMutation({
    mutationFn: (id: string) => post(`/api/v1/tokens/${id}/revoke`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['tokens'] }),
  })

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">{t('tokens.title')}</h1>
        <button className="btn-primary" onClick={() => setCreating(true)}>
          <Plus size={16} /> {t('tokens.new')}
        </button>
      </div>

      <div className="card overflow-hidden">
        <table className="table">
          <thead>
            <tr>
              <th>{t('tokens.label')}</th>
              <th>{t('tokens.uses')}</th>
              <th>{t('tokens.expires')}</th>
              <th>{t('tokens.created')}</th>
              <th>{t('agents.column.status')}</th>
              <th></th>
            </tr>
          </thead>
              <tbody>
                {isLoading ? (
                  <tr><td colSpan={6} className="text-center text-slate-500 dark:text-slate-400">{t('common.loading')}</td></tr>
                ) : !data?.items?.length ? (
                  <tr><td colSpan={6} className="text-center text-slate-500 dark:text-slate-400">{t('common.empty')}</td></tr>
                ) : data.items.map((tok) => {
                  const status = tokenStatus(tok)
                  return (
                    <tr key={tok.id} className="hover:bg-slate-50 dark:hover:bg-slate-700/40">
                      <td className="font-medium">{tok.label}</td>
                      <td className="text-xs">{tok.uses}/{tok.max_uses}</td>
                      <td className="text-xs">{tok.expires_at || '—'}</td>
                      <td className="text-xs text-slate-500 dark:text-slate-400">{tok.created_at}</td>
                      <td><StatusBadge kind="token" value={status} /></td>
                      <td>
                        {!tok.revoked_at && (
                          <button onClick={() => { if (confirm(t('tokens.revoke.confirm'))) revokeMut.mutate(tok.id) }} className="inline-flex items-center text-xs text-red-700 hover:underline dark:text-red-400" title={t('tokens.revoke')}>
                            <Trash2 size={12} />
                          </button>
                        )}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
        </table>
      </div>

      {creating && (
        <CreateModal
          onClose={() => setCreating(false)}
          onSubmit={(b) => createMut.mutate(b)}
        />
      )}
      {justCreated && (
        <div className="fixed inset-0 bg-black/40 grid place-items-center z-50 p-4" onClick={() => setJustCreated(null)}>
          <div className="bg-white dark:bg-slate-800 rounded-xl shadow-xl w-full max-w-lg p-5 space-y-3" onClick={(e) => e.stopPropagation()}>
            <h2 className="text-lg font-semibold">{t('tokens.new')}</h2>
            <div className="text-sm text-amber-700 bg-amber-50 border border-amber-200 rounded px-3 py-2 dark:bg-amber-950/40 dark:border-amber-900 dark:text-amber-300">
              {t('tokens.plain_notice')}
            </div>
            <div>
              <label className="label">Plain token</label>
              <div className="flex gap-2">
                <input className="input font-mono text-xs" readOnly value={justCreated.plain} />
                <button
                  className="btn-secondary"
                  onClick={() => { navigator.clipboard.writeText(justCreated.plain); setCopied(true); setTimeout(() => setCopied(false), 1500) }}
                >
                  {copied ? <Check size={14} /> : <Copy size={14} />} {copied ? t('tokens.copied') : t('tokens.copy')}
                </button>
              </div>
            </div>
            <div>
              <label className="label">{t('tokens.download_url')}</label>
              <div className="font-mono text-xs break-all bg-slate-50 p-2 rounded border border-slate-200 dark:bg-slate-900 dark:border-slate-700">
                {window.location.origin + justCreated.download_url}
              </div>
            </div>
            <div className="flex justify-end">
              <button className="btn-secondary" onClick={() => setJustCreated(null)}>{t('common.close')}</button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

function tokenStatus(tok: Token): string {
  if (tok.revoked_at) return 'revoked'
  if (tok.expires_at && new Date(tok.expires_at) < new Date()) return 'expired'
  if (tok.uses >= tok.max_uses) return 'exhausted'
  return 'active'
}

function CreateModal({ onClose, onSubmit }: { onClose: () => void; onSubmit: (b: any) => void }) {
  const { t } = useTranslation()
  const [label, setLabel] = useState('')
  const [maxUses, setMaxUses] = useState(1)
  const [ttlHours, setTtlHours] = useState(24)

  return (
    <div className="fixed inset-0 bg-black/40 grid place-items-center z-50 p-4">
      <div className="bg-white dark:bg-slate-800 rounded-xl shadow-xl w-full max-w-md p-5 space-y-3">
        <h2 className="text-lg font-semibold">{t('tokens.new')}</h2>
        <div>
          <label className="label">{t('tokens.label')}</label>
          <input className="input" value={label} onChange={(e) => setLabel(e.target.value)} placeholder="Laptop marketing" />
        </div>
        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className="label">{t('tokens.max_uses')}</label>
            <input className="input" type="number" min={1} value={maxUses} onChange={(e) => setMaxUses(+e.target.value)} />
          </div>
          <div>
            <label className="label">{t('tokens.ttl_hours')}</label>
            <input className="input" type="number" min={1} value={ttlHours} onChange={(e) => setTtlHours(+e.target.value)} />
          </div>
        </div>
        <div className="flex justify-end gap-2">
          <button className="btn-secondary" onClick={onClose}>{t('common.cancel')}</button>
          <button className="btn-primary" disabled={!label.trim()} onClick={() => onSubmit({ label: label.trim(), max_uses: maxUses, ttl_hours: ttlHours })}>
            {t('tokens.create')}
          </button>
        </div>
      </div>
    </div>
  )
}