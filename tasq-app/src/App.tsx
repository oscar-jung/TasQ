import {
  FormEvent,
  MouseEvent as ReactMouseEvent,
  useEffect,
  useMemo,
  useRef,
  useState
} from 'react'
import { Check, ChevronDown, Pencil, Plus, X } from 'lucide-react'
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

type TaskStatus = 'planned' | 'in_progress' | 'done'

type TaskContentPatch = {
  title?: string
  spec_md?: string
  result_md?: string
}

type TaskDeleteStrategy = 'promote_children' | 'delete_subtree'
type TreeValidationReport = {
  project_id: number
  checked_tasks: number
  missing_parent_task_ids: number[]
  cycle_task_ids: number[]
  status_violation_task_ids: number[]
  cleansed_task_ids: number[]
  valid: boolean
  cleansed: boolean
}

const apiBase = import.meta.env.VITE_API_BASE ?? 'http://localhost:8080'
const NODE_WIDTH = 180
const NODE_GAP = 56
const CONNECTOR_TOP = 30
const CONNECTOR_BOTTOM = 38

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

export function App() {
  const [projects, setProjects] = useState<Project[]>([])
  const [selectedProjectID, setSelectedProjectID] = useState<number | null>(null)
  const [newProjectName, setNewProjectName] = useState('')

  const [editingProjectID, setEditingProjectID] = useState<number | null>(null)
  const [editingProjectName, setEditingProjectName] = useState('')

  const [tree, setTree] = useState<TaskNode[]>([])
  const [selectedTaskID, setSelectedTaskID] = useState<number | null>(null)
  const [newRootTaskTitle, setNewRootTaskTitle] = useState('')

  const [editingTaskTitle, setEditingTaskTitle] = useState(false)
  const [taskTitleDraft, setTaskTitleDraft] = useState('')
  const [taskSpecDraft, setTaskSpecDraft] = useState('')
  const [taskResultDraft, setTaskResultDraft] = useState('')
  const [isSpecDirty, setIsSpecDirty] = useState(false)
  const [isResultDirty, setIsResultDirty] = useState(false)
  const [isSavingTitle, setIsSavingTitle] = useState(false)
  const [isSavingSpec, setIsSavingSpec] = useState(false)
  const [isSavingResult, setIsSavingResult] = useState(false)
  const [isSavingStatus, setIsSavingStatus] = useState(false)
  const [taskMessage, setTaskMessage] = useState('')
  const [addChildModalTask, setAddChildModalTask] = useState<TaskNode | null>(null)
  const [addChildTitle, setAddChildTitle] = useState('')
  const [isAddingChild, setIsAddingChild] = useState(false)
  const [addChildError, setAddChildError] = useState('')
  const [deleteModal, setDeleteModal] = useState<{ task: TaskNode; strategy: TaskDeleteStrategy } | null>(null)
  const [isDeletingTask, setIsDeletingTask] = useState(false)
  const [isValidatingTree, setIsValidatingTree] = useState(false)
  const [validationError, setValidationError] = useState('')
  const [validationDialog, setValidationDialog] = useState<TreeValidationReport | null>(null)
  const [isCleansingTree, setIsCleansingTree] = useState(false)

  const treeViewportRef = useRef<HTMLDivElement | null>(null)
  const isPanningRef = useRef(false)
  const panOriginRef = useRef({ x: 0, y: 0, left: 0, top: 0 })
  const [isPanningUI, setIsPanningUI] = useState(false)
  const syncedTaskIDRef = useRef<number | null>(null)
  const lastValidationAlertKeyRef = useRef('')

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

  const taskByID = useMemo(() => {
    const out = new Map<number, TaskNode>()
    const walk = (node: TaskNode) => {
      out.set(node.id, node)
      node.children.forEach(walk)
    }
    tree.forEach(walk)
    return out
  }, [tree])

  const selectedTask = useMemo(
    () => (selectedTaskID ? taskByID.get(selectedTaskID) ?? null : null),
    [selectedTaskID, taskByID]
  )

  useEffect(() => {
    void fetchProjects()
  }, [])

  useEffect(() => {
    if (selectedProjectID) {
      void fetchTaskTree(selectedProjectID)
    } else {
      setTree([])
      setSelectedTaskID(null)
      setValidationDialog(null)
      setValidationError('')
    }
  }, [selectedProjectID])

  useEffect(() => {
    if (!selectedProjectID) return

    void runTreeValidation(selectedProjectID, false, 'auto')
    const timer = window.setInterval(() => {
      void runTreeValidation(selectedProjectID, false, 'auto')
    }, 15000)

    return () => {
      window.clearInterval(timer)
    }
  }, [selectedProjectID])

  useEffect(() => {
    if (!selectedTaskID) return
    if (!taskByID.has(selectedTaskID)) {
      setSelectedTaskID(null)
    }
  }, [taskByID, selectedTaskID])

  useEffect(() => {
    if (!selectedTask) {
      syncedTaskIDRef.current = null
      setEditingTaskTitle(false)
      setTaskTitleDraft('')
      setTaskSpecDraft('')
      setTaskResultDraft('')
      setIsSpecDirty(false)
      setIsResultDirty(false)
      setTaskMessage('')
      return
    }

    if (syncedTaskIDRef.current !== selectedTask.id) {
      syncedTaskIDRef.current = selectedTask.id
      setEditingTaskTitle(false)
      setTaskTitleDraft(selectedTask.title)
      setTaskSpecDraft(selectedTask.spec_md)
      setTaskResultDraft(selectedTask.result_md)
      setIsSpecDirty(false)
      setIsResultDirty(false)
      setTaskMessage('')
      return
    }

    if (!editingTaskTitle && !isSavingTitle) {
      setTaskTitleDraft(selectedTask.title)
    }
    if (!isSpecDirty && !isSavingSpec) {
      setTaskSpecDraft(selectedTask.spec_md)
    }
    if (!isResultDirty && !isSavingResult) {
      setTaskResultDraft(selectedTask.result_md)
    }
  }, [selectedTask, editingTaskTitle, isSavingTitle, isSpecDirty, isSavingSpec, isResultDirty, isSavingResult])

  function handleSpecDraftChange(next: string) {
    setTaskSpecDraft(next)
    if (!selectedTask) return
    setIsSpecDirty(next !== selectedTask.spec_md)
  }

  function handleResultDraftChange(next: string) {
    setTaskResultDraft(next)
    if (!selectedTask) return
    setIsResultDirty(next !== selectedTask.result_md)
  }

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

    if (!selectedTaskID && data.length > 0) {
      setSelectedTaskID(data[0].id)
    }
  }

  async function patchTaskContent(taskID: number, payload: TaskContentPatch) {
    const res = await fetch(`${apiBase}/tasks/${taskID}/content`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    })
    if (!res.ok) {
      const message = await res.text()
      throw new Error(message || 'failed to update task content')
    }

    if (selectedProjectID) {
      await fetchTaskTree(selectedProjectID)
    }
  }

  function hasValidationIssues(report: TreeValidationReport) {
    return (
      report.missing_parent_task_ids.length > 0 ||
      report.cycle_task_ids.length > 0 ||
      report.status_violation_task_ids.length > 0
    )
  }

  function validationAlertKey(report: TreeValidationReport) {
    return JSON.stringify({
      p: report.project_id,
      m: report.missing_parent_task_ids,
      c: report.cycle_task_ids,
      s: report.status_violation_task_ids
    })
  }

  async function validateProjectTree(projectID: number, cleanse: boolean) {
    const res = await fetch(`${apiBase}/projects/${projectID}/tasks/validate`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ cleanse })
    })
    if (!res.ok) {
      const message = await res.text()
      throw new Error(message || 'failed to validate task tree')
    }
    return (await res.json()) as TreeValidationReport
  }

  async function runTreeValidation(projectID: number, cleanse: boolean, source: 'manual' | 'auto') {
    if (source === 'manual') {
      setIsValidatingTree(true)
      setValidationError('')
    }

    try {
      const report = await validateProjectTree(projectID, cleanse)
      const hasIssues = hasValidationIssues(report)
      const key = validationAlertKey(report)

      if (source === 'manual') {
        setValidationDialog(report)
      } else if (hasIssues && key !== lastValidationAlertKeyRef.current) {
        setValidationDialog(report)
      }

      if (hasIssues) {
        lastValidationAlertKeyRef.current = key
      } else if (source === 'manual') {
        lastValidationAlertKeyRef.current = ''
      }

      if (cleanse) {
        setTaskMessage(report.cleansed_task_ids.length > 0 ? 'Tree cleanse completed.' : 'No cleanup needed.')
        if (selectedProjectID) {
          await fetchTaskTree(selectedProjectID)
        }
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : 'failed to validate task tree'
      if (source === 'manual') {
        setValidationError(message)
      }
    } finally {
      if (source === 'manual') {
        setIsValidatingTree(false)
      }
    }
  }

  async function runTreeCleanseFromDialog() {
    if (!selectedProjectID) return
    setIsCleansingTree(true)
    try {
      await runTreeValidation(selectedProjectID, true, 'manual')
    } finally {
      setIsCleansingTree(false)
    }
  }

  async function patchTaskStatus(taskID: number, status: TaskStatus) {
    const res = await fetch(`${apiBase}/tasks/${taskID}/status`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ status })
    })
    if (!res.ok) {
      const message = await res.text()
      throw new Error(message || 'failed to update task status')
    }

    if (selectedProjectID) {
      await fetchTaskTree(selectedProjectID)
    }
  }

  async function moveTask(taskID: number, newParentTaskID: number | null) {
    const res = await fetch(`${apiBase}/tasks/${taskID}/move`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ new_parent_task_id: newParentTaskID })
    })
    if (!res.ok) {
      const message = await res.text()
      throw new Error(message || 'failed to move task')
    }

    if (selectedProjectID) {
      await fetchTaskTree(selectedProjectID)
    }
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

  function openAddChildModal(task: TaskNode) {
    setAddChildModalTask(task)
    setAddChildTitle('')
    setAddChildError('')
  }

  function closeAddChildModal() {
    if (isAddingChild) return
    setAddChildModalTask(null)
    setAddChildTitle('')
    setAddChildError('')
  }

  async function submitAddChildTask(e: FormEvent) {
    e.preventDefault()
    if (!addChildModalTask) return
    const title = addChildTitle.trim()
    if (!title) {
      setAddChildError('Task name is required.')
      return
    }

    setAddChildError('')
    setIsAddingChild(true)
    try {
      const res = await fetch(`${apiBase}/projects/${addChildModalTask.project_id}/tasks`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          parent_task_id: addChildModalTask.id,
          title,
          spec_md: ''
        })
      })
      if (!res.ok) {
        const message = await res.text()
        throw new Error(message || 'failed to create child task')
      }
      await fetchTaskTree(addChildModalTask.project_id)
      setSelectedTaskID(addChildModalTask.id)
      setTaskMessage('Child task added.')
      closeAddChildModal()
    } catch (err) {
      const message = err instanceof Error ? err.message : 'failed to create child task'
      setAddChildError(message)
    } finally {
      setIsAddingChild(false)
    }
  }

  function openDeleteModal(task: TaskNode, strategy: TaskDeleteStrategy) {
    setDeleteModal({ task, strategy })
  }

  function closeDeleteModal() {
    if (isDeletingTask) return
    setDeleteModal(null)
  }

  async function submitDeleteTask() {
    if (!deleteModal) return
    const { task, strategy } = deleteModal
    setIsDeletingTask(true)
    try {
      const res = await fetch(`${apiBase}/tasks/${task.id}/delete`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ strategy })
      })
      if (!res.ok) {
        const message = await res.text()
        throw new Error(message || 'failed to delete task')
      }
      await fetchTaskTree(task.project_id)
      setSelectedTaskID(task.parent_task_id ?? null)
      setTaskMessage('Task deleted.')
      closeDeleteModal()
    } catch (err) {
      const message = err instanceof Error ? err.message : 'failed to delete task'
      setTaskMessage(message)
    } finally {
      setIsDeletingTask(false)
    }
  }

  async function saveTaskTitle() {
    if (!selectedTask) return
    const trimmed = taskTitleDraft.trim()
    if (!trimmed || trimmed === selectedTask.title) {
      setEditingTaskTitle(false)
      setTaskTitleDraft(selectedTask.title)
      return
    }

    setTaskMessage('')
    setIsSavingTitle(true)
    try {
      await patchTaskContent(selectedTask.id, { title: trimmed })
      setEditingTaskTitle(false)
      setTaskMessage('Task title saved.')
    } catch (err) {
      const message = err instanceof Error ? err.message : 'failed to save task title'
      setTaskMessage(message)
    } finally {
      setIsSavingTitle(false)
    }
  }

  async function saveTaskSpec() {
    if (!selectedTask || taskSpecDraft === selectedTask.spec_md) return
    setTaskMessage('')
    setIsSavingSpec(true)
    try {
      await patchTaskContent(selectedTask.id, { spec_md: taskSpecDraft })
      setIsSpecDirty(false)
      setTaskMessage('Task spec saved.')
    } catch (err) {
      const message = err instanceof Error ? err.message : 'failed to save task spec'
      setTaskMessage(message)
    } finally {
      setIsSavingSpec(false)
    }
  }

  async function saveTaskResult() {
    if (!selectedTask || taskResultDraft === selectedTask.result_md) return
    setTaskMessage('')
    setIsSavingResult(true)
    try {
      await patchTaskContent(selectedTask.id, { result_md: taskResultDraft })
      setIsResultDirty(false)
      setTaskMessage('Task result saved.')
    } catch (err) {
      const message = err instanceof Error ? err.message : 'failed to save task result'
      setTaskMessage(message)
    } finally {
      setIsSavingResult(false)
    }
  }

  async function updateTaskStatus(nextStatus: TaskStatus) {
    if (!selectedTask || nextStatus === selectedTask.status) return
    setTaskMessage('')
    setIsSavingStatus(true)
    try {
      await patchTaskStatus(selectedTask.id, nextStatus)
      setTaskMessage('Task status updated.')
    } catch (err) {
      const message = err instanceof Error ? err.message : 'failed to update task status'
      setTaskMessage(message)
    } finally {
      setIsSavingStatus(false)
    }
  }

  async function promoteTaskOneLevel(task: TaskNode) {
    if (task.parent_task_id === null) {
      setTaskMessage('Task is already at root level.')
      return
    }

    const parent = taskByID.get(task.parent_task_id)
    const grandParentID = parent?.parent_task_id ?? null

    setTaskMessage('')
    try {
      await moveTask(task.id, grandParentID)
      setTaskMessage('Task promoted one level.')
    } catch (err) {
      const message = err instanceof Error ? err.message : 'failed to promote task'
      setTaskMessage(message)
    }
  }

  async function promoteTaskToRoot(task: TaskNode) {
    if (task.parent_task_id === null) {
      setTaskMessage('Task is already a root task.')
      return
    }

    setTaskMessage('')
    try {
      await moveTask(task.id, null)
      setTaskMessage('Task promoted to root.')
    } catch (err) {
      const message = err instanceof Error ? err.message : 'failed to promote task to root'
      setTaskMessage(message)
    }
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
    <main className="mx-auto grid h-screen min-w-[1520px] w-full max-w-[1880px] grid-cols-[300px_minmax(760px,1fr)_420px] gap-4 px-6 py-4">
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
                      if (!isEditing) {
                        setSelectedProjectID(project.id)
                        setSelectedTaskID(null)
                      }
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
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={!selectedProjectID || isValidatingTree}
              onClick={() => {
                if (!selectedProjectID) return
                void runTreeValidation(selectedProjectID, false, 'manual')
              }}
            >
              {isValidatingTree ? 'Validating...' : 'Validate'}
            </Button>
          </form>
          {validationError && <p className="text-xs text-red-300">{validationError}</p>}

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
                  <TreeNodeView
                    key={root.id}
                    node={root}
                    subtreeUnits={subtreeUnits}
                    selectedTaskID={selectedTaskID}
                    onSelectTask={setSelectedTaskID}
                  />
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
          {!selectedTask && (
            <div className="h-full rounded-md border border-dashed bg-muted/30 p-4 text-sm text-muted-foreground">
              Select a task in the tree.
            </div>
          )}

          {selectedTask && (
            <div className="flex h-full flex-col gap-4">
              <div className="rounded-md border bg-muted/20 p-3">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0 flex-1">
                    <p className="text-xs uppercase tracking-wide text-muted-foreground">Task Name</p>
                    {!editingTaskTitle && (
                      <div className="mt-2 flex items-center gap-2">
                        <p className="min-w-0 flex-1 truncate text-base font-semibold">{selectedTask.title}</p>
                        <Button size="sm" variant="ghost" onClick={() => setEditingTaskTitle(true)}>
                          <Pencil className="h-4 w-4" />
                        </Button>
                      </div>
                    )}

                    {editingTaskTitle && (
                      <div className="mt-2 flex items-center gap-2">
                        <Input
                          autoFocus
                          value={taskTitleDraft}
                          onChange={(e) => setTaskTitleDraft(e.target.value)}
                          className="h-9"
                        />
                        <Button size="sm" variant="ghost" disabled={isSavingTitle} onClick={() => void saveTaskTitle()}>
                          <Check className="h-4 w-4" />
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          disabled={isSavingTitle}
                          onClick={() => {
                            setEditingTaskTitle(false)
                            setTaskTitleDraft(selectedTask.title)
                          }}
                        >
                          <X className="h-4 w-4" />
                        </Button>
                      </div>
                    )}
                  </div>

                  <div className="w-[170px] shrink-0">
                    <p className="text-xs uppercase tracking-wide text-muted-foreground">Status</p>
                    <div className="mt-2 flex items-center gap-2">
                      <StatusDropdown
                        value={selectedTask.status}
                        parentStatus={
                          selectedTask.parent_task_id ? (taskByID.get(selectedTask.parent_task_id)?.status ?? null) : null
                        }
                        disabled={isSavingStatus}
                        onChange={(status) => void updateTaskStatus(status)}
                      />
                    </div>
                  </div>
                </div>

                {taskMessage && <p className="mt-2 text-xs text-muted-foreground">{taskMessage}</p>}
              </div>

              <div className="rounded-md border bg-muted/20 p-3">
                <p className="text-xs uppercase tracking-wide text-muted-foreground">Task Actions</p>
                <div className="mt-2 flex flex-wrap gap-2">
                  <Button size="sm" variant="outline" onClick={() => openAddChildModal(selectedTask)}>
                    Add child task
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={selectedTask.parent_task_id === null}
                    onClick={() => void promoteTaskOneLevel(selectedTask)}
                  >
                    Promote
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={selectedTask.parent_task_id === null}
                    onClick={() => void promoteTaskToRoot(selectedTask)}
                  >
                    Promote (to root)
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => openDeleteModal(selectedTask, 'promote_children')}
                  >
                    Delete (promote children)
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    className="text-red-300 hover:text-red-200"
                    onClick={() => openDeleteModal(selectedTask, 'delete_subtree')}
                  >
                    Delete (with subtree)
                  </Button>
                </div>
              </div>

              <MarkdownEditor
                label="Task Spec"
                value={taskSpecDraft}
                onChange={handleSpecDraftChange}
                onSave={() => void saveTaskSpec()}
                isSaving={isSavingSpec}
              />

              <MarkdownEditor
                label="Task Result"
                value={taskResultDraft}
                onChange={handleResultDraftChange}
                onSave={() => void saveTaskResult()}
                isSaving={isSavingResult}
              />
            </div>
          )}
        </CardContent>
      </Card>

      {addChildModalTask && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
          <div className="w-full max-w-md rounded-lg border border-border bg-card p-4 shadow-2xl">
            <p className="text-sm font-semibold">Add Child Task</p>
            <p className="mt-1 text-xs text-muted-foreground">
              Parent: <span className="text-foreground">{addChildModalTask.title}</span>
            </p>
            <form className="mt-3 space-y-3" onSubmit={submitAddChildTask}>
              <Input
                autoFocus
                placeholder="Child task name"
                value={addChildTitle}
                onChange={(e) => setAddChildTitle(e.target.value)}
                disabled={isAddingChild}
              />
              {addChildError && <p className="text-xs text-red-300">{addChildError}</p>}
              <div className="flex justify-end gap-2">
                <Button type="button" variant="ghost" onClick={closeAddChildModal} disabled={isAddingChild}>
                  Cancel
                </Button>
                <Button type="submit" variant="outline" disabled={isAddingChild}>
                  {isAddingChild ? 'Adding...' : 'Add'}
                </Button>
              </div>
            </form>
          </div>
        </div>
      )}

      {deleteModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
          <div className="w-full max-w-md rounded-lg border border-border bg-card p-4 shadow-2xl">
            <p className="text-sm font-semibold">
              {deleteModal.strategy === 'promote_children' ? 'Delete Task (Promote Children)' : 'Delete Task (With Subtree)'}
            </p>
            <p className="mt-2 text-sm text-muted-foreground">
              {deleteModal.strategy === 'promote_children'
                ? 'The selected task will be deleted and its children will move up one level.'
                : 'The selected task and all descendant tasks will be permanently deleted.'}
            </p>
            <p className="mt-2 text-xs text-muted-foreground">
              Target: <span className="text-foreground">{deleteModal.task.title}</span>
            </p>
            <div className="mt-4 flex justify-end gap-2">
              <Button type="button" variant="ghost" onClick={closeDeleteModal} disabled={isDeletingTask}>
                Cancel
              </Button>
              <Button type="button" variant="outline" onClick={() => void submitDeleteTask()} disabled={isDeletingTask}>
                {isDeletingTask ? 'Deleting...' : 'Delete'}
              </Button>
            </div>
          </div>
        </div>
      )}

      {validationDialog && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
          <div className="w-full max-w-xl rounded-lg border border-border bg-card p-4 shadow-2xl">
            <p className="text-sm font-semibold">Tree Validation Report</p>
            <p className="mt-1 text-xs text-muted-foreground">
              Project #{validationDialog.project_id} / checked {validationDialog.checked_tasks} tasks
            </p>

            <div className="mt-3 space-y-2 text-sm">
              <p>Missing parent links: {validationDialog.missing_parent_task_ids.length}</p>
              <p>Cycle tasks: {validationDialog.cycle_task_ids.length}</p>
              <p>Status violations: {validationDialog.status_violation_task_ids.length}</p>
              <p>Cleansed tasks: {validationDialog.cleansed_task_ids.length}</p>
            </div>

            <div className="mt-4 flex justify-end gap-2">
              <Button type="button" variant="ghost" onClick={() => setValidationDialog(null)} disabled={isCleansingTree}>
                Close
              </Button>
              <Button
                type="button"
                variant="outline"
                disabled={isCleansingTree || validationDialog.status_violation_task_ids.length === 0 || !selectedProjectID}
                onClick={() => void runTreeCleanseFromDialog()}
              >
                {isCleansingTree ? 'Cleansing...' : 'Cleanse Status Violations'}
              </Button>
            </div>
          </div>
        </div>
      )}

    </main>
  )
}

