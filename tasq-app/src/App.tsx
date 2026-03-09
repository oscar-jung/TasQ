import {
  FormEvent,
  MouseEvent as ReactMouseEvent,
  useEffect,
  useMemo,
  useRef,
  useState
} from 'react'
import { Check, Pencil, Plus, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { TreeNodeView, type TaskNode } from '@/components/task-tree/TreeNodeView'
import { StatusDropdown, type TaskStatus } from '@/components/task-detail/StatusDropdown'
import { MarkdownEditor } from '@/components/task-detail/MarkdownEditor'

type Project = {
  id: number
  name: string
  description: string
}

type TaskContentPatch = {
  title?: string
  spec_md?: string
  result_md?: string
}

type TaskDeleteStrategy = 'promote_children' | 'delete_subtree'
type TaskRunSummary = {
  id: number
  agent_id: string
  attempt_no: number
  status: 'running' | 'completed' | 'failed' | 'released'
  started_at: string
  finished_at: string | null
}

type TaskGitRefSummary = {
  id: number
  repo: string
  branch: string
  base_commit: string
  commit_sha: string
  created_at: string
}

type TaskContextResponse = {
  recent_runs: TaskRunSummary[]
  git_refs: TaskGitRefSummary[]
}

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
  const [isSavingExecutionPolicy, setIsSavingExecutionPolicy] = useState(false)
  const [isSavingGitLink, setIsSavingGitLink] = useState(false)
  const [taskMessage, setTaskMessage] = useState('')
  const [taskContext, setTaskContext] = useState<TaskContextResponse | null>(null)
  const [taskContextError, setTaskContextError] = useState('')
  const [isLoadingTaskContext, setIsLoadingTaskContext] = useState(false)
  const [maxAttemptsDraft, setMaxAttemptsDraft] = useState('1')
  const [gitRepoDraft, setGitRepoDraft] = useState('')
  const [gitBranchDraft, setGitBranchDraft] = useState('')
  const [gitBaseCommitDraft, setGitBaseCommitDraft] = useState('')
  const [gitCommitDraft, setGitCommitDraft] = useState('')
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
      setTaskContext(null)
      setTaskContextError('')
      setMaxAttemptsDraft('1')
      setGitRepoDraft('')
      setGitBranchDraft('')
      setGitBaseCommitDraft('')
      setGitCommitDraft('')
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
      setMaxAttemptsDraft(String(selectedTask.max_attempts))
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
    if (!isSavingExecutionPolicy) {
      setMaxAttemptsDraft(String(selectedTask.max_attempts))
    }
  }, [
    selectedTask,
    editingTaskTitle,
    isSavingTitle,
    isSpecDirty,
    isSavingSpec,
    isResultDirty,
    isSavingResult,
    isSavingExecutionPolicy
  ])

  useEffect(() => {
    if (!selectedTaskID) {
      setTaskContext(null)
      setTaskContextError('')
      setIsLoadingTaskContext(false)
      return
    }

    let cancelled = false
    const run = async () => {
      setIsLoadingTaskContext(true)
      setTaskContextError('')
      try {
        const ctx = await fetchTaskContext(selectedTaskID)
        if (cancelled) return
        setTaskContext(ctx)
      } catch (err) {
        if (cancelled) return
        const message = err instanceof Error ? err.message : 'failed to load task context'
        setTaskContextError(message)
        setTaskContext(null)
      } finally {
        if (!cancelled) {
          setIsLoadingTaskContext(false)
        }
      }
    }

    void run()
    return () => {
      cancelled = true
    }
  }, [selectedTaskID])

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

  async function fetchTaskContext(taskID: number) {
    const res = await fetch(`${apiBase}/tasks/${taskID}/context`)
    if (!res.ok) {
      const message = await res.text()
      throw new Error(message || 'failed to fetch task context')
    }
    return (await res.json()) as TaskContextResponse
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

  async function patchExecutionPolicy(taskID: number, maxAttempts: number) {
    const res = await fetch(`${apiBase}/tasks/${taskID}/execution-policy`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ max_attempts: maxAttempts })
    })
    if (!res.ok) {
      const message = await res.text()
      throw new Error(message || 'failed to update execution policy')
    }
    if (selectedProjectID) {
      await fetchTaskTree(selectedProjectID)
    }
    const ctx = await fetchTaskContext(taskID)
    setTaskContext(ctx)
  }

  async function linkTaskGitRef(
    taskID: number,
    payload: { repo: string; branch: string; base_commit: string; commit_sha: string }
  ) {
    const res = await fetch(`${apiBase}/tasks/${taskID}/git-link`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    })
    if (!res.ok) {
      const message = await res.text()
      throw new Error(message || 'failed to link git ref')
    }
    const ctx = await fetchTaskContext(taskID)
    setTaskContext(ctx)
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
      const ctx = await fetchTaskContext(selectedTask.id)
      setTaskContext(ctx)
      setTaskMessage('Task status updated.')
    } catch (err) {
      const message = err instanceof Error ? err.message : 'failed to update task status'
      setTaskMessage(message)
    } finally {
      setIsSavingStatus(false)
    }
  }

  async function saveExecutionPolicy() {
    if (!selectedTask) return
    const parsed = Number(maxAttemptsDraft)
    if (!Number.isInteger(parsed) || parsed < 1) {
      setTaskMessage('Max attempts must be an integer >= 1.')
      return
    }
    if (parsed === selectedTask.max_attempts) return

    setTaskMessage('')
    setIsSavingExecutionPolicy(true)
    try {
      await patchExecutionPolicy(selectedTask.id, parsed)
      setTaskMessage('Execution policy saved.')
    } catch (err) {
      const message = err instanceof Error ? err.message : 'failed to save execution policy'
      setTaskMessage(message)
    } finally {
      setIsSavingExecutionPolicy(false)
    }
  }

  async function saveGitLink(e: FormEvent) {
    e.preventDefault()
    if (!selectedTask) return
    const payload = {
      repo: gitRepoDraft.trim(),
      branch: gitBranchDraft.trim(),
      base_commit: gitBaseCommitDraft.trim(),
      commit_sha: gitCommitDraft.trim()
    }
    if (!payload.repo || !payload.branch || !payload.base_commit || !payload.commit_sha) {
      setTaskMessage('Repo, branch, base commit, and commit SHA are required.')
      return
    }

    setTaskMessage('')
    setIsSavingGitLink(true)
    try {
      await linkTaskGitRef(selectedTask.id, payload)
      setGitRepoDraft('')
      setGitBranchDraft('')
      setGitBaseCommitDraft('')
      setGitCommitDraft('')
      setTaskMessage('Git link added.')
    } catch (err) {
      const message = err instanceof Error ? err.message : 'failed to add git link'
      setTaskMessage(message)
    } finally {
      setIsSavingGitLink(false)
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

              <div className="rounded-md border bg-muted/20 p-3">
                <p className="text-xs uppercase tracking-wide text-muted-foreground">Execution</p>
                <div className="mt-2 flex items-end gap-2">
                  <div className="w-28">
                    <p className="mb-1 text-[11px] text-muted-foreground">Max attempts</p>
                    <Input
                      type="number"
                      min={1}
                      value={maxAttemptsDraft}
                      onChange={(e) => setMaxAttemptsDraft(e.target.value)}
                      className="h-9"
                    />
                  </div>
                  <Button size="sm" variant="outline" disabled={isSavingExecutionPolicy} onClick={() => void saveExecutionPolicy()}>
                    {isSavingExecutionPolicy ? 'Saving...' : 'Save policy'}
                  </Button>
                </div>

                <form className="mt-3 space-y-2" onSubmit={saveGitLink}>
                  <p className="text-[11px] text-muted-foreground">Link Git result</p>
                  <div className="grid grid-cols-2 gap-2">
                    <Input
                      placeholder="repo"
                      value={gitRepoDraft}
                      onChange={(e) => setGitRepoDraft(e.target.value)}
                      disabled={isSavingGitLink}
                    />
                    <Input
                      placeholder="branch"
                      value={gitBranchDraft}
                      onChange={(e) => setGitBranchDraft(e.target.value)}
                      disabled={isSavingGitLink}
                    />
                    <Input
                      placeholder="base commit"
                      value={gitBaseCommitDraft}
                      onChange={(e) => setGitBaseCommitDraft(e.target.value)}
                      disabled={isSavingGitLink}
                    />
                    <Input
                      placeholder="commit sha"
                      value={gitCommitDraft}
                      onChange={(e) => setGitCommitDraft(e.target.value)}
                      disabled={isSavingGitLink}
                    />
                  </div>
                  <div className="flex justify-end">
                    <Button type="submit" size="sm" variant="outline" disabled={isSavingGitLink}>
                      {isSavingGitLink ? 'Linking...' : 'Add Git Link'}
                    </Button>
                  </div>
                </form>

                {isLoadingTaskContext && <p className="mt-3 text-xs text-muted-foreground">Loading execution context...</p>}
                {taskContextError && <p className="mt-3 text-xs text-red-300">{taskContextError}</p>}

                {taskContext && (
                  <div className="mt-3 grid gap-3">
                    <div>
                      <p className="text-[11px] uppercase tracking-wide text-muted-foreground">Recent Runs</p>
                      {taskContext.recent_runs.length === 0 && (
                        <p className="mt-1 text-xs text-muted-foreground">No run history yet.</p>
                      )}
                      {taskContext.recent_runs.length > 0 && (
                        <div className="mt-1 max-h-32 space-y-1 overflow-y-auto rounded-md border bg-background/50 p-2">
                          {taskContext.recent_runs.map((run) => (
                            <div key={run.id} className="flex items-center justify-between text-xs">
                              <span className="truncate text-muted-foreground">
                                #{run.attempt_no} {run.agent_id}
                              </span>
                              <span className="uppercase text-foreground/85">{run.status}</span>
                            </div>
                          ))}
                        </div>
                      )}
                    </div>

                    <div>
                      <p className="text-[11px] uppercase tracking-wide text-muted-foreground">Git Refs</p>
                      {taskContext.git_refs.length === 0 && (
                        <p className="mt-1 text-xs text-muted-foreground">No git refs linked yet.</p>
                      )}
                      {taskContext.git_refs.length > 0 && (
                        <div className="mt-1 max-h-32 space-y-1 overflow-y-auto rounded-md border bg-background/50 p-2">
                          {taskContext.git_refs.map((ref) => (
                            <div key={ref.id} className="text-xs">
                              <p className="truncate text-foreground/90">
                                {ref.repo} · {ref.branch}
                              </p>
                              <p className="truncate text-muted-foreground">
                                {ref.base_commit.slice(0, 12)} → {ref.commit_sha.slice(0, 12)}
                              </p>
                            </div>
                          ))}
                        </div>
                      )}
                    </div>
                  </div>
                )}
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
