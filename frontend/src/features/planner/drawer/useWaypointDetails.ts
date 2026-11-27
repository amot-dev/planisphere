import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { GetWaypointDetails } from '../../../api/backend'
import { expedition } from '../../../api/models'

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

function isDrawerEligibleRow(
  rowView: expedition.RowView | undefined,
): rowView is expedition.RowView {
  return Boolean(rowView && rowView.row.rowType === 'waypoint' && rowView.visitState !== 'future')
}

function pruneDrawerRecord<T>(
  record: Record<number, T>,
  rowViewById: Map<number, expedition.RowView>,
  onPruneRowId?: (rowId: number) => void,
): Record<number, T> {
  let nextRecord: Record<number, T> | null = null
  for (const idString of Object.keys(record)) {
    const rowId = Number(idString)
    if (!isDrawerEligibleRow(rowViewById.get(rowId))) {
      nextRecord ??= { ...record }
      delete nextRecord[rowId]
      onPruneRowId?.(rowId)
    }
  }
  return nextRecord ?? record
}

interface DrawerRevisionSnapshot {
  telemetryRevision?: number
  closedWindowRevision?: number
  scopeSignature?: string
}

function formatExpeditionCommanderScopeSignature(plan: expedition.PlanView | null): string {
  return String(plan?.expedition?.commanderProfileId ?? 0)
}

function isLoadedDrawerFresh(
  rowView: expedition.RowView,
  loadedDetails: expedition.WaypointDetails | undefined,
  loadedRevisions: DrawerRevisionSnapshot,
  telemetryRevision: number,
  closedWindowRevision: number,
  commanderScopeSignature: string,
): boolean {
  if (!loadedDetails || !loadedDetails.kind || loadedDetails.number !== rowView.number) {
    return false
  }
  if (loadedRevisions.scopeSignature !== commanderScopeSignature) {
    return false
  }
  const expectedArrivedAt = rowView.row.visitedAt ?? ''
  const expectedTo = rowView.drawerTo ?? (rowView.row.departedAt ?? '')
  const expectedFrom = rowView.number === 1 ? loadedDetails.from : (rowView.drawerFrom ?? '')
  const isRevisionFresh =
    expectedTo !== ''
      ? loadedRevisions.closedWindowRevision === closedWindowRevision
      : loadedRevisions.telemetryRevision === telemetryRevision &&
        loadedRevisions.closedWindowRevision === closedWindowRevision
  return (
    isRevisionFresh &&
    loadedDetails.to === expectedTo &&
    loadedDetails.from === expectedFrom &&
    (loadedDetails.arrivedAt ?? '') === expectedArrivedAt
  )
}

/**
 * useWaypointDetails manages expandable waypoint telemetry drawers, resets state
 * across expedition or database switches, prunes demoted future rows, and selectively
 * refreshes open drawers when leg bounds or relevant journal watermarks change.
 */
