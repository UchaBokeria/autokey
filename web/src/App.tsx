import { useState } from 'react'
import { Inbox, KeyRound, LayoutDashboard, MailOpen, ScrollText, Tags, Wallet } from 'lucide-react'
import { setToken } from './lib/api'
import { CardsPage } from './pages/Cards'
import { InboxPage } from './pages/Inbox'
import { LogsPage } from './pages/Logs'
import { OTPPage } from './pages/OTP'
import { OverviewPage } from './pages/Overview'
import { RequestsPage } from './pages/Requests'
import { StockPage } from './pages/Stock'
import { Button } from './components/ui/button'
import { Input } from './components/ui/input'
import { cn } from './lib/cn'

const tabs = [
  { id: 'overview', label: 'Overview', icon: LayoutDashboard },
  { id: 'cards', label: 'Cards', icon: Wallet },
  { id: 'requests', label: 'Keys', icon: KeyRound },
  { id: 'inbox', label: 'Inbox', icon: Inbox },
  { id: 'otp', label: 'OTP', icon: MailOpen },
  { id: 'stock', label: 'Stock', icon: Tags },
  { id: 'logs', label: 'Logs', icon: ScrollText },
] as const

type TabId = (typeof tabs)[number]['id']

export default function App() {
  const [tab, setTab] = useState<TabId>('overview')
  const [token, setTokenState] = useState(() => localStorage.getItem('autokey-token') ?? '')
  const saveToken = () => {
    setToken(token)
    window.location.reload()
  }
  return (
    <div className="mx-auto min-h-screen max-w-5xl px-4 py-6">
      <header className="mb-6 flex flex-wrap items-center gap-3">
        <h1 className="text-xl font-bold">
          <span className="text-neon-pink">AUTOKEY</span>{' '}
          <span className="text-sm font-normal text-neon-cyan">wildcard inbox // card pool // key pipeline</span>
        </h1>
        <div className="ml-auto flex gap-2">
          <Input
            type="password"
            value={token}
            onChange={(e) => setTokenState(e.target.value)}
            placeholder="bearer token"
            className="w-44"
          />
          <Button variant="outline" onClick={saveToken}>
            save
          </Button>
        </div>
      </header>
      <nav className="mb-6 flex flex-wrap gap-2">
        {tabs.map((t) => {
          const Icon = t.icon
          return (
            <Button
              key={t.id}
              variant={tab === t.id ? 'default' : 'outline'}
              onClick={() => setTab(t.id)}
            >
              <Icon size={14} />
              {t.label}
            </Button>
          )
        })}
      </nav>
      <main className={cn(tab === 'otp' && 'mx-auto max-w-xl')}>
        {tab === 'overview' && <OverviewPage />}
        {tab === 'cards' && <CardsPage />}
        {tab === 'requests' && <RequestsPage />}
        {tab === 'inbox' && <InboxPage />}
        {tab === 'otp' && <OTPPage />}
        {tab === 'stock' && <StockPage />}
        {tab === 'logs' && <LogsPage />}
      </main>
    </div>
  )
}
