import { useState } from 'react'
import { api, type RequestAccount, type StepRow } from '../lib/api'
import { useFetch } from '../lib/useFetch'
import { Badge, Card, CardTitle, Empty, ErrorBox } from '../components/ui/card'

export function RequestsPage() {
  const { data, error, loading, reload } = useFetch(() => api.requests(100), [])
  const [openId, setOpenId] = useState<string | null>(null)
  const [keys, setKeys] = useState<Record<string, string[]>>({})
  const [steps, setSteps] = useState<Record<string, StepRow[]>>({})
  const [accounts, setAccounts] = useState<Record<string, RequestAccount>>({})
  const [showSecrets, setShowSecrets] = useState<Record<string, boolean>>({})
  const toggle = (id: string) => {
    if (openId === id) {
      setOpenId(null)
      return
    }
    setOpenId(id)
    if (!keys[id]) api.requestKeys(id).then((r) => setKeys((k) => ({ ...k, [id]: r.keys }))).catch(() => {})
    if (!steps[id])
      api
        .requestSteps(id)
        .then((r) => setSteps((s) => ({ ...s, [id]: r.steps })))
        .catch(() => {})
  }
  const reveal = (id: string) => {
    if (accounts[id]) {
      setShowSecrets((s) => ({ ...s, [id]: !s[id] }))
      return
    }
    api
      .requestAccount(id)
      .then((a) => {
        setAccounts((m) => ({ ...m, [id]: a }))
        setShowSecrets((s) => ({ ...s, [id]: true }))
      })
      .catch(() => {})
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
              <div className="mt-2 space-y-2 rounded bg-void p-2 font-terminal text-xs">
                <div>
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
                <div>
                  <div className="mb-1 text-[#8a8aa0]">run steps</div>
                  {!steps[r.id] ? (
                    'loading…'
                  ) : steps[r.id].length === 0 ? (
                    <span className="text-[#8a8aa0]">no steps logged</span>
                  ) : (
                    steps[r.id].map((s, i) => (
                      <div key={i} className={s.ok ? '' : 'text-err-red'}>
                        [{s.ok ? 'ok' : 'FAIL'}] {s.step} — {s.detail}
                      </div>
                    ))
                  )}
                </div>
                <div>
                  <button onClick={() => reveal(r.id)} className="text-neon-cyan hover:underline">
                    {showSecrets[r.id] ? 'hide account secrets' : 'reveal account secrets'}
                  </button>
                  {showSecrets[r.id] &&
                    (accounts[r.id] ? (
                      <div className="mt-1">
                        <div>password: {accounts[r.id].password}</div>
                        <div>
                          dob: {accounts[r.id].dob} country: {accounts[r.id].country} otp_used:{' '}
                          {accounts[r.id].otp_used ? 'yes' : 'no'}
                        </div>
                      </div>
                    ) : (
                      <div className="text-[#8a8aa0]">loading…</div>
                    ))}
                </div>
              </div>
            )}
          </div>
        ))
      )}
    </Card>
  )
}
