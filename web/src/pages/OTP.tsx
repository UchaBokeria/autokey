import { useState } from 'react'
import { api } from '../lib/api'
import { Button } from '../components/ui/button'
import { Card, CardTitle } from '../components/ui/card'
import { Input, Label } from '../components/ui/input'

export function OTPPage() {
  const [email, setEmail] = useState('')
  const [code, setCode] = useState<string | null>(null)
  const [meta, setMeta] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const lookup = async () => {
    setBusy(true)
    setError(null)
    setCode(null)
    try {
      const r = await api.otp(email)
      setCode(r.code)
      setMeta(`from=${r.from} subj=${r.subject} at=${r.received_at}`)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Card>
      <CardTitle>one-time code lookup</CardTitle>
      <Label text="email user" />
      <div className="flex gap-2">
        <Input value={email} onChange={(e) => setEmail(e.target.value)} placeholder="user@domain" />
        <Button onClick={lookup} disabled={busy || !email}>
          {busy ? '…' : 'read code'}
        </Button>
      </div>
      {code && (
        <div className="mt-4 rounded border border-neon-lime/40 p-3 text-center">
          <div className="font-terminal text-4xl text-neon-lime">{code}</div>
          <div className="mt-1 font-terminal text-xs text-[#8a8aa0]">{meta}</div>
        </div>
      )}
      {error && <div className="mt-3 text-sm text-err-red">{error}</div>}
      <div className="mt-3 font-terminal text-xs text-[#8a8aa0]">
        CLI: autokey inbox otp --email user@domain [--latest]
      </div>
    </Card>
  )
}
