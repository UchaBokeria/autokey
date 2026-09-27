import type { FocusEvent } from 'react'
import { useState } from 'react'
import { Trash2 } from 'lucide-react'
import { api, type PoolCard } from '../lib/api'
import { useFetch } from '../lib/useFetch'
import { Button } from '../components/ui/button'
import { Badge, Card, CardTitle, Empty, ErrorBox } from '../components/ui/card'
import { Input, Label } from '../components/ui/input'

function CardRow({ card, onChanged }: { card: PoolCard; onChanged: () => void }) {
  const [open, setOpen] = useState(false)
  const [stats, setStats] = useState<null | Awaited<ReturnType<typeof api.cardStats>>>(null)
  const [removing, setRemoving] = useState(false)
  const toggle = () => {
    if (!open) api.cardStats(card.id).then(setStats).catch(() => setStats({ stats: [] }))
    setOpen(!open)
  }
  const remove = async () => {
    if (!confirm(`Remove custom card ${card.id}? Secrets are deleted.`)) return
    setRemoving(true)
    try {
      await api.removeCustom(card.id)
      onChanged()
    } catch (e) {
      alert(e instanceof Error ? e.message : String(e))
    } finally {
      setRemoving(false)
    }
  }
  return (
    <div className="border-b border-edge py-2 last:border-0">
      <div className="flex flex-wrap items-center gap-2">
        <button onClick={toggle} className="font-terminal text-sm text-neon-cyan hover:underline">
          {card.id}
        </button>
        <Badge tone="info">{card.provider}</Badge>
        <Badge tone={card.status === 'active' ? 'ok' : 'warn'}>{card.status}</Badge>
        {card.claimed && <Badge tone="warn">claimed</Badge>}
        <span className="font-terminal text-xs text-[#8a8aa0]">
          {card.name} ···{card.last4 || '····'} ok={card.ok} fail={card.fail}
        </span>
        {card.provider === 'custom' && (
          <Button variant="danger" onClick={remove} disabled={removing} className="ml-auto px-2 py-1 text-xs">
            <Trash2 size={12} /> remove
          </Button>
        )}
      </div>
      {card.last_error && <div className="mt-1 font-terminal text-xs text-err-red">{card.last_error}</div>}
      {open && (
        <div className="mt-2 rounded bg-void p-2 font-terminal text-xs">
          {!stats ? (
            'loading…'
          ) : stats.stats.length === 0 ? (
            <span className="text-[#8a8aa0]">no per-service stats</span>
          ) : (
            stats.stats.map((s) => (
              <div key={s.service}>
                {s.service}: ok={s.ok} fail={s.fail} {s.last_error && `err=${s.last_error}`}
              </div>
            ))
          )}
        </div>
      )}
    </div>
  )
}

export function CardsPage() {
  const [provider, setProvider] = useState('')
  const { data, error, loading, reload } = useFetch(() => api.cards(provider), [provider])
  return (
    <div className="space-y-4">
      <Card>
        <Label text="provider filter" />
        <div className="flex gap-2">
          {['', 'kripi', 'onramp', 'custom'].map((p) => (
            <Button key={p} variant={provider === p ? 'default' : 'outline'} onClick={() => setProvider(p)}>
              {p === '' ? 'all' : p}
            </Button>
          ))}
        </div>
      </Card>
      <Card>
        <CardTitle>pool cards</CardTitle>
        {loading ? (
          <div className="text-sm text-[#8a8aa0]">loading…</div>
        ) : error || !data ? (
          <ErrorBox message={error ?? 'no data'} onRetry={reload} />
        ) : data.cards.length === 0 ? (
          <Empty text="pool is empty" />
        ) : (
          data.cards.map((c) => <CardRow key={c.id} card={c} onChanged={reload} />)
        )}
      </Card>
      <Card>
        <CardTitle>add via CLI (interactive prompts)</CardTitle>
        <Input readOnly value="autokey cards add --label mine" onFocus={(e: FocusEvent<HTMLInputElement>) => e.target.select()} />
      </Card>
    </div>
  )
}
