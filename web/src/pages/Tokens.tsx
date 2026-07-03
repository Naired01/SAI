import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Plus, Copy, Check, Trash2, Download, Monitor, Apple, Terminal } from 'lucide-react'
import clsx from 'clsx'
import { get, post, del, type Token, type PlatformUrl } from '../lib/api'
import { StatusBadge } from '../components/StatusBadge'

// detectClientPlatform intenta adivinar el SO + arch del navegador que
// corre el panel, para resaltar el download más probable. Es solo UX;
// el admin puede elegir cualquier plataforma igualmente.
function detectClientPlatform(): { os: 'windows' | 'linux' | 'darwin'; arch: 'amd64' | 'arm64' } {
  const ua = (typeof navigator !== 'undefined' ? navigator.userAgent : '').toLowerCase()
  const uaData = (typeof navigator !== 'undefined' ? (navigator as any).userAgentData : null)
  const platform = (uaData?.platform || (typeof navigator !== 'undefined' ? (navigator as any).platform : '') || '').toLowerCase()

  let os: 'windows' | 'linux' | 'darwin' = 'linux'
  if (ua.includes('win') || platform.includes('win')) os = 'windows'
  else if (ua.includes('mac') || ua.includes('darwin') || platform.includes('mac')) os = 'darwin'
  else os = 'linux'

  // arm64 solo lo exponen navegadores modernos vía userAgentData
  let arch: 'amd64' | 'arm64' = 'amd64'
  if (uaData?.platform === 'macOS' && uaData?.architecture) {
    if (uaData.architecture === 'arm') arch = 'arm64'
  } else if (platform.includes('arm') || platform.includes('aarch64')) {
    arch = 'arm64'
  }
  return { os, arch }
}

export function Tokens() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data, isLoading } = useQuery({
    queryKey: ['tokens'],
    queryFn: () => get<{ items: Token[] }>('/api/v1/tokens'),
  })

  const [creating, setCreating] = useState(false)
  const [justCreated, setJustCreated] = useState<{ plain: string; download_urls: PlatformUrl[]; token: Token } | null>(null)
  const [copied, setCopied] = useState(false)
  const clientPlat = detectClientPlatform()

  const createMut = useMutation({
    mutationFn: (b: any) => post<{ token: Token; plain: string; download_urls: PlatformUrl[] }>('/api/v1/tokens', b),
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
          <div className="bg-white dark:bg-slate-800 rounded-xl shadow-xl w-full max-w-2xl p-5 space-y-4" onClick={(e) => e.stopPropagation()}>
            <h2 className="text-lg font-semibold">{t('tokens.new')}</h2>
            <div className="text-sm text-amber-700 bg-amber-50 border border-amber-200 rounded px-3 py-2 dark:bg-amber-950/40 dark:border-amber-900 dark:text-amber-300">
              {t('tokens.plain_notice')}
            </div>

            {/* Plain token con botón copiar */}
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

            {/* Grid de descarga por plataforma */}
            <div>
              <label className="label">{t('tokens.download_for_platform')}</label>
              <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
                {(['windows', 'linux', 'darwin'] as const).map((os) => (
                  <div key={os} className="card p-3">
                    <div className="flex items-center gap-2 text-sm font-semibold mb-2">
                      {os === 'windows' && <Monitor size={16} className="text-blue-600" />}
                      {os === 'linux' && <Terminal size={16} className="text-orange-600" />}
                      {os === 'darwin' && <Apple size={16} className="text-slate-600" />}
                      {t(`tokens.platform.${os}`)}
                    </div>
                    <div className="space-y-1.5">
                      {(['amd64', 'arm64'] as const).map((arch) => {
                        const link = justCreated.download_urls.find((p) => p.os === os && p.arch === arch)
                        if (!link) return null
                        const isClient = os === clientPlat.os && arch === clientPlat.arch
                        return (
                          <a
                            key={arch}
                            href={window.location.origin + link.url}
                            target="_blank"
                            rel="noopener"
                            download
                            className={clsx(
                              'flex items-center justify-between gap-2 px-2.5 py-1.5 rounded text-xs border transition',
                              isClient
                                ? 'bg-brand-50 border-brand-300 text-brand-800 hover:bg-brand-100'
                                : 'bg-white border-slate-200 text-slate-700 hover:bg-slate-50'
                            )}
                          >
                            <span className="flex items-center gap-1.5">
                              <Download size={12} /> {arch}
                              {isClient && (
                                <span className="badge bg-brand-600 text-white text-[10px] px-1.5 py-0">
                                  {t('tokens.platform.detected')}
                                </span>
                              )}
                            </span>
                            <span className="font-mono text-[10px] text-slate-400">sai-agent-{os}-{arch}{os === 'windows' ? '.exe' : ''}</span>
                          </a>
                        )
                      })}
                    </div>
                  </div>
                ))}
              </div>
              <p className="text-xs text-slate-500 mt-2">{t('tokens.platform.help')}</p>
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