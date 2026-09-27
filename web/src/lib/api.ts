export interface Overview {
  version: string
  cards_total: number
  cards_active: number
  cards_claimed: number
  requests_total: number
  requests_done: number
  keys_total: number
  inbox_total: number
  by_provider: Array<{ provider: string; count: number }>
}

export interface PoolCard {
  id: string
  last4: string
  bin: string
  name: string
  provider: string
  status: string
  claimed: boolean
  created_at: string
  ok: number
  fail: number
  last_error: string
}

export interface CardStat {
  service: string
  ok: number
  fail: number
  last_error: string
  updated_at: string
}

export interface KeyRequest {
  id: string
  email: string
  service: string
  key_quantity: number
  provider: string
  status: string
  card_id: string
  error: string
  created_at: string
  updated_at: string
  keys: number
}

export interface InboxEmail {
  message_id: string
  recipient: string
  sender: string
  subject: string
  preview: string
  received_at: string
}

export interface InboxMessage extends InboxEmail {
  body_text: string
  body_html: string
}

export interface OTP {
  email: string
  code: string
  subject: string
  from: string
  received_at: string
}

export interface StockProduct {
  brand: string
  provider: string
  status: string
  currency?: string
  amount?: { min: number; max: number }
}

const token = () => localStorage.getItem('autokey-token') ?? ''

async function get<T>(path: string): Promise<T> {
  const res = await fetch(path, { headers: { Authorization: `Bearer ${token()}` } })
  if (res.status === 401) throw new Error('unauthorized — set bearer token')
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { message?: string }
    throw new Error(body.message ?? `request failed (${res.status})`)
  }
  return res.json() as Promise<T>
}

async function del(path: string): Promise<void> {
  const res = await fetch(path, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${token()}` },
  })
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { message?: string }
    throw new Error(body.message ?? `request failed (${res.status})`)
  }
}

export const api = {
  overview: () => get<Overview>('/api/overview'),
  cards: (provider = '') =>
    get<{ cards: PoolCard[] }>(`/api/cards${provider ? `?provider=${provider}` : ''}`),
  cardStats: (id: string) => get<{ stats: CardStat[] }>(`/api/cards/${id}/stats`),
  requests: (limit = 100) => get<{ requests: KeyRequest[] }>(`/api/requests?limit=${limit}`),
  requestKeys: (id: string) => get<{ keys: string[] }>(`/api/requests/${id}/keys`),
  requestEmail: (id: string) => get<{ email: string }>(`/api/requests/${id}/email`),
  inbox: (limit = 100, recipient = '') =>
    get<{ emails: InboxEmail[] }>(
      `/api/inbox?limit=${limit}${recipient ? `&recipient=${recipient}` : ''}`,
    ),
  inboxMessage: (id: string) =>
    get<InboxMessage>(`/api/inbox/${encodeURIComponent(id)}`),
  otp: (email: string) => get<OTP>(`/api/otp?email=${encodeURIComponent(email)}`),
  stock: () => get<{ stock: Record<string, StockProduct> }>('/api/onramp/stock'),
  removeCustom: (id: string) => del(`/api/cards-custom/${id}`),
  logs: (lines = 100) => get<{ lines: string[] }>(`/api/logs?lines=${lines}`),
};

export function setToken(t: string) {
  localStorage.setItem('autokey-token', t)
}
