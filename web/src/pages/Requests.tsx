import { useState } from 'react'
import { api } from '../lib/api'
import { useFetch } from '../lib/useFetch'
import { Badge, Card, CardTitle, Empty, ErrorBox } from '../components/ui/card'

export function RequestsPage() {
  const { data, error, loading, reload } = useFetch(() => api.requests(100), [])
  const [openId, setOpenId] = useState<string | null>(null)
  const [keys, setKeys] = useState<Record<string, string[]>>({})
  const toggle = (id: string) => {
    if (openId === id) {
      setOpenId(null)
      return
    }
    setOpenId(id)
    if (!keys[id]) api.requestKeys(id).then((r) => setKeys((k) => ({ ...k, [id]: r.keys }))).catch(() => {})
  }
  return (
    <Card>
      <CardTitle>key requests</CardTitle>
      {loading ? (
        <div className="text-sm text-[#8a8aa0]">loading…</div>
      ) : error || !data ? (
        <ErrorBox message={error ?? 'no data'} onRetry={reload} />
      ) : data.requests.length === 0 ? (
        <Empty text="no requests yet" />
      ) : (
        data.requests.map((r) => (
          <div key={r.id} className="border-b border-edge py-2 last:border-0">
            <div className="flex flex-wrap items-center gap-2">
              <button onClick={() => toggle(r.id)} className="font-terminal text-sm text-neon-cyan hover:underline">
                {r.email}
              </button>
              <Badge tone="info">{r.provider}</Badge>
              <Badge tone={r.status === 'done' ? 'ok' : r.status === 'failed' ? 'err' : 'warn'}>
                {r.status}
              </Badge>
              <span className="font-terminal text-xs text-[#8a8aa0]">
                qty={r.key_quantity} keys={r.keys} card={r.card_id || '—'}
              </span>
            </div>
            {r.error && <div className="mt-1 font-terminal text-xs text-err-red">{r.error}</div>}
            {openId === r.id && (
              <div className="mt-2 rounded bg-void p-2 font-terminal text-xs">
                {keys[r.id] ? (
                  keys[r.id].length === 0 ? (
                    <span className="text-[#8a8aa0]">no keys stored</span>
                  ) : (
                    keys[r.id].map((k) => <div key={k}>{k}</div>)
                  )
                ) : (
                  'loading…'
                )}
              </div>
            )}
          </div>
        ))
      )}
    </Card>
  )
}
