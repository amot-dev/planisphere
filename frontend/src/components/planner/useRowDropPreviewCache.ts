import { useCallback, useRef } from 'react'
import { GetRowDropPreviews } from '../../api/backend'
import { expedition } from '../../api/models'

/**
 * resolveActiveDragRowIndex returns the current row index matching grabbedRowId, or -1.
 */
export function resolveActiveDragRowIndex(
  rows: readonly (expedition.RowView | null | undefined)[] | null | undefined,
  grabbedRowId: number | null,
): number {
  if (!rows || grabbedRowId === null) return -1
  return rows.findIndex((rowView) => rowView?.row?.id === grabbedRowId)
}

/**
 * useCachedRowDropPreviews caches backend drop-slot validation previews per row
 * for the current PlanView and active Spansh target lock, deduplicating in-flight requests.
 */
export function useCachedRowDropPreviews(
  plan: expedition.PlanView | null,
  isPlanning: boolean,
  isCompleted: boolean,
  lockedSpanshTargetRowId: number,
) {
  const cachedPreviewsByRowIdRef = useRef<Record<number, expedition.DropSlotPreview[]>>({})
  const inflightByRowIdRef = useRef<Record<number, Promise<expedition.DropSlotPreview[] | null>>>({})
  const planRef = useRef(plan)
  const lockedTargetRef = useRef(lockedSpanshTargetRowId)

  if (planRef.current !== plan || lockedTargetRef.current !== lockedSpanshTargetRowId) {
    planRef.current = plan
    lockedTargetRef.current = lockedSpanshTargetRowId
    cachedPreviewsByRowIdRef.current = {}
    inflightByRowIdRef.current = {}
  }

  const getImmediatePreviews = useCallback(
    (row: expedition.Row | undefined): expedition.DropSlotPreview[] | null => {
      if (isPlanning || isCompleted || !row || row.rowType !== 'waypoint') return null
      return cachedPreviewsByRowIdRef.current[row.id] ?? null
    },
    [isCompleted, isPlanning],
  )

  const ensureDropPreviews = useCallback(
    (row: expedition.Row | undefined): Promise<expedition.DropSlotPreview[] | null> => {
      if (isPlanning || isCompleted || !row || row.rowType !== 'waypoint') {
        return Promise.resolve(null)
      }
      const cached = cachedPreviewsByRowIdRef.current[row.id]
      if (cached) return Promise.resolve(cached)
      const existingInflight = inflightByRowIdRef.current[row.id]
      if (existingInflight) return existingInflight

      const requestPlan = planRef.current
      const requestPromise = GetRowDropPreviews(row.id)
        .then((fetched) => {
          if (planRef.current === requestPlan && Array.isArray(fetched)) {
            cachedPreviewsByRowIdRef.current[row.id] = fetched
            return fetched
          }
          return null
        })
        .catch(() => null)
        .finally(() => {
          if (inflightByRowIdRef.current[row.id] === requestPromise) {
            delete inflightByRowIdRef.current[row.id]
          }
        })
      inflightByRowIdRef.current[row.id] = requestPromise
      return requestPromise
    },
    [isCompleted, isPlanning],
  )

  return { getImmediatePreviews, ensureDropPreviews }
}

