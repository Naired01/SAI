import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Package, AlertCircle, RefreshCw, Search } from 'lucide-react'
import type { InventorySoftware as Software } from '../lib/api'

type Props = {
  software?: Software
}

type SubTab = 'packages' | 'services' | 'updates'

export function InventorySoftware({ software }: Props) {
  const { t } = useTranslation()
  const [tab, setTab] = useState<SubTab>('packages')
  const [q, setQ] = useState('')
  const [sourceFilter, setSourceFilter] = useState<string>('')

  const packages = software?.packages ?? []
  const services = software?.services ?? []
  const updates = software?.updates ?? []

  const sources = useMemo(() => {
    const set = new Set<string>()
    packages.forEach((p) => p.source && set.add(p.source))
    services.forEach((s) => s.source && set.add(s.source))
    return Array.from(set).sort()
  }, [packages, services])

  const filteredPackages = packages
    .filter((p) => (q ? p.name.toLowerCase().includes(q.toLowerCase()) : true))
    .filter((p) => (sourceFilter ? p.source === sourceFilter : true))
    .sort((a, b) => a.name.localeCompare(b.name))

  const filteredServices = services
    .filter((s) => (q ? s.name.toLowerCase().includes(q.toLowerCase()) : true))
    .filter((s) => (sourceFilter ? s.source === sourceFilter : true))
    .sort((a, b) => a.name.localeCompare(b.name))

  const filteredUpdates = updates
    .filter((u) => (q ? u.name.toLowerCase().includes(q.toLowerCase()) : true))
    .sort((a, b) => a.name.localeCompare(b.name))

  if (!software || (packages.length + services.length + updates.length === 0)) {
    return (
      <div className="card p-8 text-center space-y-3">
        <div className="flex justify-center text-slate-400 dark:text-slate-500">
          <Package size={32} />
        </div>
        <div className="text-slate-500 dark:text-slate-400 text-sm">
          {t('inventory.software.empty.title')}
        </div>
        <div className="text-slate-400 dark:text-slate-500 text-xs">
          {t('inventory.software.empty.hint')}
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      {/* KPIs */}
      <div className="grid grid-cols-3 gap-3">
        <KpiCard label={t('inventory.software.packages')} value={packages.length} />
        <KpiCard label={t('inventory.software.services')} value={services.length} />
        <KpiCard
          label={t('inventory.software.updates')}
          value={updates.length}
          accent={updates.length > 0 ? 'amber' : 'default'}
        />
      </div>

      {/* Tabs */}
      <div className="border-b border-slate-200 flex gap-1 dark:border-slate-700">
        <SubTabBtn active={tab === 'packages'} onClick={() => setTab('packages')} disabled={packages.length === 0}>
          {t('inventory.software.packages')} {packages.length > 0 && <span className="text-xs text-slate-400">({packages.length})</span>}
        </SubTabBtn>
        <SubTabBtn active={tab === 'services'} onClick={() => setTab('services')} disabled={services.length === 0}>
          {t('inventory.software.services')} {services.length > 0 && <span className="text-xs text-slate-400">({services.length})</span>}
        </SubTabBtn>
        <SubTabBtn active={tab === 'updates'} onClick={() => setTab('updates')} disabled={updates.length === 0}>
          {t('inventory.software.updates')} {updates.length > 0 && <span className="text-xs text-slate-400">({updates.length})</span>}
        </SubTabBtn>
      </div>

      {/* Search + source filter */}
      <div className="flex gap-2 flex-wrap items-center">
        <label className="relative flex items-center flex-1 min-w-[200px]">
          <Search size={14} className="absolute left-2 text-slate-400 pointer-events-none" />
          <input
            type="text"
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder={t('common.search')}
            className="w-full pl-7 pr-2 py-1.5 text-sm rounded border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-800 focus:outline-none focus:ring-1 focus:ring-brand-500"
          />
        </label>
        {sources.length > 1 && (
          <select
            value={sourceFilter}
            onChange={(e) => setSourceFilter(e.target.value)}
            className="px-2 py-1.5 text-sm rounded border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-800"
          >
            <option value="">{t('inventory.software.all_sources')}</option>
            {sources.map((s) => (
              <option key={s} value={s}>{s}</option>
            ))}
          </select>
        )}
      </div>

      {tab === 'packages' && <PackagesTable items={filteredPackages} emptyHint={t('inventory.software.packages_empty')} />}
      {tab === 'services' && <ServicesTable items={filteredServices} emptyHint={t('inventory.software.services_empty')} />}
      {tab === 'updates' && <UpdatesTable items={filteredUpdates} emptyHint={t('inventory.software.updates_empty')} />}
    </div>
  )
}

function KpiCard({ label, value, accent }: { label: string; value: number; accent?: 'amber' | 'default' }) {
  const color = accent === 'amber' ? 'text-amber-600 dark:text-amber-400' : 'text-slate-900 dark:text-slate-100'
  return (
    <div className="rounded border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 p-3">
      <div className="text-xs text-slate-500 dark:text-slate-400">{label}</div>
      <div className={`text-2xl font-semibold mt-1 ${color}`}>{value}</div>
    </div>
  )
}

function SubTabBtn({
  active,
  onClick,
  disabled,
  children,
}: {
  active: boolean
  onClick: () => void
  disabled: boolean
  children: React.ReactNode
}) {
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      className={`px-3 py-2 text-sm border-b-2 -mb-px whitespace-nowrap ${
        active
          ? 'border-brand-600 text-brand-700 font-medium dark:border-brand-400 dark:text-brand-300'
          : 'border-transparent text-slate-600 hover:text-slate-900 dark:text-slate-400 dark:hover:text-slate-100'
      } ${disabled ? 'opacity-40 cursor-not-allowed' : ''}`}
    >
      {children}
    </button>
  )
}

