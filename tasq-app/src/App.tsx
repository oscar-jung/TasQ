import { FormEvent, MouseEvent as ReactMouseEvent, useEffect, useMemo, useRef, useState } from 'react'
import { Check, Pencil, Plus, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'

type Project = {
  id: number
  name: string
  description: string
}

type TaskNode = {
  id: number
  project_id: number
  parent_task_id: number | null
  title: string
  spec_md: string
  result_md: string
  status: 'planned' | 'in_progress' | 'done'
  display_order: number
  children: TaskNode[]
}

const apiBase = import.meta.env.VITE_API_BASE ?? 'http://localhost:8080'
const NODE_WIDTH = 180
const NODE_GAP = 56
const CONNECTOR_TOP = 28
const CONNECTOR_BOTTOM = 34

export function App() {
  const [projects, setProjects] = useState<Project[]>([])
  const [selectedProjectID, setSelectedProjectID] = useState<number | null>(null)
  const [newProjectName, setNewProjectName] = useState('')

  const [editingProjectID, setEditingProjectID] = useState<number | null>(null)
  const [editingProjectName, setEditingProjectName] = useState('')

  const [tree, setTree] = useState<TaskNode[]>([])
  const [newRootTaskTitle, setNewRootTaskTitle] = useState('')
  const treeViewportRef = useRef<HTMLDivElement | null>(null)
  const isPanningRef = useRef(false)
  const panOriginRef = useRef({ x: 0, y: 0, left: 0, top: 0 })
  const [isPanningUI, setIsPanningUI] = useState(false)

  const selectedProject = useMemo(
    () => projects.find((project) => project.id === selectedProjectID) ?? null,
    [projects, selectedProjectID]
  )
  const subtreeUnits = useMemo(() => {
    const map = new Map<number, number>()
    const walk = (node: TaskNode): number => {
      if (node.children.length === 0) {
        map.set(node.id, 1)
        return 1
      }
      const units = node.children.reduce((acc, child) => acc + walk(child), 0)
      map.set(node.id, units)
      return units
    }
    tree.forEach((root) => {
      walk(root)
    })
    return map
  }, [tree])

  useEffect(() => {
    void fetchProjects()
  }, [])

  useEffect(() => {
    if (selectedProjectID) {
      void fetchTaskTree(selectedProjectID)
    } else {
      setTree([])
    }
  }, [selectedProjectID])

  useEffect(() => {
    function handleMouseMove(e: MouseEvent) {
      if (!isPanningRef.current || !treeViewportRef.current) return
      const dx = e.clientX - panOriginRef.current.x
      const dy = e.clientY - panOriginRef.current.y
      treeViewportRef.current.scrollLeft = panOriginRef.current.left - dx
      treeViewportRef.current.scrollTop = panOriginRef.current.top - dy
    }

    function handleMouseUp() {
      if (!isPanningRef.current) return
      isPanningRef.current = false
      setIsPanningUI(false)
      document.body.style.userSelect = ''
    }

    window.addEventListener('mousemove', handleMouseMove)
    window.addEventListener('mouseup', handleMouseUp)
    return () => {
      window.removeEventListener('mousemove', handleMouseMove)
      window.removeEventListener('mouseup', handleMouseUp)
    }
  }, [])

  async function fetchProjects() {
    const res = await fetch(`${apiBase}/projects`)
    const data = (await res.json()) as Project[]
    setProjects(data)
    if (!selectedProjectID && data.length > 0) {
      setSelectedProjectID(data[0].id)
    }
  }

  async function fetchTaskTree(projectID: number) {
    const res = await fetch(`${apiBase}/projects/${projectID}/tasks/tree`)
    const data = (await res.json()) as TaskNode[]
    setTree(data)
  }

  async function createProject(e: FormEvent) {
    e.preventDefault()
    if (!newProjectName.trim()) return
    await fetch(`${apiBase}/projects`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: newProjectName.trim(), description: '' })
    })
    setNewProjectName('')
    await fetchProjects()
  }

  function startProjectEdit(project: Project) {
    setEditingProjectID(project.id)
    setEditingProjectName(project.name)
  }

  function cancelProjectEdit() {
    setEditingProjectID(null)
    setEditingProjectName('')
  }

  async function saveProjectEdit(projectID: number) {
    const trimmed = editingProjectName.trim()
    if (!trimmed) return
    await fetch(`${apiBase}/projects/${projectID}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: trimmed })
    })
    cancelProjectEdit()
    await fetchProjects()
  }

  async function createRootTask(e: FormEvent) {
    e.preventDefault()
    if (!selectedProjectID || !newRootTaskTitle.trim()) return
    await fetch(`${apiBase}/projects/${selectedProjectID}/tasks`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ title: newRootTaskTitle.trim(), spec_md: '' })
    })
    setNewRootTaskTitle('')
    await fetchTaskTree(selectedProjectID)
  }

  function startPan(e: ReactMouseEvent<HTMLDivElement>) {
    if (e.button !== 0 || !treeViewportRef.current) return
    const target = e.target as HTMLElement
    if (target.closest('button, input, textarea, a')) return
    isPanningRef.current = true
    setIsPanningUI(true)
    panOriginRef.current = {
      x: e.clientX,
      y: e.clientY,
      left: treeViewportRef.current.scrollLeft,
      top: treeViewportRef.current.scrollTop
    }
    document.body.style.userSelect = 'none'
  }

  return (
    <main className="mx-auto grid h-screen min-w-[1460px] max-w-[1600px] grid-cols-[300px_minmax(680px,1fr)_420px] gap-3 p-3">
      <Card className="h-full">
        <CardHeader>
          <CardTitle className="text-lg font-semibold tracking-tight">Projects</CardTitle>
        </CardHeader>
        <CardContent className="flex h-[calc(100%-70px)] flex-col gap-3">
          <form className="flex gap-2" onSubmit={createProject}>
            <Input
              value={newProjectName}
              onChange={(e) => setNewProjectName(e.target.value)}
              placeholder="Add project"
            />
            <Button type="submit" size="sm" variant="outline">
              <Plus className="h-4 w-4" />
            </Button>
          </form>

          <div className="flex-1 overflow-y-auto rounded-md border bg-muted/30 p-2">
            <div className="space-y-1">
              {projects.map((project) => {
                const isSelected = selectedProjectID === project.id
                const isEditing = editingProjectID === project.id
                return (
                  <div
                    key={project.id}
                    className={`group flex cursor-pointer items-center gap-2 rounded-md border px-2 py-1.5 ${
                      isSelected ? 'border-border bg-secondary' : 'border-transparent hover:border-border hover:bg-muted'
                    }`}
                    onClick={() => {
                      if (!isEditing) setSelectedProjectID(project.id)
                    }}
                  >
                    {!isEditing && (
                      <p className="min-w-0 flex-1 truncate text-left text-sm">{project.name}</p>
                    )}

                    {isEditing && (
                      <>
                        <Input
                          autoFocus
                          className="h-8 flex-1"
                          value={editingProjectName}
                          onChange={(e) => setEditingProjectName(e.target.value)}
                        />
                        <Button size="sm" variant="ghost" onClick={() => void saveProjectEdit(project.id)}>
                          <Check className="h-4 w-4" />
                        </Button>
                        <Button size="sm" variant="ghost" onClick={cancelProjectEdit}>
                          <X className="h-4 w-4" />
                        </Button>
                      </>
                    )}

                    {!isEditing && (
                      <Button
                        size="sm"
                        variant="ghost"
                        className="h-7 w-7 p-0 opacity-0 transition-opacity group-hover:opacity-100"
                        onClick={(e) => {
                          e.stopPropagation()
                          startProjectEdit(project)
                        }}
                      >
                        <Pencil className="h-3.5 w-3.5" />
                      </Button>
                    )}
                  </div>
                )
              })}
            </div>
          </div>
        </CardContent>
      </Card>

      <Card className="h-full overflow-hidden">
        <CardHeader className="border-b border-border/80">
          <CardTitle className="font-semibold tracking-tight">Task Tree</CardTitle>
          <p className="mt-1 text-xs text-muted-foreground">
            {selectedProject ? `Project: ${selectedProject.name}` : 'Select a project'}
          </p>
        </CardHeader>
        <CardContent className="flex h-[calc(100%-86px)] flex-col gap-4 pt-4">
          <form onSubmit={createRootTask} className="flex items-center gap-2 overflow-x-auto rounded-md border bg-muted/20 p-2">
            <Input
              value={newRootTaskTitle}
              onChange={(e) => setNewRootTaskTitle(e.target.value)}
              placeholder="Add root task"
              disabled={!selectedProjectID}
              className="w-56 shrink-0"
            />
            <Button type="submit" variant="outline" size="sm" disabled={!selectedProjectID}>
              <Plus className="mr-1 h-4 w-4" />
              Add Root
            </Button>
          </form>

          <div
            ref={treeViewportRef}
            className={`tree-scroll tree-viewport flex-1 overflow-scroll rounded-md border bg-muted/20 p-4 ${
              isPanningUI ? 'cursor-grabbing' : 'cursor-grab'
            }`}
            onMouseDown={startPan}
          >
            {tree.length === 0 && (
              <p className="text-sm text-muted-foreground">No root task yet. Add one from the row above.</p>
            )}

            {tree.length > 0 && (
              <div className="tree-canvas flex min-w-max items-start gap-16 pb-10">
                {tree.map((root) => (
                  <TreeNodeView key={root.id} node={root} subtreeUnits={subtreeUnits} />
                ))}
              </div>
            )}
          </div>
        </CardContent>
      </Card>

      <Card className="h-full">
        <CardHeader>
          <CardTitle className="font-semibold tracking-tight">Task Detail</CardTitle>
        </CardHeader>
        <CardContent className="h-[calc(100%-70px)]">
          <div className="h-full rounded-md border border-dashed bg-muted/30 p-4 text-sm text-muted-foreground">
            Task detail scaffold
          </div>
        </CardContent>
      </Card>
    </main>
  )
}

function nodeSpanWidth(units: number) {
  return units * (NODE_WIDTH + NODE_GAP) - NODE_GAP
}

function TreeNodeView({ node, subtreeUnits }: { node: TaskNode; subtreeUnits: Map<number, number> }) {
  const currentUnits = subtreeUnits.get(node.id) ?? 1
  const currentWidth = nodeSpanWidth(currentUnits)
  const childCount = node.children.length
  const hasManyChildren = childCount > 1
  const centerX = currentWidth / 2
  let cursorX = 0
  const childLayouts = node.children.map((child, index) => {
    const childUnits = subtreeUnits.get(child.id) ?? 1
    const width = nodeSpanWidth(childUnits)
    const center = hasManyChildren ? cursorX + width / 2 : centerX
    cursorX += width + (index < node.children.length - 1 ? NODE_GAP : 0)
    return { child, width, center }
  })

  return (
    <div className="flex flex-col items-center" style={{ width: `${currentWidth}px`, minWidth: `${currentWidth}px` }}>
      <div className="w-[180px] rounded-lg border border-white/25 bg-card px-4 py-2 text-center text-sm shadow-[0_0_0_1px_rgba(255,255,255,0.12)]">
        <p className="truncate font-semibold text-foreground">{node.title}</p>
        <p className="mt-1 text-[11px] uppercase tracking-wide text-foreground/75">{node.status}</p>
      </div>

      {childCount > 0 && (
        <div className="relative mt-2 w-full" style={{ paddingTop: `${CONNECTOR_TOP + CONNECTOR_BOTTOM}px` }}>
          <svg
            className="pointer-events-none absolute left-0 top-0"
            width={currentWidth}
            height={CONNECTOR_TOP + CONNECTOR_BOTTOM}
            viewBox={`0 0 ${currentWidth} ${CONNECTOR_TOP + CONNECTOR_BOTTOM}`}
            fill="none"
          >
            <path
              d={`M ${centerX} 0 V ${CONNECTOR_TOP}`}
              stroke="rgba(255,255,255,0.28)"
              strokeWidth="1"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
            {hasManyChildren && (
              <path
                d={`M ${childLayouts[0].center} ${CONNECTOR_TOP} H ${childLayouts[childLayouts.length - 1].center}`}
                stroke="rgba(255,255,255,0.28)"
                strokeWidth="1"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
            )}
            {childLayouts.map((layout) => {
              return (
                <path
                  key={`line-${layout.child.id}`}
                  d={`M ${layout.center} ${CONNECTOR_TOP} V ${CONNECTOR_TOP + CONNECTOR_BOTTOM}`}
                  stroke="rgba(255,255,255,0.28)"
                  strokeWidth="1"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                />
              )
            })}
          </svg>

          <div className={`flex items-start ${hasManyChildren ? 'justify-between' : 'justify-center'}`}>
            {childLayouts.map((layout) => {
              return (
                <div
                  key={layout.child.id}
                  className="flex flex-col items-center"
                  style={{ width: `${layout.width}px`, minWidth: `${layout.width}px` }}
                >
                  <TreeNodeView node={layout.child} subtreeUnits={subtreeUnits} />
                </div>
              )
            })}
          </div>
        </div>
      )}
    </div>
  )
}
