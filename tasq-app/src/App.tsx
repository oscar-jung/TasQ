import {
  FormEvent,
  MouseEvent as ReactMouseEvent,
  useEffect,
  useMemo,
  useRef,
  useState
} from 'react'
import { Check, Pencil, Plus, Trash2, X } from 'lucide-react'
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

type TaskEventSummary = {
  id: number
  task_id: number
  project_id: number
  event_type: string
  actor_type: string
  actor_id: string
  payload: Record<string, unknown>
  created_at: string
}

type TaskEventsResponse = {
  task_id: number
  project_id: number
  events: TaskEventSummary[]
}

type ProjectEventsResponse = {
  project_id: number
  events: TaskEventSummary[]
}

type RuntimeClaimAlert = {
  claim_id: number
  task_id: number
  agent_id: string
  lease_until: string
  heartbeat_at: string | null
  seconds_since_heartbeat: number
}

type OrphanTaskAlert = {
  task_id: number
  started_at: string | null
}

type ExhaustedTaskAlert = {
  task_id: number
  title: string
  status: string
  attempts_used: number
  max_attempts: number
}

type RuntimeAlertsResponse = {
  project_id: number
  heartbeat_stale_seconds: number
  total_tasks: number
  done_tasks: number
  unfinished_tasks: number
  claimable_tasks: number
  blocked_planned_tasks: number
  stale_active_claims: RuntimeClaimAlert[]
  heartbeat_overdue_claims: RuntimeClaimAlert[]
  orphan_in_progress_tasks: OrphanTaskAlert[]
  exhausted_tasks: ExhaustedTaskAlert[]
}

type ProjectAlertBadge = {
  stale: number
  overdue: number
  orphan: number
  exhausted: number
  score: number
  loading: boolean
  error: boolean
}

type TaskCapabilitiesResponse = {
  task_id: number
  required_capabilities: string[]
}

