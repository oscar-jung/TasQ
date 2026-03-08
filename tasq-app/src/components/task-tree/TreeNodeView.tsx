export type TaskNode = {
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

const NODE_WIDTH = 180
const NODE_GAP = 56
const CONNECTOR_TOP = 30
const CONNECTOR_BOTTOM = 38

function nodeSpanWidth(units: number) {
  return units * (NODE_WIDTH + NODE_GAP) - NODE_GAP
}

function statusLabel(status: TaskNode['status']) {
  if (status === 'in_progress') return 'In Progress'
  if (status === 'done') return 'Done'
  return 'Planned'
}

function statusDotClass(status: TaskNode['status']) {
  if (status === 'in_progress') return 'bg-amber-400'
  if (status === 'done') return 'bg-emerald-400'
  return 'bg-slate-400'
}

export function TreeNodeView({
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