function nodeSpanWidth(units: number) {
  return units * (NODE_WIDTH + NODE_GAP) - NODE_GAP
}

function TreeNodeView({
  node,
  subtreeUnits,
  selectedTaskID,
  onSelectTask
}: {
  node: TaskNode
  subtreeUnits: Map<number, number>
  selectedTaskID: number | null
  onSelectTask: (taskID: number) => void
}) {
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
      <button
        type="button"
        onClick={() => onSelectTask(node.id)}
        className={`w-[180px] rounded-lg border bg-card px-4 py-2 text-center text-sm shadow-[0_0_0_1px_#49566a] transition-colors ${
          selectedTaskID === node.id
            ? 'border-[#7c8da3] bg-secondary/70'
            : 'border-[#556276] hover:border-[#6a7a90] hover:bg-secondary/40'
        }`}
      >
        <p className="truncate font-semibold text-foreground">{node.title}</p>
        <p className="mt-1 flex items-center justify-center gap-1.5 text-[11px] uppercase tracking-wide text-foreground/75">
          <span className={`h-2 w-2 rounded-full ${statusDotClass(node.status)}`} />
          <span>{statusLabel(node.status)}</span>
        </p>
      </button>

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
              stroke="#4e5d72"
              strokeWidth="1"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
            {childLayouts.map((layout) => {
              if (!hasManyChildren || layout.center === centerX) {
                return (
                  <path
                    key={`line-${layout.child.id}`}
                    d={`M ${centerX} ${CONNECTOR_TOP} V ${CONNECTOR_TOP + CONNECTOR_BOTTOM}`}
                    stroke="#4e5d72"
                    strokeWidth="1"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                  />
                )
              }

              const elbowY = CONNECTOR_TOP + 10
              const branchY = CONNECTOR_TOP + 20
              const direction = layout.center > centerX ? 1 : -1
              const corner = 8
              return (
                <path
                  key={`line-${layout.child.id}`}
                  d={[
                    `M ${centerX} ${CONNECTOR_TOP}`,
                    `V ${elbowY - corner}`,
                    `Q ${centerX} ${elbowY} ${centerX + direction * corner} ${elbowY}`,
                    `H ${layout.center - direction * corner}`,
                    `Q ${layout.center} ${elbowY} ${layout.center} ${elbowY + corner}`,
                    `V ${branchY}`,
                    `V ${CONNECTOR_TOP + CONNECTOR_BOTTOM}`
                  ].join(' ')}
                  stroke="#4e5d72"
                  strokeWidth="1"
                  fill="none"
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
                  <TreeNodeView
                    node={layout.child}
                    subtreeUnits={subtreeUnits}
                    selectedTaskID={selectedTaskID}
                    onSelectTask={onSelectTask}
                  />
                </div>
              )
            })}
          </div>
        </div>
      )}
    </div>
  )
}

function StatusDropdown({
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

function MarkdownEditor({
  label,
  value,
  onChange,
  onSave,
  isSaving
}: {
  label: string
  value: string
  onChange: (next: string) => void
  onSave: () => void
  isSaving: boolean
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
            className="h-full min-h-[140px] flex-1 resize-none bg-transparent p-3 text-sm outline-none"
          />
        </div>
      )}

      {mode === 'preview' && (
        <div
          className="markdown-preview min-h-[140px] flex-1 overflow-auto p-3 text-sm"
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