type ClaimNextResponse = {
  task: TaskNode | null
  claim?: {
    id: number
    token: string
    attempt_no: number
    task_run_id: number
    lease_seconds: number
  }
  message?: string
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

function buildProjectAlertBadge(data: RuntimeAlertsResponse): ProjectAlertBadge {
  return {
    stale: data.stale_active_claims.length,
    overdue: data.heartbeat_overdue_claims.length,
    orphan: data.orphan_in_progress_tasks.length,
    exhausted: data.exhausted_tasks.length,
    score:
      data.stale_active_claims.length +
      data.heartbeat_overdue_claims.length +
      data.orphan_in_progress_tasks.length +
      data.exhausted_tasks.length,
    loading: false,
    error: false
  }
}

export function App() {
  const [projects, setProjects] = useState<Project[]>([])
  const [projectAlertBadges, setProjectAlertBadges] = useState<Record<number, ProjectAlertBadge>>({})
  const [selectedProjectID, setSelectedProjectID] = useState<number | null>(null)
  const [newProjectName, setNewProjectName] = useState('')

  const [editingProjectID, setEditingProjectID] = useState<number | null>(null)
  const [editingProjectName, setEditingProjectName] = useState('')
  const [deleteProjectModal, setDeleteProjectModal] = useState<Project | null>(null)
  const [isDeletingProject, setIsDeletingProject] = useState(false)

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
  const [isSavingCapabilities, setIsSavingCapabilities] = useState(false)
  const [isAgentActionRunning, setIsAgentActionRunning] = useState(false)
  const [taskMessage, setTaskMessage] = useState('')
  const [taskContext, setTaskContext] = useState<TaskContextResponse | null>(null)
  const [taskContextError, setTaskContextError] = useState('')
  const [isLoadingTaskContext, setIsLoadingTaskContext] = useState(false)
  const [taskEvents, setTaskEvents] = useState<TaskEventSummary[]>([])
  const [projectEvents, setProjectEvents] = useState<TaskEventSummary[]>([])
  const [runtimeAlerts, setRuntimeAlerts] = useState<RuntimeAlertsResponse | null>(null)
  const [isLoadingRuntimeAlerts, setIsLoadingRuntimeAlerts] = useState(false)
  const [runtimeAlertsError, setRuntimeAlertsError] = useState('')
  const [heartbeatAlertThresholdDraft, setHeartbeatAlertThresholdDraft] = useState('300')
  const [eventTypeFilter, setEventTypeFilter] = useState('')
  const [eventActorFilter, setEventActorFilter] = useState('')
  const [eventFromDraft, setEventFromDraft] = useState('')
  const [eventToDraft, setEventToDraft] = useState('')
  const [runStatusFilter, setRunStatusFilter] = useState<'all' | 'running' | 'completed' | 'failed' | 'released'>('all')
  const [runAgentFilter, setRunAgentFilter] = useState('')
  const [isLoadingTaskEvents, setIsLoadingTaskEvents] = useState(false)
  const [isLoadingProjectEvents, setIsLoadingProjectEvents] = useState(false)
  const [taskEventsError, setTaskEventsError] = useState('')
  const [projectEventsError, setProjectEventsError] = useState('')
  const [maxAttemptsDraft, setMaxAttemptsDraft] = useState('1')
  const [gitRepoDraft, setGitRepoDraft] = useState('')
  const [gitBranchDraft, setGitBranchDraft] = useState('')
  const [gitBaseCommitDraft, setGitBaseCommitDraft] = useState('')
  const [gitCommitDraft, setGitCommitDraft] = useState('')
  const [requiredCapabilitiesDraft, setRequiredCapabilitiesDraft] = useState('')
  const [agentIDDraft, setAgentIDDraft] = useState('agent-local')
  const [leaseSecondsDraft, setLeaseSecondsDraft] = useState('120')
  const [claimTokenDraft, setClaimTokenDraft] = useState('')
  const [agentCapabilitiesDraft, setAgentCapabilitiesDraft] = useState('')
  const [failReasonDraft, setFailReasonDraft] = useState('')
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

  const filteredRuns = useMemo(() => {
    if (!taskContext) return []
    const agentNeedle = runAgentFilter.trim().toLowerCase()
    return taskContext.recent_runs.filter((run) => {
      if (runStatusFilter !== 'all' && run.status !== runStatusFilter) return false
      if (agentNeedle && !run.agent_id.toLowerCase().includes(agentNeedle)) return false
      return true
    })
  }, [taskContext, runStatusFilter, runAgentFilter])

  const filteredTaskEvents = useMemo(() => {
    const typeNeedle = eventTypeFilter.trim().toLowerCase()
    const actorNeedle = eventActorFilter.trim().toLowerCase()
    const fromTime = eventFromDraft ? new Date(eventFromDraft).getTime() : null
    const toTime = eventToDraft ? new Date(eventToDraft).getTime() : null
    return taskEvents.filter((event) => {
      if (typeNeedle && !event.event_type.toLowerCase().includes(typeNeedle)) return false
      const actor = `${event.actor_type}:${event.actor_id}`.toLowerCase()
      if (actorNeedle && !actor.includes(actorNeedle)) return false
      const t = new Date(event.created_at).getTime()
      if (fromTime && !Number.isNaN(fromTime) && t < fromTime) return false
      if (toTime && !Number.isNaN(toTime) && t > toTime) return false
      return true
    })
  }, [taskEvents, eventTypeFilter, eventActorFilter, eventFromDraft, eventToDraft])

  const filteredProjectEvents = useMemo(() => {
    const typeNeedle = eventTypeFilter.trim().toLowerCase()
    const actorNeedle = eventActorFilter.trim().toLowerCase()
    const fromTime = eventFromDraft ? new Date(eventFromDraft).getTime() : null
    const toTime = eventToDraft ? new Date(eventToDraft).getTime() : null
    return projectEvents.filter((event) => {
      if (typeNeedle && !event.event_type.toLowerCase().includes(typeNeedle)) return false
      const actor = `${event.actor_type}:${event.actor_id}`.toLowerCase()
      if (actorNeedle && !actor.includes(actorNeedle)) return false
      const t = new Date(event.created_at).getTime()
      if (fromTime && !Number.isNaN(fromTime) && t < fromTime) return false
      if (toTime && !Number.isNaN(toTime) && t > toTime) return false
      return true
    })
  }, [projectEvents, eventTypeFilter, eventActorFilter, eventFromDraft, eventToDraft])

  const projectFailureStats = useMemo(() => {
    const now = Date.now()
    const dayAgo = now - 24 * 60 * 60 * 1000
    let totalFailures = 0
    let failures24h = 0
    const taskSet = new Set<number>()
    filteredProjectEvents.forEach((event) => {
      if (event.event_type !== 'task.failed') return
      totalFailures += 1
      taskSet.add(event.task_id)
      const t = new Date(event.created_at).getTime()
      if (!Number.isNaN(t) && t >= dayAgo) failures24h += 1
    })
    return { totalFailures, failures24h, affectedTasks: taskSet.size }
  }, [filteredProjectEvents])

  function formatEventTime(iso: string) {
    const date = new Date(iso)
    if (Number.isNaN(date.getTime())) return iso
    return date.toLocaleString()
  }

  function formatPayload(payload: Record<string, unknown>) {
    const entries = Object.entries(payload ?? {})
    if (entries.length === 0) return '-'
    return entries
      .slice(0, 3)
      .map(([k, v]) => `${k}: ${String(v)}`)
      .join(' | ')
  }

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
      setTaskEvents([])
      setTaskEventsError('')
      setProjectEvents([])
      setProjectEventsError('')
      setRuntimeAlerts(null)
      setRuntimeAlertsError('')
      setHeartbeatAlertThresholdDraft('300')
      setMaxAttemptsDraft('1')
      setGitRepoDraft('')
      setGitBranchDraft('')
      setGitBaseCommitDraft('')
      setGitCommitDraft('')
      setRequiredCapabilitiesDraft('')
      setClaimTokenDraft('')
      setAgentCapabilitiesDraft('')
      setFailReasonDraft('')
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
      setRequiredCapabilitiesDraft('')
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

  useEffect(() => {
    if (!selectedTaskID) {
      setRequiredCapabilitiesDraft('')
      return
    }
    let cancelled = false
    const run = async () => {
      try {
        const data = await fetchTaskCapabilities(selectedTaskID)
        if (cancelled) return
        setRequiredCapabilitiesDraft(data.required_capabilities.join(', '))
      } catch {
        if (!cancelled) {
          setRequiredCapabilitiesDraft('')
        }
      }
    }
    void run()
    return () => {
      cancelled = true
    }
  }, [selectedTaskID])

  useEffect(() => {
    if (!selectedProjectID) {
      setProjectEvents([])
      setProjectEventsError('')
      setIsLoadingProjectEvents(false)
      setRuntimeAlerts(null)
      setRuntimeAlertsError('')
      setIsLoadingRuntimeAlerts(false)
      return
    }
    let cancelled = false
    const run = async () => {
      setIsLoadingProjectEvents(true)
      setProjectEventsError('')
      try {
        const data = await fetchProjectEvents(selectedProjectID)
        if (cancelled) return
        setProjectEvents(data.events)
      } catch (err) {
        if (cancelled) return
        const message = err instanceof Error ? err.message : 'failed to load project events'
        setProjectEventsError(message)
        setProjectEvents([])
      } finally {
        if (!cancelled) {
          setIsLoadingProjectEvents(false)
        }
      }
    }
    void run()
    return () => {
      cancelled = true
    }
  }, [selectedProjectID])

  useEffect(() => {
    if (projects.length === 0) {
      setProjectAlertBadges({})
      return
    }
    let cancelled = false
    const run = async () => {
      const ids = projects.map((p) => p.id)
      setProjectAlertBadges((prev) => {
        const next: Record<number, ProjectAlertBadge> = {}
        ids.forEach((id) => {
          next[id] =
            prev[id] ??
            { stale: 0, overdue: 0, orphan: 0, exhausted: 0, score: 0, loading: true, error: false }
          next[id] = { ...next[id], loading: true, error: false }
        })
        return next
      })

      const settled = await Promise.all(
        projects.map(async (project) => {
          try {
            const data = await fetchRuntimeAlerts(project.id, 300)
            return {
              id: project.id,
              badge: buildProjectAlertBadge(data)
            }
          } catch {
            return {
              id: project.id,
              badge: { stale: 0, overdue: 0, orphan: 0, exhausted: 0, score: 0, loading: false, error: true }
            }
          }
        })
      )

      if (cancelled) return
      setProjectAlertBadges((prev) => {
        const next = { ...prev }
        settled.forEach((item) => {
          next[item.id] = item.badge
        })
        return next
      })
    }
    void run()
    const timer = window.setInterval(() => {
      void run()
    }, 30000)
    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [projects])

  useEffect(() => {
    if (!selectedProjectID) return
    let cancelled = false
    const run = async () => {
      setIsLoadingRuntimeAlerts(true)
      setRuntimeAlertsError('')
      try {
        const threshold = Number(heartbeatAlertThresholdDraft)
        const data = await fetchRuntimeAlerts(
          selectedProjectID,
          Number.isInteger(threshold) && threshold > 0 ? threshold : undefined
        )
        if (cancelled) return
        setRuntimeAlerts(data)
        setProjectAlertBadges((prev) => ({
          ...prev,
          [selectedProjectID]: buildProjectAlertBadge(data)
        }))
      } catch (err) {
        if (cancelled) return
        const message = err instanceof Error ? err.message : 'failed to load runtime alerts'
        setRuntimeAlertsError(message)
        setRuntimeAlerts(null)
        setProjectAlertBadges((prev) => ({
          ...prev,
          [selectedProjectID]: {
            stale: 0,
            overdue: 0,
            orphan: 0,
            exhausted: 0,
            score: 0,
            loading: false,
            error: true
          }
        }))
      } finally {
        if (!cancelled) {
          setIsLoadingRuntimeAlerts(false)
        }
      }
    }
    void run()
    return () => {
      cancelled = true
    }
  }, [selectedProjectID])

  useEffect(() => {
    if (!selectedTaskID) {
      setTaskEvents([])
      setTaskEventsError('')
      setIsLoadingTaskEvents(false)
      return
    }
    let cancelled = false
    const run = async () => {
      setIsLoadingTaskEvents(true)
      setTaskEventsError('')
      try {
        const data = await fetchTaskEvents(selectedTaskID)
        if (cancelled) return
        setTaskEvents(data.events)
      } catch (err) {
        if (cancelled) return
        const message = err instanceof Error ? err.message : 'failed to load task events'
        setTaskEventsError(message)
        setTaskEvents([])
      } finally {
        if (!cancelled) {
          setIsLoadingTaskEvents(false)
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
    if (data.length === 0) {
      setSelectedProjectID(null)
      setSelectedTaskID(null)
      setTree([])
      return
    }
    if (!selectedProjectID || !data.some((project) => project.id === selectedProjectID)) {
      setSelectedProjectID(data[0].id)
      setSelectedTaskID(null)
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
    const raw = (await res.json()) as {
      recent_runs?: TaskRunSummary[] | null
      git_refs?: TaskGitRefSummary[] | null
    }
    return {
      recent_runs: Array.isArray(raw.recent_runs) ? raw.recent_runs : [],
      git_refs: Array.isArray(raw.git_refs) ? raw.git_refs : []
    }
  }

  async function fetchTaskEvents(taskID: number) {
    const res = await fetch(`${apiBase}/tasks/${taskID}/events?limit=30`)
    if (!res.ok) {
      const message = await res.text()
      throw new Error(message || 'failed to fetch task events')
    }
    const raw = (await res.json()) as TaskEventsResponse
    return {
      task_id: raw.task_id,
      project_id: raw.project_id,
      events: Array.isArray(raw.events) ? raw.events : []
    }
  }

  async function fetchProjectEvents(projectID: number) {
    const res = await fetch(`${apiBase}/projects/${projectID}/events?limit=30`)
    if (!res.ok) {
      const message = await res.text()
      throw new Error(message || 'failed to fetch project events')
    }
    const raw = (await res.json()) as ProjectEventsResponse
    return {
      project_id: raw.project_id,
      events: Array.isArray(raw.events) ? raw.events : []
    }
  }

  async function fetchRuntimeAlerts(projectID: number, heartbeatStaleSeconds?: number) {
    const q =
      heartbeatStaleSeconds && heartbeatStaleSeconds > 0
        ? `?heartbeat_stale_seconds=${heartbeatStaleSeconds}`
        : ''
    const res = await fetch(`${apiBase}/projects/${projectID}/runtime-alerts${q}`)
    if (!res.ok) {
      const message = await res.text()
      throw new Error(message || 'failed to fetch runtime alerts')
    }
    const raw = (await res.json()) as RuntimeAlertsResponse
    return {
      project_id: raw.project_id,
      heartbeat_stale_seconds: raw.heartbeat_stale_seconds,
      total_tasks: Number(raw.total_tasks) || 0,
      done_tasks: Number(raw.done_tasks) || 0,
      unfinished_tasks: Number(raw.unfinished_tasks) || 0,
      claimable_tasks: Number(raw.claimable_tasks) || 0,
      blocked_planned_tasks: Number(raw.blocked_planned_tasks) || 0,
      stale_active_claims: Array.isArray(raw.stale_active_claims) ? raw.stale_active_claims : [],
      heartbeat_overdue_claims: Array.isArray(raw.heartbeat_overdue_claims) ? raw.heartbeat_overdue_claims : [],
      orphan_in_progress_tasks: Array.isArray(raw.orphan_in_progress_tasks) ? raw.orphan_in_progress_tasks : [],
      exhausted_tasks: Array.isArray(raw.exhausted_tasks) ? raw.exhausted_tasks : []
    }
  }

  async function refreshRuntimeAlerts() {
    if (!selectedProjectID) return
    const threshold = Number(heartbeatAlertThresholdDraft)
    setIsLoadingRuntimeAlerts(true)
    setRuntimeAlertsError('')
    try {
      const data = await fetchRuntimeAlerts(
        selectedProjectID,
        Number.isInteger(threshold) && threshold > 0 ? threshold : undefined
      )
      setRuntimeAlerts(data)
      setProjectAlertBadges((prev) => ({
        ...prev,
        [selectedProjectID]: buildProjectAlertBadge(data)
      }))
    } catch (err) {
      const message = err instanceof Error ? err.message : 'failed to refresh runtime alerts'
      setRuntimeAlertsError(message)
    } finally {
      setIsLoadingRuntimeAlerts(false)
    }
  }

  async function fetchTaskCapabilities(taskID: number) {
    const res = await fetch(`${apiBase}/tasks/${taskID}/capabilities`)
    if (!res.ok) {
      const message = await res.text()
      throw new Error(message || 'failed to fetch task capabilities')
    }
    const raw = (await res.json()) as TaskCapabilitiesResponse
    return {
      task_id: raw.task_id,
      required_capabilities: Array.isArray(raw.required_capabilities) ? raw.required_capabilities : []
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
    const [taskEventData, projectEventData] = await Promise.all([
      fetchTaskEvents(taskID),
      selectedProjectID ? fetchProjectEvents(selectedProjectID) : Promise.resolve({ project_id: 0, events: [] })
    ])
    setTaskEvents(taskEventData.events)
    if (selectedProjectID) {
      setProjectEvents(projectEventData.events)
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
    const [ctx, taskEventData, projectEventData] = await Promise.all([
      fetchTaskContext(taskID),
      fetchTaskEvents(taskID),
      selectedProjectID ? fetchProjectEvents(selectedProjectID) : Promise.resolve({ project_id: 0, events: [] })
    ])
    setTaskContext(ctx)
    setTaskEvents(taskEventData.events)
    if (selectedProjectID) {
      setProjectEvents(projectEventData.events)
    }
  }

  async function patchTaskCapabilities(taskID: number, requiredCapabilities: string[]) {
    const res = await fetch(`${apiBase}/tasks/${taskID}/capabilities`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ required_capabilities: requiredCapabilities })
    })
    if (!res.ok) {
      const message = await res.text()
      throw new Error(message || 'failed to update task capabilities')
    }
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
    const [ctx, taskEventData, projectEventData] = await Promise.all([
      fetchTaskContext(taskID),
      fetchTaskEvents(taskID),
      selectedProjectID ? fetchProjectEvents(selectedProjectID) : Promise.resolve({ project_id: 0, events: [] })
    ])
    setTaskContext(ctx)
    setTaskEvents(taskEventData.events)
    if (selectedProjectID) {
      setProjectEvents(projectEventData.events)
    }
  }

  async function refreshObservability(taskID: number, projectID: number | null) {
    const [ctx, taskEventData, projectEventData] = await Promise.all([
      fetchTaskContext(taskID),
      fetchTaskEvents(taskID),
      projectID ? fetchProjectEvents(projectID) : Promise.resolve({ project_id: 0, events: [] })
    ])
    setTaskContext(ctx)
    setTaskEvents(taskEventData.events)
    if (projectID) {
      setProjectEvents(projectEventData.events)
      const threshold = Number(heartbeatAlertThresholdDraft)
      const runtime = await fetchRuntimeAlerts(
        projectID,
        Number.isInteger(threshold) && threshold > 0 ? threshold : undefined
      )
      setRuntimeAlerts(runtime)
      setProjectAlertBadges((prev) => ({
        ...prev,
        [projectID]: buildProjectAlertBadge(runtime)
      }))
    }
  }

  function buildDefaultResultPayload() {
    const now = new Date().toISOString()
    return {
      summary: `Updated via TasQ UI at ${now}`,
      changes: ['See task result markdown for details.'],
      paths: ['N/A'],
      commands: ['N/A'],
      tests: ['N/A'],
      artifacts: ['N/A'],
      next_risks: ['N/A']
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

  function openDeleteProjectModal(project: Project) {
    setDeleteProjectModal(project)
  }

  function closeDeleteProjectModal() {
    if (isDeletingProject) return
    setDeleteProjectModal(null)
  }

  async function submitDeleteProject() {
    if (!deleteProjectModal) return
    setIsDeletingProject(true)
    try {
      const res = await fetch(`${apiBase}/projects/${deleteProjectModal.id}`, {
        method: 'DELETE'
      })
      if (!res.ok) {
        const message = await res.text()
        throw new Error(message || 'failed to delete project')
      }

      if (selectedProjectID === deleteProjectModal.id) {
        setSelectedProjectID(null)
        setSelectedTaskID(null)
        setTree([])
      }
      closeDeleteProjectModal()
      await fetchProjects()
    } catch (err) {
      const message = err instanceof Error ? err.message : 'failed to delete project'
      setTaskMessage(message)
    } finally {
      setIsDeletingProject(false)
    }
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

  function parseCapabilityInput(input: string) {
    return Array.from(
      new Set(
        input
          .split(',')
          .map((item) => item.trim().toLowerCase())
          .filter((item) => item.length > 0)
      )
    )
  }

  async function saveTaskCapabilities() {
    if (!selectedTask) return
    const required = parseCapabilityInput(requiredCapabilitiesDraft)

    setTaskMessage('')
    setIsSavingCapabilities(true)
    try {
      await patchTaskCapabilities(selectedTask.id, required)
      setRequiredCapabilitiesDraft(required.join(', '))
      if (selectedProjectID) {
        const projectEventData = await fetchProjectEvents(selectedProjectID)
        setProjectEvents(projectEventData.events)
      }
      const taskEventData = await fetchTaskEvents(selectedTask.id)
      setTaskEvents(taskEventData.events)
      setTaskMessage('Task capabilities saved.')
    } catch (err) {
      const message = err instanceof Error ? err.message : 'failed to save task capabilities'
      setTaskMessage(message)
    } finally {
      setIsSavingCapabilities(false)
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

  async function claimNextTask() {
    if (!selectedProjectID) {
      setTaskMessage('Select a project first.')
      return
    }
    const agentID = agentIDDraft.trim()
    if (!agentID) {
      setTaskMessage('Agent ID is required.')
      return
    }
    const lease = Number(leaseSecondsDraft)
    const capabilities = parseCapabilityInput(agentCapabilitiesDraft)
    if (!Number.isInteger(lease) || lease <= 0) {
      setTaskMessage('Lease seconds must be a positive integer.')
      return
    }

    setIsAgentActionRunning(true)
    setTaskMessage('')
    try {
      const res = await fetch(`${apiBase}/agents/claim-next`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          project_id: selectedProjectID,
          agent_id: agentID,
          lease_seconds: lease,
          capabilities
        })
      })
      if (!res.ok) {
        const message = await res.text()
        throw new Error(message || 'failed to claim next task')
      }
      const data = (await res.json()) as ClaimNextResponse
      if (!data.task) {
        setTaskMessage(data.message || 'No claimable task.')
        return
      }
      if (data.claim?.token) {
        setClaimTokenDraft(data.claim.token)
      }
      setSelectedTaskID(data.task.id)
      await fetchTaskTree(selectedProjectID)
      await refreshObservability(data.task.id, selectedProjectID)
      setTaskMessage(`Claimed task #${data.task.id}.`)
    } catch (err) {
      const message = err instanceof Error ? err.message : 'failed to claim next task'
      setTaskMessage(message)
    } finally {
      setIsAgentActionRunning(false)
    }
  }

  async function heartbeatClaim() {
    if (!selectedTask) return
    const agentID = agentIDDraft.trim()
    const lease = Number(leaseSecondsDraft)
    if (!agentID || !claimTokenDraft.trim()) {
      setTaskMessage('Agent ID and claim token are required.')
      return
    }
    if (!Number.isInteger(lease) || lease <= 0) {
      setTaskMessage('Lease seconds must be a positive integer.')
      return
    }

    setIsAgentActionRunning(true)
    setTaskMessage('')
    try {
      const res = await fetch(`${apiBase}/tasks/${selectedTask.id}/heartbeat`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ agent_id: agentID, lease_seconds: lease, claim_token: claimTokenDraft.trim() })
      })
      if (!res.ok) {
        const message = await res.text()
        throw new Error(message || 'failed to heartbeat claim')
      }
      const taskEventData = await fetchTaskEvents(selectedTask.id)
      setTaskEvents(taskEventData.events)
      if (selectedProjectID) {
        const projectEventData = await fetchProjectEvents(selectedProjectID)
        setProjectEvents(projectEventData.events)
      }
      setTaskMessage('Heartbeat sent.')
    } catch (err) {
      const message = err instanceof Error ? err.message : 'failed to heartbeat claim'
      setTaskMessage(message)
    } finally {
      setIsAgentActionRunning(false)
    }
  }

  async function releaseClaim() {
    if (!selectedTask) return
    const agentID = agentIDDraft.trim()
    if (!agentID || !claimTokenDraft.trim()) {
      setTaskMessage('Agent ID and claim token are required.')
      return
    }

    setIsAgentActionRunning(true)
    setTaskMessage('')
    try {
      const res = await fetch(`${apiBase}/tasks/${selectedTask.id}/release`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ agent_id: agentID, claim_token: claimTokenDraft.trim(), to_status: 'planned' })
      })
      if (!res.ok) {
        const message = await res.text()
        throw new Error(message || 'failed to release claim')
      }
      if (selectedProjectID) {
        await fetchTaskTree(selectedProjectID)
      }
      await refreshObservability(selectedTask.id, selectedProjectID)
      setTaskMessage('Claim released to planned.')
    } catch (err) {
      const message = err instanceof Error ? err.message : 'failed to release claim'
      setTaskMessage(message)
    } finally {
      setIsAgentActionRunning(false)
    }
  }

  async function completeClaimedTask() {
    if (!selectedTask) return
    const agentID = agentIDDraft.trim()
    if (!agentID || !claimTokenDraft.trim()) {
      setTaskMessage('Agent ID and claim token are required.')
      return
    }

    setIsAgentActionRunning(true)
    setTaskMessage('')
    try {
      const res = await fetch(`${apiBase}/tasks/${selectedTask.id}/complete`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          agent_id: agentID,
          claim_token: claimTokenDraft.trim(),
          result_md: taskResultDraft,
          result_payload_version: 'v2',
          result_payload: buildDefaultResultPayload()
        })
      })
      if (!res.ok) {
        const message = await res.text()
        throw new Error(message || 'failed to complete task')
      }
      if (selectedProjectID) {
        await fetchTaskTree(selectedProjectID)
      }
      await refreshObservability(selectedTask.id, selectedProjectID)
      setTaskMessage('Task completed by agent action.')
    } catch (err) {
      const message = err instanceof Error ? err.message : 'failed to complete task'
      setTaskMessage(message)
    } finally {
      setIsAgentActionRunning(false)
    }
  }

  async function failClaimedTask() {
    if (!selectedTask) return
    const agentID = agentIDDraft.trim()
    if (!agentID || !claimTokenDraft.trim()) {
      setTaskMessage('Agent ID and claim token are required.')
      return
    }

    setIsAgentActionRunning(true)
    setTaskMessage('')
    try {
      const res = await fetch(`${apiBase}/tasks/${selectedTask.id}/fail`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          agent_id: agentID,
          claim_token: claimTokenDraft.trim(),
          reason: failReasonDraft.trim() || 'manual fail from UI',
          result_md: taskResultDraft,
          result_payload_version: 'v2',
          result_payload: buildDefaultResultPayload()
        })
      })
      if (!res.ok) {
        const message = await res.text()
        throw new Error(message || 'failed to fail task')
      }
      if (selectedProjectID) {
        await fetchTaskTree(selectedProjectID)
      }
      await refreshObservability(selectedTask.id, selectedProjectID)
      setTaskMessage('Task marked as failed by agent action.')
    } catch (err) {
      const message = err instanceof Error ? err.message : 'failed to fail task'
      setTaskMessage(message)
    } finally {
      setIsAgentActionRunning(false)
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
                const badge = projectAlertBadges[project.id]
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
                      <div className="flex min-w-0 flex-1 items-center gap-2">
                        <div className="min-w-0 flex-1">
                          <p className="truncate text-left text-sm">{project.name}</p>
                          <p className="truncate text-left text-[10px] uppercase tracking-[0.22em] text-muted-foreground/80">
                            Project #{project.id}
                          </p>
                        </div>
                        {badge?.loading && <span className="h-2 w-2 rounded-full bg-slate-400" />}
                        {!badge?.loading && badge?.error && <span className="h-2 w-2 rounded-full bg-rose-400" />}
                        {!badge?.loading && !badge?.error && badge && badge.score > 0 && (
                          <span className="rounded-full border border-rose-400/50 bg-rose-400/20 px-2 py-0.5 text-[10px] font-semibold text-rose-200">
                            {badge.score}
                          </span>
                        )}
                      </div>
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
                      <div className="flex items-center gap-1 opacity-0 transition-opacity group-hover:opacity-100">
                        <Button
                          size="sm"
                          variant="ghost"
                          className="h-7 w-7 p-0"
                          onClick={(e) => {
                            e.stopPropagation()
                            startProjectEdit(project)
                          }}
                        >
                          <Pencil className="h-3.5 w-3.5" />
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          className="h-7 w-7 p-0 text-rose-200 hover:text-rose-100"
                          onClick={(e) => {
                            e.stopPropagation()
                            openDeleteProjectModal(project)
                          }}
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      </div>
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
          <div className="mt-1 flex items-center gap-2 text-xs text-muted-foreground">
            <span>{selectedProject ? `Project: ${selectedProject.name}` : 'Select a project'}</span>
            {selectedProject && (
              <span className="rounded-full border border-border/70 bg-muted/30 px-2 py-0.5 text-[10px] uppercase tracking-[0.18em] text-muted-foreground">
                ID {selectedProject.id}
              </span>
            )}
          </div>
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

                <div className="mt-3">
                  <p className="mb-1 text-[11px] text-muted-foreground">Required capabilities (comma separated)</p>
                  <div className="flex items-center gap-2">
                    <Input
                      placeholder="go, backend, tests"
                      value={requiredCapabilitiesDraft}
                      onChange={(e) => setRequiredCapabilitiesDraft(e.target.value)}
                    />
                    <Button
                      size="sm"
                      variant="outline"
                      className="shrink-0 whitespace-nowrap"
                      disabled={isSavingCapabilities}
                      onClick={() => void saveTaskCapabilities()}
                    >
                      {isSavingCapabilities ? 'Saving...' : 'Save capabilities'}
                    </Button>
                  </div>
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
                    <div className="rounded-md border bg-background/40 p-2">
                      <p className="text-[11px] uppercase tracking-wide text-muted-foreground">Observability Filters</p>
                      <div className="mt-2 grid grid-cols-2 gap-2">
                        <Input
                          placeholder="event type (e.g. task.failed)"
                          value={eventTypeFilter}
                          onChange={(e) => setEventTypeFilter(e.target.value)}
                        />
                        <Input
                          placeholder="actor (type:id)"
                          value={eventActorFilter}
                          onChange={(e) => setEventActorFilter(e.target.value)}
                        />
                        <Input
                          type="datetime-local"
                          value={eventFromDraft}
                          onChange={(e) => setEventFromDraft(e.target.value)}
                        />
                        <Input
                          type="datetime-local"
                          value={eventToDraft}
                          onChange={(e) => setEventToDraft(e.target.value)}
                        />
                        <Input
                          placeholder="run agent filter"
                          value={runAgentFilter}
                          onChange={(e) => setRunAgentFilter(e.target.value)}
                        />
                        <select
                          className="h-10 rounded-md border border-border bg-background px-2 text-sm"
                          value={runStatusFilter}
                          onChange={(e) =>
                            setRunStatusFilter(
                              e.target.value as 'all' | 'running' | 'completed' | 'failed' | 'released'
                            )
                          }
                        >
                          <option value="all">All run statuses</option>
                          <option value="running">running</option>
                          <option value="completed">completed</option>
                          <option value="failed">failed</option>
                          <option value="released">released</option>
                        </select>
                      </div>
                    </div>

                    <div className="rounded-md border bg-background/40 p-2">
                      <div className="flex items-center justify-between gap-2">
                        <p className="text-[11px] uppercase tracking-wide text-muted-foreground">Runtime Alerts</p>
                        <div className="flex items-center gap-2">
                          <Input
                            className="h-8 w-28"
                            type="number"
                            min={1}
                            placeholder="stale sec"
                            value={heartbeatAlertThresholdDraft}
                            onChange={(e) => setHeartbeatAlertThresholdDraft(e.target.value)}
                          />
                          <Button size="sm" variant="outline" onClick={() => void refreshRuntimeAlerts()} disabled={isLoadingRuntimeAlerts}>
                            {isLoadingRuntimeAlerts ? 'Refreshing...' : 'Refresh'}
                          </Button>
                        </div>
                      </div>
                      {runtimeAlertsError && <p className="mt-2 text-xs text-red-300">{runtimeAlertsError}</p>}
                      {!runtimeAlertsError && !runtimeAlerts && isLoadingRuntimeAlerts && (
                        <p className="mt-2 text-xs text-muted-foreground">Loading runtime alerts...</p>
                      )}
                      {runtimeAlerts && (
                        <div className="mt-2 space-y-2">
                          <div className="grid grid-cols-2 gap-2 text-xs xl:grid-cols-4">
                            <div className="rounded border border-border/60 bg-background/60 p-1.5">
                              <p className="text-muted-foreground">Queue</p>
                              <p className="font-semibold">
                                {runtimeAlerts.done_tasks}/{runtimeAlerts.total_tasks} done
                              </p>
                              <p className="text-[11px] text-muted-foreground">
                                {runtimeAlerts.claimable_tasks} claimable · {runtimeAlerts.blocked_planned_tasks} blocked
                              </p>
                            </div>
                            <div className="rounded border border-border/60 bg-background/60 p-1.5">
                              <p className="text-muted-foreground">Stale Claims</p>
                              <p className="font-semibold">{runtimeAlerts.stale_active_claims.length}</p>
                            </div>
                            <div className="rounded border border-border/60 bg-background/60 p-1.5">
                              <p className="text-muted-foreground">Heartbeat Overdue</p>
                              <p className="font-semibold">{runtimeAlerts.heartbeat_overdue_claims.length}</p>
                            </div>
                            <div className="rounded border border-border/60 bg-background/60 p-1.5">
                              <p className="text-muted-foreground">Orphan In Progress</p>
                              <p className="font-semibold">{runtimeAlerts.orphan_in_progress_tasks.length}</p>
                            </div>
                            <div className="rounded border border-border/60 bg-background/60 p-1.5">
                              <p className="text-muted-foreground">Exhausted Tasks</p>
                              <p className="font-semibold">{runtimeAlerts.exhausted_tasks.length}</p>
                            </div>
                          </div>

                          <div className="grid grid-cols-1 gap-2 xl:grid-cols-4">
                            <div className="rounded border border-border/60 bg-background/60 p-1.5 text-xs">
                              <p className="mb-1 font-medium text-foreground/90">Stale Claims</p>
                              <div className="max-h-24 space-y-1 overflow-y-auto">
                                {runtimeAlerts.stale_active_claims.length === 0 && (
                                  <p className="text-muted-foreground">None</p>
                                )}
                                {runtimeAlerts.stale_active_claims.map((item) => (
                                  <p key={item.claim_id} className="text-muted-foreground">
                                    C#{item.claim_id} T#{item.task_id} {item.agent_id}
                                  </p>
                                ))}
                              </div>
                            </div>
                            <div className="rounded border border-border/60 bg-background/60 p-1.5 text-xs">
                              <p className="mb-1 font-medium text-foreground/90">Heartbeat Overdue</p>
                              <div className="max-h-24 space-y-1 overflow-y-auto">
                                {runtimeAlerts.heartbeat_overdue_claims.length === 0 && (
                                  <p className="text-muted-foreground">None</p>
                                )}
                                {runtimeAlerts.heartbeat_overdue_claims.map((item) => (
                                  <p key={item.claim_id} className="text-muted-foreground">
                                    C#{item.claim_id} T#{item.task_id} {item.seconds_since_heartbeat}s
                                  </p>
                                ))}
                              </div>
                            </div>
                            <div className="rounded border border-border/60 bg-background/60 p-1.5 text-xs">
                              <p className="mb-1 font-medium text-foreground/90">Orphan In Progress</p>
                              <div className="max-h-24 space-y-1 overflow-y-auto">
                                {runtimeAlerts.orphan_in_progress_tasks.length === 0 && (
                                  <p className="text-muted-foreground">None</p>
                                )}
                                {runtimeAlerts.orphan_in_progress_tasks.map((item) => (
                                  <p key={item.task_id} className="text-muted-foreground">
                                    T#{item.task_id}
                                  </p>
                                ))}
                              </div>
                            </div>
                            <div className="rounded border border-border/60 bg-background/60 p-1.5 text-xs">
                              <p className="mb-1 font-medium text-foreground/90">Exhausted Tasks</p>
                              <div className="max-h-24 space-y-1 overflow-y-auto">
                                {runtimeAlerts.exhausted_tasks.length === 0 && (
                                  <p className="text-muted-foreground">None</p>
                                )}
                                {runtimeAlerts.exhausted_tasks.map((item) => (
                                  <p key={item.task_id} className="text-muted-foreground">
                                    T#{item.task_id} {item.title} ({item.attempts_used}/{item.max_attempts})
                                  </p>
                                ))}
                              </div>
                            </div>
                          </div>
                        </div>
                      )}
                    </div>

                    <div>
                      <p className="text-[11px] uppercase tracking-wide text-muted-foreground">Recent Runs</p>
                      {filteredRuns.length === 0 && (
                        <p className="mt-1 text-xs text-muted-foreground">No run history yet.</p>
                      )}
                      {filteredRuns.length > 0 && (
                        <div className="mt-1 max-h-32 space-y-1 overflow-y-auto rounded-md border bg-background/50 p-2">
                          {filteredRuns.map((run) => (
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

                    <div className="grid grid-cols-1 gap-3 xl:grid-cols-2">
                      <div>
                        <p className="text-[11px] uppercase tracking-wide text-muted-foreground">Task Events</p>
                        {isLoadingTaskEvents && <p className="mt-1 text-xs text-muted-foreground">Loading task events...</p>}
                        {taskEventsError && <p className="mt-1 text-xs text-red-300">{taskEventsError}</p>}
                        {!isLoadingTaskEvents && !taskEventsError && filteredTaskEvents.length === 0 && (
                          <p className="mt-1 text-xs text-muted-foreground">No task events yet.</p>
                        )}
                        {filteredTaskEvents.length > 0 && (
                          <div className="mt-1 max-h-40 space-y-1 overflow-y-auto rounded-md border bg-background/50 p-2">
                            {filteredTaskEvents.map((event) => (
                              <div key={event.id} className="rounded border border-border/60 bg-background/60 p-1.5 text-xs">
                                <p className="font-medium text-foreground/90">{event.event_type}</p>
                                <p className="text-muted-foreground">
                                  {event.actor_type}:{event.actor_id} · {formatEventTime(event.created_at)}
                                </p>
                                <p className="truncate text-muted-foreground">{formatPayload(event.payload ?? {})}</p>
                              </div>
                            ))}
                          </div>
                        )}
                      </div>

                      <div>
                        <p className="text-[11px] uppercase tracking-wide text-muted-foreground">Project Events</p>
                        {isLoadingProjectEvents && (
                          <p className="mt-1 text-xs text-muted-foreground">Loading project events...</p>
                        )}
                        {projectEventsError && <p className="mt-1 text-xs text-red-300">{projectEventsError}</p>}
                        {!isLoadingProjectEvents && !projectEventsError && filteredProjectEvents.length === 0 && (
                          <p className="mt-1 text-xs text-muted-foreground">No project events yet.</p>
                        )}
                        <div className="mt-1 grid grid-cols-3 gap-2 text-xs">
                          <div className="rounded border border-border/60 bg-background/60 p-1.5">
                            <p className="text-muted-foreground">Failures</p>
                            <p className="font-semibold">{projectFailureStats.totalFailures}</p>
                          </div>
                          <div className="rounded border border-border/60 bg-background/60 p-1.5">
                            <p className="text-muted-foreground">Failures 24h</p>
                            <p className="font-semibold">{projectFailureStats.failures24h}</p>
                          </div>
                          <div className="rounded border border-border/60 bg-background/60 p-1.5">
                            <p className="text-muted-foreground">Affected Tasks</p>
                            <p className="font-semibold">{projectFailureStats.affectedTasks}</p>
                          </div>
                        </div>
                        {filteredProjectEvents.length > 0 && (
                          <div className="mt-1 max-h-40 space-y-1 overflow-y-auto rounded-md border bg-background/50 p-2">
                            {filteredProjectEvents.map((event) => (
                              <div key={event.id} className="rounded border border-border/60 bg-background/60 p-1.5 text-xs">
                                <p className="font-medium text-foreground/90">
                                  T#{event.task_id} · {event.event_type}
                                </p>
                                <p className="text-muted-foreground">
                                  {event.actor_type}:{event.actor_id} · {formatEventTime(event.created_at)}
                                </p>
                                <p className="truncate text-muted-foreground">{formatPayload(event.payload ?? {})}</p>
                              </div>
                            ))}
                          </div>
                        )}
                      </div>
                    </div>
                  </div>
                )}

                <div className="mt-3 rounded-md border bg-background/40 p-2">
                  <p className="text-[11px] uppercase tracking-wide text-muted-foreground">Agent Test Controls</p>
                  <div className="mt-2 grid grid-cols-2 gap-2">
                    <Input
                      placeholder="agent id"
                      value={agentIDDraft}
                      onChange={(e) => setAgentIDDraft(e.target.value)}
                      disabled={isAgentActionRunning}
                    />
                    <Input
                      type="number"
                      min={1}
                      placeholder="lease seconds"
                      value={leaseSecondsDraft}
                      onChange={(e) => setLeaseSecondsDraft(e.target.value)}
                      disabled={isAgentActionRunning}
                    />
                    <Input
                      className="col-span-2"
                      placeholder="agent capabilities (comma separated)"
                      value={agentCapabilitiesDraft}
                      onChange={(e) => setAgentCapabilitiesDraft(e.target.value)}
                      disabled={isAgentActionRunning}
                    />
                    <Input
                      className="col-span-2"
                      placeholder="claim token"
                      value={claimTokenDraft}
                      onChange={(e) => setClaimTokenDraft(e.target.value)}
                      disabled={isAgentActionRunning}
                    />
                    <Input
                      className="col-span-2"
                      placeholder="fail reason (optional)"
                      value={failReasonDraft}
                      onChange={(e) => setFailReasonDraft(e.target.value)}
                      disabled={isAgentActionRunning}
                    />
                  </div>
                  <div className="mt-2 flex flex-wrap gap-2">
                    <Button size="sm" variant="outline" disabled={isAgentActionRunning} onClick={() => void claimNextTask()}>
                      Claim Next
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={isAgentActionRunning || !selectedTask}
                      onClick={() => void heartbeatClaim()}
                    >
                      Heartbeat
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={isAgentActionRunning || !selectedTask}
                      onClick={() => void releaseClaim()}
                    >
                      Release
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={isAgentActionRunning || !selectedTask}
                      onClick={() => void completeClaimedTask()}
                    >
                      Complete
                    </Button>
                    <Button
                      size="sm"
                      variant="ghost"
                      className="text-red-300 hover:text-red-200"
                      disabled={isAgentActionRunning || !selectedTask}
                      onClick={() => void failClaimedTask()}
                    >
                      Fail
                    </Button>
                  </div>
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

      {deleteProjectModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
          <div className="w-full max-w-md rounded-lg border border-border bg-card p-4 shadow-2xl">
            <p className="text-sm font-semibold">Delete Project</p>
            <p className="mt-2 text-sm text-muted-foreground">
              This will permanently delete the project and all tasks, dependencies, runs, events, and git links under it.
            </p>
            <p className="mt-2 text-xs text-muted-foreground">
              Target: <span className="text-foreground">{deleteProjectModal.name}</span>
            </p>
            <div className="mt-4 flex justify-end gap-2">
              <Button type="button" variant="ghost" onClick={closeDeleteProjectModal} disabled={isDeletingProject}>
                Cancel
              </Button>
              <Button type="button" variant="outline" onClick={() => void submitDeleteProject()} disabled={isDeletingProject}>
                {isDeletingProject ? 'Deleting...' : 'Delete'}
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
