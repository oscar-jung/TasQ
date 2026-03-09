import { useRef, useState } from 'react'
import { Button } from '@/components/ui/button'

export function MarkdownEditor({
  label,
  value,
  onChange,
  onSave,
  isSaving,
  minHeightClassName = 'min-h-[140px]'
}: {
  label: string
  value: string
  onChange: (next: string) => void
  onSave: () => void
  isSaving: boolean
  minHeightClassName?: string
}) {
  const [mode, setMode] = useState<'write' | 'preview'>('write')
  const textareaRef = useRef<HTMLTextAreaElement | null>(null)

  function injectSnippet(prefix: string, suffix = '') {
    const textarea = textareaRef.current
    if (!textarea) return

    const start = textarea.selectionStart
    const end = textarea.selectionEnd
    const selected = value.slice(start, end)
    const next = value.slice(0, start) + prefix + selected + suffix + value.slice(end)
    onChange(next)

    requestAnimationFrame(() => {
      textarea.focus()
      const caret = start + prefix.length + selected.length + suffix.length
      textarea.setSelectionRange(caret, caret)
    })
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col rounded-md border bg-muted/20">
      <div className="flex items-center justify-between border-b border-border/70 px-3 py-2">
        <p className="text-xs uppercase tracking-wide text-muted-foreground">{label}</p>
        <div className="flex items-center gap-2">
          <div className="flex rounded-md border border-border/70 bg-background/40 p-0.5">
            <button
              type="button"
              onClick={() => setMode('write')}
              className={`rounded px-2 py-1 text-xs ${mode === 'write' ? 'bg-secondary text-foreground' : 'text-muted-foreground'}`}
            >
              Write
            </button>
            <button
              type="button"
              onClick={() => setMode('preview')}
              className={`rounded px-2 py-1 text-xs ${mode === 'preview' ? 'bg-secondary text-foreground' : 'text-muted-foreground'}`}
            >
              Preview
            </button>
          </div>
          <Button size="sm" variant="outline" disabled={isSaving} onClick={onSave}>
            {isSaving ? 'Saving...' : 'Save'}
          </Button>
        </div>
      </div>

      {mode === 'write' && (
        <div className="flex min-h-0 flex-1 flex-col">
          <div className="flex items-center gap-1 border-b border-border/70 px-2 py-1">
            <EditorToolButton label="H2" onClick={() => injectSnippet('## ')} />
            <EditorToolButton label="B" onClick={() => injectSnippet('**', '**')} />
            <EditorToolButton label="I" onClick={() => injectSnippet('*', '*')} />
            <EditorToolButton label="Code" onClick={() => injectSnippet('`', '`')} />
            <EditorToolButton label="List" onClick={() => injectSnippet('- ')} />
            <EditorToolButton label="Link" onClick={() => injectSnippet('[text](', ')')} />
          </div>

          <textarea
            ref={textareaRef}
            value={value}
            onChange={(e) => onChange(e.target.value)}
            placeholder="Write markdown..."
            className={`h-full flex-1 resize-none bg-transparent p-3 text-sm outline-none ${minHeightClassName}`}
          />
        </div>
      )}

      {mode === 'preview' && (
        <div
          className={`markdown-preview flex-1 overflow-auto p-3 text-sm ${minHeightClassName}`}
          dangerouslySetInnerHTML={{ __html: toMarkdownHTML(value) }}
        />
      )}
    </div>
  )
}

function EditorToolButton({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="rounded border border-border/70 bg-background/30 px-2 py-1 text-[11px] text-muted-foreground hover:text-foreground"
    >
      {label}
    </button>
  )
}

function toMarkdownHTML(markdown: string) {
  if (!markdown.trim()) {
    return '<p class="muted">No content yet.</p>'
  }

  let html = escapeHTML(markdown)

  html = html.replace(/```([\s\S]*?)```/g, (_m, code) => `<pre><code>${code.trim()}</code></pre>`)
  html = html.replace(/^### (.+)$/gm, '<h3>$1</h3>')
  html = html.replace(/^## (.+)$/gm, '<h2>$1</h2>')
  html = html.replace(/^# (.+)$/gm, '<h1>$1</h1>')
  html = html.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>')
  html = html.replace(/\*(.+?)\*/g, '<em>$1</em>')
  html = html.replace(/`([^`]+)`/g, '<code>$1</code>')
  html = html.replace(/\[([^\]]+)\]\(([^\)]+)\)/g, '<a href="$2" target="_blank" rel="noreferrer">$1</a>')

  html = html
    .split('\n')
    .map((line) => {
      if (line.startsWith('- ')) {
        return `<li>${line.slice(2)}</li>`
      }
      return line
    })
    .join('\n')

  html = html.replace(/(<li>.*?<\/li>\n?)+/gs, (list) => `<ul>${list}</ul>`)

  const blocks = html
    .split(/\n{2,}/)
    .map((block) => block.trim())
    .filter(Boolean)
    .map((block) => {
      if (block.startsWith('<h1') || block.startsWith('<h2') || block.startsWith('<h3') || block.startsWith('<pre') || block.startsWith('<ul')) {
        return block
      }
      return `<p>${block.replace(/\n/g, '<br />')}</p>`
    })

  return blocks.join('')
}

function escapeHTML(value: string) {
  return value
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#039;')
}
