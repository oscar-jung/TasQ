import { useEffect, useRef, useState } from 'react'
import { ChevronDown } from 'lucide-react'

export type TaskStatus = 'planned' | 'in_progress' | 'done'

function statusLabel(status: TaskStatus) {
  if (status === 'in_progress') return 'In Progress'
  if (status === 'done') return 'Done'
  return 'Planned'
}

function statusDotClass(status: TaskStatus) {
  if (status === 'in_progress') return 'bg-amber-400'
  if (status === 'done') return 'bg-emerald-400'
  return 'bg-slate-400'
}

function statusBorderClass(status: TaskStatus) {
  if (status === 'in_progress') return 'border-amber-400/50'
  if (status === 'done') return 'border-emerald-400/50'
  return 'border-slate-400/50'
}

function canSelectStatus(nextStatus: TaskStatus, parentStatus: TaskStatus | null) {
  if (!parentStatus) return true
  if (parentStatus === 'planned' || parentStatus === 'in_progress') {
    return nextStatus === 'planned'
  }
  return true
}

export function StatusDropdown({
  value,
  parentStatus,
  disabled,
  onChange
}: {
  value: TaskStatus
  parentStatus: TaskStatus | null
  disabled: boolean
  onChange: (status: TaskStatus) => void
}) {
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    if (!open) return
    const onMouseDown = (e: MouseEvent) => {
      if (!rootRef.current) return
      if (!rootRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    window.addEventListener('mousedown', onMouseDown)
    window.addEventListener('keydown', onKeyDown)
    return () => {
      window.removeEventListener('mousedown', onMouseDown)
      window.removeEventListener('keydown', onKeyDown)
    }
  }, [open])

  const options: TaskStatus[] = ['planned', 'in_progress', 'done']
  const parentConstrained = parentStatus === 'planned' || parentStatus === 'in_progress'

  return (
    <div className="relative w-full" ref={rootRef}>
      <button
        type="button"
        disabled={disabled}
        onClick={() => setOpen((v) => !v)}
        className={`flex h-9 w-full items-center justify-between rounded-md border bg-background px-2 text-sm ${statusBorderClass(
          value
        )}`}
      >
        <span className="flex items-center gap-2">
          <span className={`h-2.5 w-2.5 rounded-full ${statusDotClass(value)}`} />
          <span>{statusLabel(value)}</span>
        </span>
        <ChevronDown className="h-3.5 w-3.5 text-muted-foreground" />
      </button>

      {open && (
        <div className="absolute left-0 top-10 z-30 w-full rounded-md border border-border bg-card p-1 shadow-2xl">
          {options.map((option) => {
            const blocked = !canSelectStatus(option, parentStatus)
            return (
              <button
                key={option}
                type="button"
                disabled={blocked}
                onClick={() => {
                  setOpen(false)
                  if (option !== value) onChange(option)
                }}
                className={`flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm ${
                  option === value ? 'bg-secondary' : 'hover:bg-muted'
                } ${blocked ? 'cursor-not-allowed opacity-40' : ''}`}
              >
                <span className={`h-2.5 w-2.5 rounded-full ${statusDotClass(option)}`} />
                <span>{statusLabel(option)}</span>
              </button>
            )
          })}
          {parentConstrained && (
            <p className="px-2 pb-1 pt-1 text-[11px] text-muted-foreground">
              Parent is not done, so only Planned is available.
            </p>
          )}
        </div>
      )}
    </div>
  )
}
