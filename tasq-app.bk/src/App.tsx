import { FormEvent, useEffect, useMemo, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger
} from '@/components/ui/context-menu'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { PencilLine, Plus, Trash2 } from 'lucide-react'

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

type DeleteStrategy = 'delete_subtree' | 'promote_children' | 'delete_if_leaf'

const apiBase = import.meta.env.VITE_API_BASE ?? 'http://localhost:8080'

export function App() {
  const [projects, setProjects] = useState<Project[]>([])
  const [projectID, setProjectID] = useState<number | null>(null)
  const [tree, setTree] = useState<TaskNode[]>([])
  const [taskMap, setTaskMap] = useState<Map<number, TaskNode>>(new Map())
  const [selectedTaskID, setSelectedTaskID] = useState<number | null>(null)

  const [projectName, setProjectName] = useState('')
  const [projectDescription, setProjectDescription] = useState('')

  const [createDialogOpen, setCreateDialogOpen] = useState(false)
  const [newTaskTitle, setNewTaskTitle] = useState('')
  const [newTaskSpec, setNewTaskSpec] = useState('')
  const [createParentID, setCreateParentID] = useState<number | null>(null)

  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [deleteTargetID, setDeleteTargetID] = useState<number | null>(null)
  const [deleteStrategy, setDeleteStrategy] = useState<DeleteStrategy>('delete_subtree')

  const [draftTitle, setDraftTitle] = useState('')
  const [draftSpec, setDraftSpec] = useState('')
  const [draftResult, setDraftResult] = useState('')
  const [message, setMessage] = useState('')

  const selectedProject = useMemo(() => projects.find((p) => p.id === projectID) ?? null, [projects, projectID])
  const selectedTask = selectedTaskID ? taskMap.get(selectedTaskID) ?? null : null

  useEffect(() => {
    void refreshProjects()
  }, [])

  useEffect(() => {
    if (!projectID && projects.length > 0) {
      setProjectID(projects[0].id)
    }
  }, [projects, projectID])

  useEffect(() => {
    if (projectID) {
      void refreshTree(projectID)
    }
  }, [projectID])

  useEffect(() => {
    if (selectedTask) {
      setDraftTitle(selectedTask.title)
      setDraftSpec(selectedTask.spec_md)
      setDraftResult(selectedTask.result_md)
    }
  }, [selectedTaskID, selectedTask])

  async function refreshProjects() {
    const res = await fetch(`${apiBase}/projects`)
    const data = (await res.json()) as Project[]
    setProjects(data)
  }

  async function refreshTree(pid: number) {
    const res = await fetch(`${apiBase}/projects/${pid}/tasks/tree`)
    const data = (await res.json()) as TaskNode[]
    setTree(data)
    const flattened = new Map<number, TaskNode>()
    const walk = (nodes: TaskNode[]) => {
      nodes.forEach((node) => {
        flattened.set(node.id, node)
        walk(node.children)
      })
    }
    walk(data)
    setTaskMap(flattened)

    if (selectedTaskID && !flattened.has(selectedTaskID)) {
      setSelectedTaskID(null)
    }
  }

  async function createProject(e: FormEvent) {
    e.preventDefault()
    await fetch(`${apiBase}/projects`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: projectName, description: projectDescription })
    })
    setProjectName('')
    setProjectDescription('')
    await refreshProjects()
  }

  async function createTask(e: FormEvent) {
    e.preventDefault()
    if (!projectID) return
    await fetch(`${apiBase}/projects/${projectID}/tasks`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ title: newTaskTitle, spec_md: newTaskSpec, parent_task_id: createParentID })
    })
    setNewTaskTitle('')
    setNewTaskSpec('')
    setCreateParentID(null)
    setCreateDialogOpen(false)
    await refreshTree(projectID)
  }

  async function saveTaskContent() {
    if (!selectedTaskID || !projectID) return
    await fetch(`${apiBase}/tasks/${selectedTaskID}/content`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ title: draftTitle, spec_md: draftSpec, result_md: draftResult })
    })
    setMessage('Task content saved.')
    await refreshTree(projectID)
  }

  async function updateStatus(status: TaskNode['status']) {
    if (!selectedTaskID || !projectID) return
    const res = await fetch(`${apiBase}/tasks/${selectedTaskID}/status`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ status })
    })
    if (!res.ok) {
      setMessage(await res.text())
      return
    }
    setMessage('Status updated.')
    await refreshTree(projectID)
  }

  async function deleteTask() {
    if (!deleteTargetID || !projectID) return
    const res = await fetch(`${apiBase}/tasks/${deleteTargetID}/delete`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ strategy: deleteStrategy })
    })
    if (!res.ok) {
      setMessage(await res.text())
      return
    }
    if (selectedTaskID === deleteTargetID) {
      setSelectedTaskID(null)
    }
    setDeleteDialogOpen(false)
    setMessage('Task deleted.')
    await refreshTree(projectID)
  }

  function openCreateTask(parentID: number | null) {
    setCreateParentID(parentID)
    setCreateDialogOpen(true)
  }

  function openDelete(taskID: number, strategy: DeleteStrategy) {
    setDeleteTargetID(taskID)
    setDeleteStrategy(strategy)
    setDeleteDialogOpen(true)
  }

  return (
    <main className="mx-auto grid h-screen max-w-[1600px] grid-cols-[260px_1fr_420px] gap-3 p-3">
      <Card className="h-full overflow-hidden">
        <CardHeader>
          <CardTitle className="text-2xl font-black">TasQ</CardTitle>
        </CardHeader>
        <CardContent className="flex h-[calc(100%-72px)] flex-col gap-4">
          <form onSubmit={createProject} className="space-y-2">
            <Input value={projectName} onChange={(e) => setProjectName(e.target.value)} placeholder="New project name" required />
            <Input value={projectDescription} onChange={(e) => setProjectDescription(e.target.value)} placeholder="Description" />
            <Button className="w-full" type="submit">
              <Plus className="h-4 w-4" />
              Create Project
            </Button>
          </form>

          <div className="flex-1 overflow-y-auto rounded-md border bg-muted/30 p-2">
            <p className="mb-2 text-xs font-semibold uppercase text-muted-foreground">Projects</p>
            <div className="space-y-1">
              {projects.map((project) => (
                <button
                  key={project.id}
                  className={`flex w-full items-center justify-between rounded-md px-2 py-2 text-left text-sm ${
                    project.id === projectID ? 'bg-primary text-primary-foreground' : 'hover:bg-accent'
                  }`}
                  onClick={() => setProjectID(project.id)}
                >
                  <span className="truncate">{project.name}</span>
                  <PencilLine className="h-3.5 w-3.5 opacity-60" />
                </button>
              ))}
            </div>
          </div>
        </CardContent>
      </Card>

      <Card className="h-full overflow-hidden">
        <CardHeader className="flex-row items-center justify-between space-y-0">
          <CardTitle>{selectedProject?.name ?? 'Project'}</CardTitle>
          <Button size="sm" onClick={() => openCreateTask(null)} disabled={!projectID}>
            <Plus className="h-4 w-4" />
            Root Task
          </Button>
        </CardHeader>
        <CardContent className="h-[calc(100%-72px)] overflow-x-auto overflow-y-auto">
          {tree.length === 0 && <p className="text-sm text-muted-foreground">No tasks yet.</p>}
          <div className="min-w-[760px] space-y-3 pb-6">
            {tree.map((node) => (
              <TaskBranch
                key={node.id}
                node={node}
                level={0}
                selectedTaskID={selectedTaskID}
                onSelect={setSelectedTaskID}
                onCreateChild={openCreateTask}
                onDelete={openDelete}
              />
            ))}
          </div>
        </CardContent>
      </Card>

      <Card className="h-full overflow-hidden">
        <CardHeader className="space-y-2">
          <CardTitle className="flex items-center gap-2">
            {selectedTask ? selectedTask.title : 'Task Detail'}
            {selectedTask?.status === 'in_progress' && <Badge variant="success">IN-PROGRESS</Badge>}
          </CardTitle>
          {selectedTask && (
            <Select value={selectedTask.status} onValueChange={(value) => void updateStatus(value as TaskNode['status'])}>
              <SelectTrigger>
                <SelectValue placeholder="Select status" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="planned">PLANNED</SelectItem>
                <SelectItem value="in_progress">IN-PROGRESS</SelectItem>
                <SelectItem value="done">DONE</SelectItem>
              </SelectContent>
            </Select>
          )}
        </CardHeader>
        <CardContent className="h-[calc(100%-88px)] overflow-y-auto space-y-3">
          {!selectedTask && <p className="text-sm text-muted-foreground">Select a task to edit.</p>}
          {selectedTask && (
            <>
              <Input value={draftTitle} onChange={(e) => setDraftTitle(e.target.value)} placeholder="Task title" />

              <section className="space-y-2">
                <p className="text-sm font-semibold">Plans</p>
                <Textarea value={draftSpec} onChange={(e) => setDraftSpec(e.target.value)} rows={7} />
                <div className="markdown-preview rounded-md border bg-muted/20 p-3">
                  <ReactMarkdown>{draftSpec || '_No plan markdown_'}</ReactMarkdown>
                </div>
              </section>

              <section className="space-y-2">
                <p className="text-sm font-semibold">Result</p>
                <Textarea value={draftResult} onChange={(e) => setDraftResult(e.target.value)} rows={7} />
                <div className="markdown-preview rounded-md border bg-muted/20 p-3">
                  <ReactMarkdown>{draftResult || '_No result markdown_'}</ReactMarkdown>
                </div>
              </section>

              <Button className="w-full" onClick={() => void saveTaskContent()}>
                Save Task
              </Button>
            </>
          )}

          {message && <p className="rounded-md bg-secondary px-3 py-2 text-xs text-secondary-foreground">{message}</p>}
        </CardContent>
      </Card>

      <Dialog open={createDialogOpen} onOpenChange={setCreateDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Create Task</DialogTitle>
            <DialogDescription>
              {createParentID ? `Create a child task under #${createParentID}` : 'Create a root-level task.'}
            </DialogDescription>
          </DialogHeader>
          <form className="space-y-3" onSubmit={createTask}>
            <Input value={newTaskTitle} onChange={(e) => setNewTaskTitle(e.target.value)} placeholder="Task title" required />
            <Textarea value={newTaskSpec} onChange={(e) => setNewTaskSpec(e.target.value)} placeholder="Task plan markdown" rows={6} />
            <Button className="w-full" type="submit">
              Create
            </Button>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Task</DialogTitle>
            <DialogDescription>
              Strategy: <code>{deleteStrategy}</code>
            </DialogDescription>
          </DialogHeader>
          <p className="text-sm text-muted-foreground">
            This action can remove the current task only, remove the entire subtree, or keep children and remove only the selected task.
          </p>
          <Button variant="destructive" onClick={() => void deleteTask()}>
            <Trash2 className="h-4 w-4" />
            Delete
          </Button>
        </DialogContent>
      </Dialog>
    </main>
  )
}

