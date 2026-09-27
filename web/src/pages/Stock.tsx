import { api } from '../lib/api'
import { useFetch } from '../lib/useFetch'
import { Badge, Card, CardTitle, Empty, ErrorBox } from '../components/ui/card'

export function StockPage() {
  const { data, error, loading, reload } = useFetch(api.stock, [])
  return (
    <Card>
      <CardTitle>onramp stock (live)</CardTitle>
      {loading ? (
        <div className="text-sm text-[#8a8aa0]">loading…</div>
      ) : error || !data ? (
        <ErrorBox message={error ?? 'no data'} onRetry={reload} />
      ) : Object.keys(data.stock).length === 0 ? (
        <Empty text="no products" />
      ) : (
        Object.entries(data.stock).map(([k, p]) => (
          <div key={k} className="flex flex-wrap items-center gap-2 border-b border-edge py-2 last:border-0">
            <Badge tone="info">{p.provider || k}</Badge>
            <span className="text-sm">{p.brand}</span>
            <Badge tone={p.status === 'available' ? 'ok' : 'err'}>{p.status}</Badge>
            {p.amount && (
              <span className="font-terminal text-xs text-[#8a8aa0]">
                ${p.amount.min}–${p.amount.max}
              </span>
            )}
          </div>
        ))
      )}
    </Card>
  )
}
