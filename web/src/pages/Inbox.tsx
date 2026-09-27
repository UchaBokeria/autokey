import { useState } from 'react'
import { api } from '../lib/api'
import { useFetch } from '../lib/useFetch'
import { Button } from '../components/ui/button'
import { Card, CardTitle, Empty, ErrorBox } from '../components/ui/card'
import { Input, Label } from '../components/ui/input'

export function InboxPage() {
  const [filter, setFilter] = useState('')
  const [applied, setApplied] = useState('')
  const [openId, setOpenId] = useState<string | null>(null)
  const [bodies, setBodies] = useState<Record<string, { text: string; html: string }>>({})
  const { data, error, loading, reload } = useFetch(() => api.inbox(100, applied), [applied])
  const open = (id: string) => {
    if (openId === id) {
      setOpenId(null)
      return
    }
    setOpenId(id)
    if (!bodies[id])
      api
        .inboxMessage(id)
        .then((m) => setBodies((b) => ({ ...b, [id]: { text: m.body_text, html: m.body_html } })))
        .catch(() => {})
  }
  return (
    <div className="space-y-4">
      <Card>
        <Label text="filter by recipient" />
        <div className="flex gap-2">
          <Input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="user@domain" />
          <Button variant="outline" onClick={() => setApplied(filter)}>
            apply
          </Button>
          <Button
            variant="ghost"
            onClick={() => {
              setFilter('')
              setApplied('')
            }}
          >
            clear
          </Button>
        </div>
      </Card>
      <Card>
        <CardTitle>inbox</CardTitle>
        {loading ? (
          <div className="text-sm text-[#8a8aa0]">loading…</div>
        ) : error || !data ? (
          <ErrorBox message={error ?? 'no data'} onRetry={reload} />
        ) : data.emails.length === 0 ? (
          <Empty text="no emails" />
        ) : (
          data.emails.map((m) => (
            <div key={m.message_id} className="border-b border-edge py-2 last:border-0">
              <button onClick={() => open(m.message_id)} className="text-left text-sm hover:underline">
                <span className="text-neon-cyan">{m.subject || '(no subject)'}</span>{' '}
                <span className="font-terminal text-xs text-[#8a8aa0]">
                  → {m.recipient} · {m.received_at}
                </span>
              </button>
              <div className="font-terminal text-xs text-[#8a8aa0]">{m.preview}</div>
              {openId === m.message_id && (
                <pre className="mt-2 max-h-64 overflow-auto rounded bg-void p-2 font-terminal text-xs whitespace-pre-wrap">
                  {bodies[m.message_id] ? bodies[m.message_id].text || '(empty body)' : 'loading…'}
                </pre>
              )}
            </div>
          ))
        )}
      </Card>
    </div>
  )
}