type TaskBranchProps = {
  node: TaskNode
  level: number
  selectedTaskID: number | null
  onSelect: (taskID: number) => void
  onCreateChild: (parentID: number) => void
  onDelete: (taskID: number, strategy: DeleteStrategy) => void
}

function TaskBranch({ node, level, selectedTaskID, onSelect, onCreateChild, onDelete }: TaskBranchProps) {
  const isSelected = selectedTaskID === node.id

  return (
    <div className="space-y-2" style={{ marginLeft: level * 18 }}>
      <ContextMenu>
        <ContextMenuTrigger>
          <button
            onClick={() => onSelect(node.id)}
            className={`flex w-fit min-w-[140px] items-center gap-2 rounded-md border px-3 py-2 text-sm ${
              isSelected ? 'border-primary bg-secondary' : 'bg-card hover:bg-accent/70'
            } ${node.status === 'in_progress' ? 'ring-1 ring-emerald-500' : ''}`}
          >
            <span className="font-medium">{node.title}</span>
            {node.status === 'done' && <Badge variant="secondary">DONE</Badge>}
          </button>
        </ContextMenuTrigger>

        <ContextMenuContent>
          <ContextMenuItem onClick={() => onCreateChild(node.id)}>Add child task</ContextMenuItem>
          <ContextMenuSeparator />
          <ContextMenuItem onClick={() => onDelete(node.id, 'delete_subtree')}>Delete task + subtree</ContextMenuItem>
          <ContextMenuItem onClick={() => onDelete(node.id, 'promote_children')}>Delete task only (keep children)</ContextMenuItem>
          <ContextMenuItem onClick={() => onDelete(node.id, 'delete_if_leaf')}>Delete only if leaf</ContextMenuItem>
        </ContextMenuContent>
      </ContextMenu>

      {node.children.length > 0 && (
        <div className="space-y-2 border-l border-dashed border-muted pl-4">
          {node.children.map((child) => (
            <TaskBranch
              key={child.id}
              node={child}
              level={level + 1}
              selectedTaskID={selectedTaskID}
              onSelect={onSelect}
              onCreateChild={onCreateChild}
              onDelete={onDelete}
            />
          ))}
        </div>
      )}
    </div>
  )
}
