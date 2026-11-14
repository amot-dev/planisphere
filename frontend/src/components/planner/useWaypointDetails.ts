import { useCallback, useEffect, useRef, useState } from 'react'
import { GetWaypointDetails } from '../../../wailsjs/go/app/App'
import { expedition } from '../../../wailsjs/go/models'

/**
 * WaypointDetailsState exposes the expanded drawer map, cached waypoint
 * telemetry payloads, loading flags, and expand toggle handler.
 */
export interface WaypointDetailsState {
  expandedRowIds: Record<number, boolean>
  detailsById: Record<number, expedition.WaypointDetails>
  loadingDetails: Record<number, boolean>
  toggleExpand: (rowId: number) => void
}

function computeLegStartTimestamp(plan: expedition.PlanView, targetVisitedAt: string): string {
  let afterTimestamp = ''
  for (const rowView of plan.rows ?? []) {
    const candidateVisitedAt = rowView.row.visitedAt ?? ''
    if (
      rowView.row.rowType === 'waypoint' &&
      candidateVisitedAt !== '' &&
      candidateVisitedAt < targetVisitedAt &&
      candidateVisitedAt > afterTimestamp
    ) {
      afterTimestamp = candidateVisitedAt
    }
  }
  return afterTimestamp || (plan.expedition?.startTimestamp ?? '')
}

/**
 * useWaypointDetails manages expandable waypoint telemetry drawers, resets state
 * across expedition switches, and refreshes open drawers when leg bounds or
 * journal events change.
 */
export function useWaypointDetails(
  plan: expedition.PlanView | null,
  isPlanning: boolean,
  archiveRevision = '',
): WaypointDetailsState {
  const [expandedRowIds, setExpandedRowIds] = useState<Record<number, boolean>>({})
  const expandedRowIdsRef = useRef<Record<number, boolean>>(expandedRowIds)
  expandedRowIdsRef.current = expandedRowIds

  const [detailsById, setDetailsById] = useState<Record<number, expedition.WaypointDetails>>({})
  const detailsByIdRef = useRef<Record<number, expedition.WaypointDetails>>(detailsById)
  detailsByIdRef.current = detailsById

  const [loadingDetails, setLoadingDetails] = useState<Record<number, boolean>>({})

  const activeExpeditionId = plan?.expedition?.id
  const activeExpeditionIdRef = useRef<number | undefined>(activeExpeditionId)
  activeExpeditionIdRef.current = activeExpeditionId

  const archiveRevisionRef = useRef<string>(archiveRevision)
  archiveRevisionRef.current = archiveRevision

  const loadedArchiveRevisionByIdRef = useRef<Record<number, string>>({})
  const requestSeqByRowIdRef = useRef<Record<number, number>>({})

  const loadDetails = useCallback((rowId: number) => {
    const requestExpeditionId = activeExpeditionIdRef.current
    const requestArchiveRevision = archiveRevisionRef.current
    const requestSequence = (requestSeqByRowIdRef.current[rowId] ?? 0) + 1
    requestSeqByRowIdRef.current[rowId] = requestSequence
    setLoadingDetails((previous) => ({ ...previous, [rowId]: true }))
    GetWaypointDetails(rowId)
      .then((details) => {
        if (
          activeExpeditionIdRef.current !== requestExpeditionId ||
          requestSeqByRowIdRef.current[rowId] !== requestSequence
        ) {
          return
        }
        loadedArchiveRevisionByIdRef.current[rowId] = requestArchiveRevision
        setDetailsById((previous) => {
          const nextDetails = { ...previous, [rowId]: details }
          detailsByIdRef.current = nextDetails
          return nextDetails
        })
      })
      .catch((detailsError) => {
        if (
          activeExpeditionIdRef.current === requestExpeditionId &&
          requestSeqByRowIdRef.current[rowId] === requestSequence
        ) {
          delete loadedArchiveRevisionByIdRef.current[rowId]
          console.error('Failed to load waypoint details:', detailsError)
        }
      })
      .finally(() => {
        if (
          activeExpeditionIdRef.current !== requestExpeditionId ||
          requestSeqByRowIdRef.current[rowId] !== requestSequence
        ) {
          return
        }
        setLoadingDetails((previous) => ({ ...previous, [rowId]: false }))
      })
  }, [])

  // Reset open drawers and cached payloads when switching expeditions or entering planning mode.
  useEffect(() => {
    loadedArchiveRevisionByIdRef.current = {}
    requestSeqByRowIdRef.current = {}
    if (
      Object.keys(expandedRowIdsRef.current).length > 0 ||
      Object.keys(detailsByIdRef.current).length > 0
    ) {
      expandedRowIdsRef.current = {}
      detailsByIdRef.current = {}
      setExpandedRowIds({})
      setDetailsById({})
      setLoadingDetails({})
    }
  }, [activeExpeditionId, isPlanning])

  // Refresh open visited drawers when their leg bounds or journal archive revision change.
  useEffect(() => {
    if (!plan || isPlanning) return
    for (const [idString, isOpen] of Object.entries(expandedRowIdsRef.current)) {
      if (!isOpen) continue
      const rowId = Number(idString)
      const rowView = plan.rows?.find((candidate) => candidate.row.id === rowId)
      const isVisited =
        rowView &&
        rowView.row.rowType === 'waypoint' &&
        (rowView.visitState === 'visited' || rowView.visitState === 'current')
      if (!isVisited) continue

      const loadedDetails = detailsByIdRef.current[rowId]
      if (
        loadedDetails &&
        loadedDetails.number === rowView.number &&
        loadedArchiveRevisionByIdRef.current[rowId] === archiveRevision
      ) {
        const expectedTo = rowView.row.visitedAt ?? ''
        const expectedFrom =
          rowView.number === 1 ? loadedDetails.from : computeLegStartTimestamp(plan, expectedTo)
        if (loadedDetails.to === expectedTo && loadedDetails.from === expectedFrom) {
          continue
        }
      }
      loadDetails(rowId)
    }
  }, [archiveRevision, isPlanning, loadDetails, plan])

  function toggleExpand(rowId: number) {
    const rowView = plan?.rows?.find((candidate) => candidate.row.id === rowId)
    const isVisited =
      rowView && (rowView.visitState === 'visited' || rowView.visitState === 'current')
    if (isPlanning || !isVisited) return

    const nextOpen = !expandedRowIdsRef.current[rowId]
    setExpandedRowIds((previous) => {
      const nextExpanded = { ...previous, [rowId]: nextOpen }
      expandedRowIdsRef.current = nextExpanded
      return nextExpanded
    })
    if (nextOpen) {
      loadDetails(rowId)
    }
  }

  return {
    expandedRowIds,
    detailsById,
    loadingDetails,
    toggleExpand,
  }
}
