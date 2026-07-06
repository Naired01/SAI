// Wrapper sobre fetch que:
//  - Envía cookies (credentials: include)
//  - Adjunta CSRF token para mutaciones
//  - Parsea errores JSON estándar {code, message}

let csrfToken: string | null = null

export function setCsrf(t: string | null) {
  csrfToken = t
}

export type ApiError = { code: string; message: string }

export class ApiException extends Error {
  status: number
  body: ApiError
  constructor(status: number, body: ApiError) {
    super(body.message || body.code || 'API error')
    this.status = status
    this.body = body
  }
}

export async function api<T = unknown>(
  path: string,
  init: RequestInit = {},
): Promise<T> {
  const headers = new Headers(init.headers)
  if (init.body && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }
  const method = (init.method || 'GET').toUpperCase()
  if (method !== 'GET' && method !== 'HEAD' && csrfToken) {
    headers.set('X-CSRF-Token', csrfToken)
  }
  const res = await fetch(path, {
    ...init,
    headers,
    credentials: 'include',
  })
  if (!res.ok) {
    let body: ApiError = { code: 'unknown', message: res.statusText }
    try {
      body = await res.json()
    } catch {}
    throw new ApiException(res.status, body)
  }
  if (res.status === 204) return undefined as T
  const ct = res.headers.get('content-type') || ''
  if (ct.includes('application/json')) {
    return (await res.json()) as T
  }
  return (await res.text()) as unknown as T
}

// Helpers ----------------------------------------------------------

export const get = <T>(path: string) => api<T>(path)
export const post = <T>(path: string, body?: unknown) =>
  api<T>(path, { method: 'POST', body: body !== undefined ? JSON.stringify(body) : undefined })
export const patch = <T>(path: string, body?: unknown) =>
  api<T>(path, { method: 'PATCH', body: body !== undefined ? JSON.stringify(body) : undefined })
export const del = <T>(path: string) => api<T>(path, { method: 'DELETE' })

// Types -------------------------------------------------------------

export type Agent = {
  id: string
  hostname: string
  os: string
  os_version?: string
  arch?: string
  agent_version?: string
  labels: Record<string, unknown>
  visibility: 'visible' | 'invisible'
  last_seen_at?: string
  first_seen_at: string
  online: boolean
  group_ids: string[]
}

export type Group = {
  id: string
  parent_id?: string | null
  name: string
  description?: string
  color?: string
  icon?: string
  sort_order: number
  member_count: number
  children?: Group[]
}

export type Template = {
  id: string
  name: string
  description?: string
  category: string
  command: string
  args: string[]
  working_dir?: string
  timeout_seconds: number
  requires_elevation: boolean
  requires_confirm: boolean
  is_builtin: boolean
  show_in_dashboard: boolean
  icon?: string
}

export type Job = {
  id: string
  name: string
  description?: string
  source: 'template' | 'inline'
  template_id?: string
  inline_command?: string
  target_type: 'agent' | 'group' | 'all'
  status:
    | 'pending' | 'dispatching' | 'running'
    | 'completed' | 'failed' | 'partial' | 'cancelled'
  total_items: number
  pending_items: number
  success_items: number
  failed_items: number
  created_by: string
  created_at: string
  started_at?: string
  completed_at?: string
}

export type JobItem = {
  id: string
  job_id: string
  agent_id: string
  agent_hostname?: string
  agent_os?: string
  status: string
  exit_code?: number
  stdout?: string
  stderr?: string
  error_msg?: string
  started_at?: string
  completed_at?: string
}

export type Token = {
  id: string
  label: string
  max_uses: number
  uses: number
  expires_at?: string
  revoked_at?: string
  created_at: string
  has_uses_left: boolean
}

export type PlatformUrl = {
  os: 'windows' | 'linux' | 'darwin'
  arch: 'amd64' | 'arm64'
  url: string
}

export type TokenCreateResponse = {
  token: Token
  plain: string
  download_urls: PlatformUrl[]
}

export type AuditEvent = {
  id: number
  occurred_at: string
  actor_type: string
  actor_id?: string
  actor_label: string
  action: string
  target_type?: string
  target_id?: string
  target_label?: string
  ip?: string
  user_agent?: string
  metadata: Record<string, unknown>
}

export type DashboardSummary = {
  kpis: {
    agents_online: number
    agents_offline: number
    agents_problem: number
    active_tokens: number
    running_jobs: number
  }
  problem_agents: Agent[]
  quick_actions: Template[]
  recent_jobs: Job[]
}

export type Me = { id: string; email: string; role: string }

// Phase 2 — inventory types ---------------------------------------

export type Host = {
  hostname: string
  os: string
  platform: string
  kernel_arch: string
  kernel_ver?: string
  uptime_secs: number
  boot_time?: string
}

export type CPUSlot = {
  model_name: string
  vendor?: string
  family?: string
  model?: string
  cores: number
  mhz: number
}

export type MemoryInfo = {
  total_bytes: number
  available_bytes: number
  used_bytes: number
}

export type Disk = {
  device: string
  mountpoint: string
  fs_type: string
  total_bytes: number
  used_bytes: number
  label?: string
}

export type NetIface = {
  name: string
  hardware_addr?: string
  mtu?: number
  flags?: string
  addrs?: string[]
  state?: string
}

export type InventoryHardware = {
  host: Host
  cpu: CPUSlot[]
  memory: MemoryInfo
  disks: Disk[]
  network: NetIface[]
}

export type InventorySnapshot = {
  agent_id: string
  received_at: string
  source: string
  hardware?: InventoryHardware
  software?: InventorySoftware
  agent_version: string
  schema_ver: number
}

export type InventoryRefreshResponse = {
  agent_id: string
  request_id: string
  delivered: boolean
  stale: boolean
}

// Phase 2.1 — software blocks ----------------------------------------

export type InventoryPackage = {
  name: string
  version: string
  source?: string
  publisher?: string
}

export type InventoryService = {
  name: string
  state: string
  start_type?: string
  source?: string
}

export type InventoryUpdate = {
  name: string
  current_version?: string
  available_version?: string
  severity?: string
  source?: string
}

// v1 snapshot had no software block; v2 carries these. Keep them optional
// so the UI can render either schema.
export type InventorySoftware = {
  packages?: InventoryPackage[]
  services?: InventoryService[]
  updates?: InventoryUpdate[]
}

export async function getInventory(agentId: string): Promise<InventorySnapshot> {
  return get<InventorySnapshot>(`/api/v1/agents/${agentId}/inventory`)
}

export async function getInventoryHistory(agentId: string): Promise<{ items: InventorySnapshot[] }> {
  return get<{ items: InventorySnapshot[] }>(`/api/v1/agents/${agentId}/inventory/history?limit=20`)
}

export async function refreshInventory(agentId: string): Promise<InventoryRefreshResponse> {
  return post<InventoryRefreshResponse>(`/api/v1/agents/${agentId}/inventory/refresh`)
}