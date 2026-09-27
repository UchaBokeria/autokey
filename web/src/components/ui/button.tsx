import type { ButtonHTMLAttributes, ReactNode } from 'react'
import { cn } from '../../lib/cn'

type Variant = 'default' | 'outline' | 'ghost' | 'danger'

const styles: Record<Variant, string> = {
  default: 'bg-neon-pink/90 text-black hover:bg-neon-pink',
  outline: 'border border-edge bg-panel hover:border-neon-cyan/60',
  ghost: 'hover:bg-panel',
  danger: 'border border-err-red/60 text-err-red hover:bg-err-red/10',
}

export function Button({
  variant = 'default',
  className,
  children,
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: Variant; children: ReactNode }) {
  return (
    <button
      className={cn(
        'inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-sm font-medium transition-colors disabled:opacity-50',
        styles[variant],
        className,
      )}
      {...rest}
    >
      {children}
    </button>
  )
}
