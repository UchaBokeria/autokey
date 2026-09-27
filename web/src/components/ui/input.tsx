import type { InputHTMLAttributes } from 'react'
import { cn } from '../../lib/cn'

export function Input({ className, ...rest }: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      className={cn(
        'w-full rounded-md border border-edge bg-void px-3 py-1.5 text-sm outline-none',
        'focus:border-neon-cyan/70 placeholder:text-[#55556a]',
        className,
      )}
      {...rest}
    />
  )
}

export function Label({ text }: { text: string }) {
  return <div className="mb-1 text-xs uppercase tracking-wider text-[#8a8aa0]">{text}</div>
}