export function useWaypointDetails(
  plan: expedition.PlanView | null,
  isPlanning: boolean,
  telemetryRevision = 0,
  isOptimisticPlan = false,
  closedWindowRevision = 0,
  databaseKey = '',
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

  const databaseKeyRef = useRef<string>(databaseKey)
  databaseKeyRef.current = databaseKey

  const telemetryRevisionRef = useRef<number>(telemetryRevision)
  telemetryRevisionRef.current = telemetryRevision

  const closedWindowRevisionRef = useRef<number>(closedWindowRevision)
  closedWindowRevisionRef.current = closedWindowRevision

  const commanderScopeSignature = formatExpeditionCommanderScopeSignature(plan)
  const commanderScopeSignatureRef = useRef<string>(commanderScopeSignature)
  commanderScopeSignatureRef.current = commanderScopeSignature

  const loadedTelemetryRevisionByIdRef = useRef<Record<number, number>>({})
  const loadedClosedRevisionByIdRef = useRef<Record<number, number>>({})
  const loadedScopeSignatureByIdRef = useRef<Record<number, string>>({})
  const requestSeqByRowIdRef = useRef<Record<number, number>>({})

  const rowViewById = useMemo(() => {
    const map = new Map<number, expedition.RowView>()
    for (const rowView of plan?.rows ?? []) {
      if (rowView?.row) {
        map.set(rowView.row.id, rowView)
      }
    }
    return map
  }, [plan?.rows])

  const loadDetails = useCallback((rowId: number) => {
    const requestExpeditionId = activeExpeditionIdRef.current
    const requestDatabaseKey = databaseKeyRef.current
    const requestTelemetryRevision = telemetryRevisionRef.current
    const requestClosedRevision = closedWindowRevisionRef.current
    const requestScopeSignature = commanderScopeSignatureRef.current
    const requestSequence = (requestSeqByRowIdRef.current[rowId] ?? 0) + 1
    requestSeqByRowIdRef.current[rowId] = requestSequence
    const hasLoadedDetails = Boolean(detailsByIdRef.current[rowId]?.kind)
    if (!hasLoadedDetails) {
      setLoadingDetails((previous) => ({ ...previous, [rowId]: true }))
    }
    GetWaypointDetails(rowId)
      .then((details) => {
        if (
          activeExpeditionIdRef.current !== requestExpeditionId ||
          databaseKeyRef.current !== requestDatabaseKey ||
          requestSeqByRowIdRef.current[rowId] !== requestSequence
        ) {
          return
        }
        if (!details || !details.kind) {
          delete loadedTelemetryRevisionByIdRef.current[rowId]
          delete loadedClosedRevisionByIdRef.current[rowId]
          delete loadedScopeSignatureByIdRef.current[rowId]
          return
        }
        loadedTelemetryRevisionByIdRef.current[rowId] = requestTelemetryRevision
        loadedClosedRevisionByIdRef.current[rowId] = requestClosedRevision
        loadedScopeSignatureByIdRef.current[rowId] = requestScopeSignature
        setDetailsById((previous) => {
          const nextDetails = { ...previous, [rowId]: details }
          detailsByIdRef.current = nextDetails
          return nextDetails
        })
      })
      .catch((detailsError) => {
        if (
          activeExpeditionIdRef.current === requestExpeditionId &&
          databaseKeyRef.current === requestDatabaseKey &&
          requestSeqByRowIdRef.current[rowId] === requestSequence
        ) {
          delete loadedTelemetryRevisionByIdRef.current[rowId]
          delete loadedClosedRevisionByIdRef.current[rowId]
          delete loadedScopeSignatureByIdRef.current[rowId]
          console.error('Failed to load waypoint details:', detailsError)
        }
      })
      .finally(() => {
        if (
          activeExpeditionIdRef.current !== requestExpeditionId ||
          databaseKeyRef.current !== requestDatabaseKey ||
          requestSeqByRowIdRef.current[rowId] !== requestSequence
        ) {
          return
        }
        setLoadingDetails((previous) => (previous[rowId] ? { ...previous, [rowId]: false } : previous))
      })
  }, [])

  // Reset open drawers and cached payloads when switching expeditions, switching databases, or entering planning mode.
  useEffect(() => {
    loadedTelemetryRevisionByIdRef.current = {}
    loadedClosedRevisionByIdRef.current = {}
    loadedScopeSignatureByIdRef.current = {}
    requestSeqByRowIdRef.current = {}
    if (
      Object.keys(expandedRowIdsRef.current).length > 0 ||
      Object.keys(detailsByIdRef.current).length > 0
    ) {
      expandedRowIdsRef.current = {}
      detailsByIdRef.current = {}
      setExpandedRowIds({})
      setDetailsById({})
    }
    setLoadingDetails((previous) => (Object.keys(previous).length > 0 ? {} : previous))
  }, [activeExpeditionId, databaseKey, isPlanning])

  // Prune ineligible or future drawers and selectively refresh open drawers on bound or revision changes.
  useEffect(() => {
    if (!plan || isPlanning || isOptimisticPlan) return

    const invalidatePrunedRow = (rowId: number) => {
      delete loadedTelemetryRevisionByIdRef.current[rowId]
      delete loadedClosedRevisionByIdRef.current[rowId]
      delete loadedScopeSignatureByIdRef.current[rowId]
      requestSeqByRowIdRef.current[rowId] = (requestSeqByRowIdRef.current[rowId] ?? 0) + 1
    }
    const nextExpanded = pruneDrawerRecord(
      expandedRowIdsRef.current,
      rowViewById,
      invalidatePrunedRow,
    )
    if (nextExpanded !== expandedRowIdsRef.current) {
      expandedRowIdsRef.current = nextExpanded
      setExpandedRowIds(nextExpanded)
    }
    const nextDetails = pruneDrawerRecord(detailsByIdRef.current, rowViewById, invalidatePrunedRow)
    if (nextDetails !== detailsByIdRef.current) {
      detailsByIdRef.current = nextDetails
      setDetailsById(nextDetails)
    }
    setLoadingDetails((previous) => pruneDrawerRecord(previous, rowViewById))

    for (const [idString, isOpen] of Object.entries(expandedRowIdsRef.current)) {
      if (!isOpen) continue
      const rowId = Number(idString)
      const rowView = rowViewById.get(rowId)
      if (!isDrawerEligibleRow(rowView)) {
        continue
      }
      if (
        isLoadedDrawerFresh(
          rowView,
          detailsByIdRef.current[rowId],
          {
            telemetryRevision: loadedTelemetryRevisionByIdRef.current[rowId],
            closedWindowRevision: loadedClosedRevisionByIdRef.current[rowId],
            scopeSignature: loadedScopeSignatureByIdRef.current[rowId],
          },
          telemetryRevision,
          closedWindowRevision,
          commanderScopeSignature,
        )
      ) {
        continue
      }
      loadDetails(rowId)
    }
  }, [
    closedWindowRevision,
    commanderScopeSignature,
    isOptimisticPlan,
    isPlanning,
    loadDetails,
    plan,
    rowViewById,
    telemetryRevision,
  ])

  const toggleExpand = useCallback(
    (rowId: number) => {
      const rowView = rowViewById.get(rowId)
      if (isPlanning || !isDrawerEligibleRow(rowView)) return

      const nextOpen = !expandedRowIdsRef.current[rowId]
      setExpandedRowIds((previous) => {
        const nextExpanded = { ...previous, [rowId]: nextOpen }
        expandedRowIdsRef.current = nextExpanded
        return nextExpanded
      })
      if (nextOpen) {
        loadDetails(rowId)
      }
    },
    [isPlanning, loadDetails, rowViewById],
  )

  return useMemo(
    () => ({
      expandedRowIds,
      detailsById,
      loadingDetails,
      toggleExpand,
    }),
    [detailsById, expandedRowIds, loadingDetails, toggleExpand],
  )
}

