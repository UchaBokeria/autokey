import type { ReactNode } from 'react'
import { cn } from '../../lib/cn'

export function Card({ className, children }: { className?: string; children: ReactNode }) {
  return (
    <div className={cn('rounded-lg border border-edge bg-panel p-4', className)}>{children}</div>
  )
}

export function CardTitle({ children }: { children: ReactNode }) {
  return <div className="mb-2 text-sm font-semibold uppercase tracking-wider text-neon-cyan">{children}</div>
}

export function Badge({ tone, children }: { tone: 'ok' | 'warn' | 'err' | 'info'; children: ReactNode }) {
  const color =
    tone === 'ok'
      ? 'border-neon-lime/40 text-neon-lime'
      : tone === 'warn'
        ? 'border-warn-amber/40 text-warn-amber'
        : tone === 'err'
          ? 'border-err-red/40 text-err-red'
          : 'border-neon-cyan/40 text-neon-cyan'
  return (
    <span className={cn('inline-block rounded border px-1.5 py-0.5 font-terminal text-xs', color)}>
      {children}
    </span>
  )
}

export function Empty({ text }: { text: string }) {
  return <div className="py-6 text-center text-sm text-[#8a8aa0]">{text}</div>
}

export function ErrorBox({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <div className="rounded-lg border border-err-red/40 bg-err-red/5 p-4 text-sm text-err-red">
      {message}
      {onRetry && (
        <button onClick={onRetry} className="ml-3 underline">
          retry
        </button>
      )}
    </div>
  )
}
