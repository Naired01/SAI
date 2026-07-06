// Helpers de formateo cross-página. Sin estado. Puros.

export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  let v = bytes
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  const fixed = i === 0 ? v.toFixed(0) : v.toFixed(v < 10 ? 1 : 0)
  return `${fixed} ${units[i]}`
}

export function formatPercent(used: number, total: number): string {
  if (!Number.isFinite(used) || !Number.isFinite(total) || total <= 0) return '—'
  return `${Math.round((used / total) * 100)}%`
}

export function formatUptime(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) return '—'
  const s = Math.floor(seconds)
  const days = Math.floor(s / 86400)
  const hours = Math.floor((s % 86400) / 3600)
  const minutes = Math.floor((s % 3600) / 60)
  const secs = s % 60
  const parts: string[] = []
  if (days > 0) parts.push(`${days}d`)
  if (hours > 0) parts.push(`${hours}h`)
  if (minutes > 0) parts.push(`${minutes}m`)
  if (parts.length === 0) parts.push(`${secs}s`)
  return parts.join(' ')
}

export function formatDate(iso: string | undefined): string {
  if (!iso) return '—'
  try {
    const d = new Date(iso)
    return d.toLocaleString()
  } catch {
    return iso
  }
}

export function formatRelativeFromNow(iso: string | undefined, nowMs: number = Date.now()): string {
  if (!iso) return '—'
  const t = new Date(iso).getTime()
  if (!Number.isFinite(t)) return iso
  const diffSec = Math.round((t - nowMs) / 1000)
  const abs = Math.abs(diffSec)
  if (abs < 5) return 'ahora'
  const sign = diffSec >= 0 ? 'en' : 'hace'
  if (abs < 60) return `${sign} ${abs}s`
  const m = Math.round(abs / 60)
  if (m < 60) return `${sign} ${m}m`
  const h = Math.round(m / 60)
  if (h < 24) return `${sign} ${h}h`
  const d = Math.round(h / 24)
  return `${sign} ${d}d`
}