function PackagesTable({
  items,
  emptyHint,
}: {
  items: NonNullable<NonNullable<Software['packages']>>
  emptyHint: string
}) {
  const { t } = useTranslation()
  if (items.length === 0) {
    return <div className="card p-4 text-sm text-slate-500 dark:text-slate-400 text-center">{emptyHint}</div>
  }
  return (
    <div className="card overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="text-left text-slate-500 dark:text-slate-400 border-b border-slate-200 dark:border-slate-700">
            <th className="px-3 py-2 font-medium">{t('inventory.software.col.name')}</th>
            <th className="px-3 py-2 font-medium">{t('inventory.software.col.version')}</th>
            <th className="px-3 py-2 font-medium">{t('inventory.software.col.source')}</th>
            <th className="px-3 py-2 font-medium">{t('inventory.software.col.publisher')}</th>
          </tr>
        </thead>
        <tbody>
          {items.map((p, i) => (
            <tr key={`${p.name}-${i}`} className="border-b border-slate-100 dark:border-slate-800">
              <td className="px-3 py-1.5 font-mono text-xs">{p.name}</td>
              <td className="px-3 py-1.5 font-mono text-xs">{p.version || '—'}</td>
              <td className="px-3 py-1.5 text-xs text-slate-500 dark:text-slate-400">{p.source || '—'}</td>
              <td className="px-3 py-1.5 text-xs text-slate-500 dark:text-slate-400">{p.publisher || '—'}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function ServicesTable({
  items,
  emptyHint,
}: {
  items: NonNullable<NonNullable<Software['services']>>
  emptyHint: string
}) {
  const { t } = useTranslation()
  if (items.length === 0) {
    return <div className="card p-4 text-sm text-slate-500 dark:text-slate-400 text-center">{emptyHint}</div>
  }
  return (
    <div className="card overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="text-left text-slate-500 dark:text-slate-400 border-b border-slate-200 dark:border-slate-700">
            <th className="px-3 py-2 font-medium">{t('inventory.software.col.name')}</th>
            <th className="px-3 py-2 font-medium">{t('inventory.software.col.state')}</th>
            <th className="px-3 py-2 font-medium">{t('inventory.software.col.start_type')}</th>
            <th className="px-3 py-2 font-medium">{t('inventory.software.col.source')}</th>
          </tr>
        </thead>
        <tbody>
          {items.map((s, i) => (
            <tr key={`${s.name}-${i}`} className="border-b border-slate-100 dark:border-slate-800">
              <td className="px-3 py-1.5 font-mono text-xs">{s.name}</td>
              <td className="px-3 py-1.5">
                <span
                  className={`inline-block px-2 py-0.5 rounded text-xs font-medium ${
                    s.state === 'running'
                      ? 'bg-emerald-100 text-emerald-800 dark:bg-emerald-900/40 dark:text-emerald-300'
                      : 'bg-slate-100 text-slate-700 dark:bg-slate-700 dark:text-slate-300'
                  }`}
                >
                  {s.state}
                </span>
              </td>
              <td className="px-3 py-1.5 text-xs text-slate-500 dark:text-slate-400">{s.start_type || '—'}</td>
              <td className="px-3 py-1.5 text-xs text-slate-500 dark:text-slate-400">{s.source || '—'}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function UpdatesTable({
  items,
  emptyHint,
}: {
  items: NonNullable<NonNullable<Software['updates']>>
  emptyHint: string
}) {
  const { t } = useTranslation()
  if (items.length === 0) {
    return <div className="card p-4 text-sm text-slate-500 dark:text-slate-400 text-center">{emptyHint}</div>
  }
  return (
    <div className="card overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="text-left text-amber-700 dark:text-amber-400 border-b border-slate-200 dark:border-slate-700">
            <th className="px-3 py-2 font-medium flex items-center gap-1">
              <AlertCircle size={12} />
              {t('inventory.software.col.name')}
            </th>
            <th className="px-3 py-2 font-medium">{t('inventory.software.col.available_version')}</th>
            <th className="px-3 py-2 font-medium">{t('inventory.software.col.source')}</th>
          </tr>
        </thead>
        <tbody>
          {items.map((u, i) => (
            <tr key={`${u.name}-${i}`} className="border-b border-slate-100 dark:border-slate-800">
              <td className="px-3 py-1.5 font-mono text-xs">{u.name}</td>
              <td className="px-3 py-1.5 font-mono text-xs">{u.available_version || '—'}</td>
              <td className="px-3 py-1.5 text-xs text-slate-500 dark:text-slate-400">{u.source || '—'}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
