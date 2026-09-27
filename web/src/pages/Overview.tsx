import { api } from '../lib/api'
import { useFetch } from '../lib/useFetch'
import { Badge, Card, CardTitle, Empty, ErrorBox } from '../components/ui/card'

export function OverviewPage() {
  const { data, error, loading, reload } = useFetch(api.overview)
  if (loading) return <div className="text-sm text-[#8a8aa0]">loading…</div>
  if (error || !data) return <ErrorBox message={error ?? 'no data'} onRetry={reload} />
  const stats: Array<[string, number | string]> = [
    ['pool cards', data.cards_total],
    ['active / unclaimed', `${data.cards_active} / ${data.cards_total - data.cards_claimed}`],
    ['requests', `${data.requests_done}/${data.requests_total} done`],
    ['keys issued', data.keys_total],
    ['inbox emails', data.inbox_total],
    ['version', data.version || 'dev'],
  ]
  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-3 md:grid-cols-3">
        {stats.map(([k, v]) => (
          <Card key={k}>
            <div className="text-xs uppercase tracking-wider text-[#8a8aa0]">{k}</div>
            <div className="font-terminal text-2xl text-neon-lime">{v}</div>
          </Card>
        ))}
      </div>
      <Card>
        <CardTitle>pool by provider</CardTitle>
        {data.by_provider.length === 0 ? (
          <Empty text="pool is empty" />
        ) : (
          <div className="flex flex-wrap gap-2">
            {data.by_provider.map((p) => (
              <span key={p.provider} className="flex items-center gap-2">
                <Badge tone="info">{p.provider}</Badge>
                <span className="font-terminal">{p.count}</span>
              </span>
            ))}
          </div>
        )}
      </Card>
    </div>
  )
}
