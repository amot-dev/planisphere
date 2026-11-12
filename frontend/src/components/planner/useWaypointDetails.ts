import { useEffect, useRef, useState } from 'react'
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

/**
 * useWaypointDetails manages expandable waypoint telemetry drawers and refreshes
 * open visited drawers whenever the underlying expedition plan updates.
 */
export function useWaypointDetails(
  plan: expedition.PlanView | null,
  isPlanning: boolean,
): WaypointDetailsState {
  const [expandedRowIds, setExpandedRowIds] = useState<Record<number, boolean>>({})
  const expandedRowIdsRef = useRef<Record<number, boolean>>(expandedRowIds)
  expandedRowIdsRef.current = expandedRowIds
  useEffect(() => {
    expandedRowIdsRef.current = expandedRowIds
  }, [expandedRowIds])

  const [detailsById, setDetailsById] = useState<Record<number, expedition.WaypointDetails>>({})
  const detailsByIdRef = useRef<Record<number, expedition.WaypointDetails>>(detailsById)
  detailsByIdRef.current = detailsById
  useEffect(() => {
    detailsByIdRef.current = detailsById
  }, [detailsById])

  const [loadingDetails, setLoadingDetails] = useState<Record<number, boolean>>({})

  function loadDetails(rowId: number) {
    setLoadingDetails((previous) => ({ ...previous, [rowId]: true }))
    GetWaypointDetails(rowId)
      .then((details) => setDetailsById((previous) => ({ ...previous, [rowId]: details })))
      .catch(console.error)
      .finally(() => setLoadingDetails((previous) => ({ ...previous, [rowId]: false })))
  }

  // Refresh open drawers when plan updates
  useEffect(() => {
    if (!plan || isPlanning) return
    const currentExpandedRowIds = expandedRowIdsRef.current
    for (const idStr of Object.keys(currentExpandedRowIds)) {
      const id = Number(idStr)
      if (currentExpandedRowIds[id]) {
        const rowView = plan.rows?.find((r) => r.row.id === id)
        const isVisited =
          rowView && (rowView.visitState === 'visited' || rowView.visitState === 'current')
        if (isVisited) {
          const loadedDetails = detailsByIdRef.current[id]
          if (loadedDetails && loadedDetails.to === (rowView.row.visitedAt ?? '')) {
            continue
          }
          loadDetails(id)
        }
      }
    }
  }, [plan, isPlanning])

  function toggleExpand(rowId: number) {
    const rowView = plan?.rows?.find((r) => r.row.id === rowId)
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

