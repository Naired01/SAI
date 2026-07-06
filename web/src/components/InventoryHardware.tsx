import { useTranslation } from 'react-i18next'
import {
  Cpu,
  HardDrive,
  MemoryStick,
  Monitor,
  Network,
  Server,
} from 'lucide-react'
import type { InventoryHardware as Hardware } from '../lib/api'
import { formatBytes, formatPercent, formatUptime } from '../lib/format'

type Props = {
  hardware: Hardware
  collectedAt?: string
  agentVersion?: string
  error?: string
}

export function InventoryHardware({ hardware, collectedAt, agentVersion, error }: Props) {
  const { t } = useTranslation()
  const memUsedPct = formatPercent(hardware.memory?.used_bytes ?? 0, hardware.memory?.total_bytes ?? 0)

  return (
    <div className="space-y-4">
      {error && (
        <div className="rounded border border-amber-300 bg-amber-50 dark:bg-amber-900/30 dark:border-amber-700 p-3 text-sm">
          <strong className="font-medium">{t('inventory.partial')}:</strong> {error}
        </div>
      )}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <Section title={t('inventory.host.title')} icon={<Monitor size={16} />}>
          <Row label={t('inventory.host.hostname')} value={hardware.host?.hostname ?? '—'} />
          <Row label={t('inventory.host.os')} value={`${hardware.host?.os ?? '—'} (${hardware.host?.platform ?? '—'})`} />
          <Row label={t('inventory.host.arch')} value={hardware.host?.kernel_arch ?? '—'} />
          {hardware.host?.kernel_ver && (
            <Row label={t('inventory.host.kernel')} value={hardware.host.kernel_ver} mono />
          )}
          <Row
            label={t('inventory.host.uptime')}
            value={formatUptime(hardware.host?.uptime_secs ?? 0)}
          />
        </Section>

        <Section title={t('inventory.cpu.title')} icon={<Cpu size={16} />}>
          {hardware.cpu && hardware.cpu.length > 0 ? (
            hardware.cpu.map((c, i) => (
              <div key={i}>
                <Row label={t('inventory.cpu.model')} value={c.model_name || '—'} />
                <Row label={t('inventory.cpu.cores')} value={String(c.cores || '—')} />
                <Row label={t('inventory.cpu.mhz')} value={c.mhz ? `${Math.round(c.mhz)} MHz` : '—'} />
                {c.vendor && <Row label={t('inventory.cpu.vendor')} value={c.vendor} />}
              </div>
            ))
          ) : (
            <Row label="—" value={t('common.empty')} />
          )}
        </Section>

        <Section title={t('inventory.memory.title')} icon={<MemoryStick size={16} />}>
          <Row label={t('inventory.memory.total')} value={formatBytes(hardware.memory?.total_bytes ?? 0)} />
          <Row label={t('inventory.memory.used')} value={formatBytes(hardware.memory?.used_bytes ?? 0)} />
          <Row label={t('inventory.memory.avail')} value={formatBytes(hardware.memory?.available_bytes ?? 0)} />
          <Row label={t('inventory.memory.usage')} value={memUsedPct} />
        </Section>

        <Section title={t('inventory.disks.title')} icon={<HardDrive size={16} />}>
          {hardware.disks && hardware.disks.length > 0 ? (
            hardware.disks.map((d, i) => {
              const usedPct = formatPercent(d.used_bytes, d.total_bytes)
              return (
                <div key={i} className="border-t first:border-t-0 border-gray-200 dark:border-gray-700 pt-2 first:pt-0">
                  <Row label={t('inventory.disks.device')} value={`${d.device} (${d.fs_type})`} mono />
                  <Row label={t('inventory.disks.mount')} value={d.mountpoint} mono />
                  <Row
                    label={t('inventory.disks.usage')}
                    value={`${formatBytes(d.used_bytes)} / ${formatBytes(d.total_bytes)} (${usedPct})`}
                  />
                </div>
              )
            })
          ) : (
            <Row label="—" value={t('common.empty')} />
          )}
        </Section>

        <Section title={t('inventory.network.title')} icon={<Network size={16} />} wide>
          {hardware.network && hardware.network.length > 0 ? (
            <table className="w-full text-sm">
              <thead className="text-left text-gray-500 dark:text-gray-400">
                <tr>
                  <th className="py-1 pr-2 font-normal">{t('inventory.network.name')}</th>
                  <th className="py-1 pr-2 font-normal">{t('inventory.network.mac')}</th>
                  <th className="py-1 pr-2 font-normal">{t('inventory.network.state')}</th>
                  <th className="py-1 pr-2 font-normal">{t('inventory.network.addrs')}</th>
                </tr>
              </thead>
              <tbody>
                {hardware.network.map((n, i) => (
                  <tr key={i} className="border-t border-gray-200 dark:border-gray-700">
                    <td className="py-1 pr-2 font-mono">{n.name}</td>
                    <td className="py-1 pr-2 font-mono text-xs">{n.hardware_addr || '—'}</td>
                    <td className="py-1 pr-2">{n.state || '—'}</td>
                    <td className="py-1 pr-2 font-mono text-xs">
                      {n.addrs && n.addrs.length > 0 ? n.addrs.join(', ') : '—'}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : (
            <Row label="—" value={t('common.empty')} />
          )}
        </Section>
      </div>

      {(collectedAt || agentVersion) && (
        <div className="text-xs text-gray-500 dark:text-gray-400 flex items-center gap-2 pt-2">
          <Server size={12} />
          {collectedAt && (
            <span>
              {t('inventory.collected_at', {
                value: new Date(collectedAt).toLocaleString(),
              })}
            </span>
          )}
          {agentVersion && (
            <span className="font-mono">v{agentVersion}</span>
          )}
        </div>
      )}
    </div>
  )
}

function Section({
  title,
  icon,
  children,
  wide,
}: {
  title: string
  icon: React.ReactNode
  children: React.ReactNode
  wide?: boolean
}) {
  return (
    <div
      className={`rounded border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800 p-4 ${
        wide ? 'md:col-span-2' : ''
      }`}
    >
      <div className="flex items-center gap-2 text-sm font-medium text-gray-700 dark:text-gray-300 mb-3">
        {icon}
        <span>{title}</span>
      </div>
      <div className="space-y-1">{children}</div>
    </div>
  )
}

function Row({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex justify-between items-baseline gap-3 text-sm">
      <span className="text-gray-500 dark:text-gray-400 shrink-0">{label}</span>
      <span className={`text-right ${mono ? 'font-mono text-xs' : ''} text-gray-900 dark:text-gray-100 truncate`} title={value}>
        {value}
      </span>
    </div>
  )
}
