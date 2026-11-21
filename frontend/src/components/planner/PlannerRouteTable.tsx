import { useMemo, useRef, type CSSProperties, type DragEvent } from 'react'
import { expedition, modes } from '../../../wailsjs/go/models'
import type { NextCarrierStopEntry } from './carrierRouteHelpers'
import { PlannerCollapsedVisitedRow, PlannerDropSlotRow } from './PlannerDragPreviewRows'
import { getWaypointLegBorderColor, type PlannerTableItem } from './plannerPlanHelpers'
import { PlannerRegionRow } from './PlannerRegionRow'
import { PlannerTableHeader } from './PlannerTableHeader'
import { WaypointDetailsDrawer } from './WaypointDetailsDrawer'
import { WaypointRowItem } from './WaypointRowItem'

const rowStyle: CSSProperties = {
  contentVisibility: 'auto',
  containIntrinsicSize: '44px',
}

/**
 * PlannerRouteTableProps defines the table items, column display toggles,
 * drag-and-drop state, carrier route state, and row action callbacks rendered
 * by the expedition route table.
 */
export interface PlannerRouteTableProps {
  plan: expedition.PlanView
  tableItems: PlannerTableItem[]
  dropSlotGaps: ReadonlySet<number>
  modesList: modes.Mode[]
  isPlanning: boolean
  isCompleted: boolean
  lastVisitedIndex: number
  lastFcArrivedIndex: number
  visibleColumnCount: number
  tableMinWidthPixels: number
  showNotes: boolean
  showCarrierStop: boolean
  externalLinkInara?: string
  externalLinkEdsm?: string
  externalLinkGec?: string
  expandedRowIds: Record<number, boolean>
  detailsById: Record<number, expedition.WaypointDetails>
  loadingDetails: Record<number, boolean>
  copiedSystemRowId: number | null
  insertMenuAfterId: number | null
  heldOrDraggedIndex: number | null
  draggedIndex: number | null
  dragOverGapIndex: number | null
  activeCarrierRoute: expedition.CarrierRouteResult | null
  activeCarrierTargetRowId: number
  isManualCarrierRoute: boolean
  nextFcStopEntry: NextCarrierStopEntry | null
  routedCarrierStopIndex: number
  getRowDropProps: (index: number) => {
    onDragOver?: (event: DragEvent<HTMLTableRowElement>) => void
    onDrop?: (event: DragEvent<HTMLTableRowElement>) => void
  }
  getSlotDropProps: (
    gapIndex: number,
    targetIndex: number,
    preview: expedition.DropSlotPreview,
  ) => {
    onDragOver: (event: DragEvent<HTMLTableRowElement>) => void
    onDrop: (event: DragEvent<HTMLTableRowElement>) => void
  }
  onClearDragOverGap: () => void
  onToggleShowNotes: () => void
  onToggleShowCarrierStop: () => void
  onChangeRegion: (rowId: number, regionId: number) => void
  onSetInsertMenuAfterId: (rowId: number | null) => void
  onInsert: (afterRowId: number, rowType: 'waypoint' | 'region') => void
  onDeleteRow: (rowView: expedition.RowView) => void
  onLegModeChange: (rowId: number, modeId: number) => void
  onToggleExpand: (rowId: number) => void
  onOriginSystemFilledChange?: (hasSystem: boolean) => void
  onFieldBlur: (
    row: expedition.Row,
    fieldName: 'systemName' | 'poiName' | 'notes',
    fieldValue: string,
  ) => void
  onCopySystem: (rowId: number, systemName: string) => void
  onToggleFcStop: (row: expedition.Row) => void
  onOverrideFcArrived: (rowView: expedition.RowView) => void
  onOpenCarrierRoutePrompt: (rowIndex: number, targetRow: expedition.Row) => void
  onRowHoldStart: (index: number) => void
  onRowHoldEnd: () => void
  onRowDragStart: (dragRowIndex: number, dragRowId: number, event: DragEvent) => void
  onRowDragEnd: () => void
}

/**
 * PlannerRouteTable renders the expedition flight plan table header and rows,
 * dispatching region banners, waypoint rows, collapsed visited summaries,
 * dashed historical drop slots, and expanded leg telemetry drawers.
 */
export function PlannerRouteTable({
  plan,
  tableItems,
  dropSlotGaps,
  modesList,
  isPlanning,
  isCompleted,
  lastVisitedIndex,
  lastFcArrivedIndex,
  visibleColumnCount,
  tableMinWidthPixels,
  showNotes,
  showCarrierStop,
  externalLinkInara,
  externalLinkEdsm,
  externalLinkGec,
  expandedRowIds,
  detailsById,
  loadingDetails,
  copiedSystemRowId,
  insertMenuAfterId,
  heldOrDraggedIndex,
  draggedIndex,
  dragOverGapIndex,
  activeCarrierRoute,
  activeCarrierTargetRowId,
  isManualCarrierRoute,
  nextFcStopEntry,
  routedCarrierStopIndex,
  getRowDropProps,
  getSlotDropProps,
  onClearDragOverGap,
  onToggleShowNotes,
  onToggleShowCarrierStop,
  onChangeRegion,
  onSetInsertMenuAfterId,
  onInsert,
  onDeleteRow,
  onLegModeChange,
  onToggleExpand,
  onOriginSystemFilledChange,
  onFieldBlur,
  onCopySystem,
  onToggleFcStop,
  onOverrideFcArrived,
  onOpenCarrierRoutePrompt,
  onRowHoldStart,
  onRowHoldEnd,
  onRowDragStart,
  onRowDragEnd,
}: PlannerRouteTableProps) {
  const rowViewById = useMemo(() => {
    const rowMap = new Map<number, expedition.RowView>()
    for (const item of tableItems) {
      if (item.kind === 'region' || item.kind === 'waypoint') {
        rowMap.set(item.rowView.row.id, item.rowView)
      }
    }
    return rowMap
  }, [tableItems])

  const regionBorderColorByRowId = useMemo(() => {
    const borderMap = new Map<number, string>()
    if (isPlanning || !plan.rows?.length) return borderMap
    let followingBorderColor: string | undefined
    // Walk plan.rows in reverse so consecutive Region Header rows inherit the leg border color of the first following waypoint.
    for (let i = plan.rows.length - 1; i >= 0; i--) {
      const candidateRowView = plan.rows[i]
      if (candidateRowView.row.rowType === 'region') {
        if (followingBorderColor) {
          borderMap.set(candidateRowView.row.id, followingBorderColor)
        }
      } else {
        followingBorderColor = getWaypointLegBorderColor(candidateRowView, isPlanning)
      }
    }
    return borderMap
  }, [isPlanning, plan.rows])

  const latestCallbacksRef = useRef({
    rowViewById,
    insertMenuAfterId,
    onSetInsertMenuAfterId,
    onChangeRegion,
    onInsert,
    onDeleteRow,
    onToggleExpand,
    onOriginSystemFilledChange,
    onFieldBlur,
    onCopySystem,
    onToggleFcStop,
    onOverrideFcArrived,
    onOpenCarrierRoutePrompt,
    onRowHoldStart,
    onRowHoldEnd,
    onRowDragStart,
    onRowDragEnd,
  })
  latestCallbacksRef.current = {
    rowViewById,
    insertMenuAfterId,
    onSetInsertMenuAfterId,
    onChangeRegion,
    onInsert,
    onDeleteRow,
    onToggleExpand,
    onOriginSystemFilledChange,
    onFieldBlur,
    onCopySystem,
    onToggleFcStop,
    onOverrideFcArrived,
    onOpenCarrierRoutePrompt,
    onRowHoldStart,
    onRowHoldEnd,
    onRowDragStart,
    onRowDragEnd,
  }

  // Keep per-row callback identities constant so memoized rows skip unaffected re-renders.
  const stableRowHandlers = useMemo(
    () => ({
      onChangeRegion: (rowId: number, regionId: number) =>
        latestCallbacksRef.current.onChangeRegion(rowId, regionId),
      onToggleInsertMenu: (rowId: number) => {
        const { insertMenuAfterId: openMenuRowId, onSetInsertMenuAfterId: setMenuAfterId } =
          latestCallbacksRef.current
        setMenuAfterId(openMenuRowId === rowId ? null : rowId)
      },
      onInsert: (afterRowId: number, rowType: 'waypoint' | 'region') =>
        latestCallbacksRef.current.onInsert(afterRowId, rowType),
      onDelete: (rowId: number) => {
        const targetRowView = latestCallbacksRef.current.rowViewById.get(rowId)
        if (targetRowView) latestCallbacksRef.current.onDeleteRow(targetRowView)
      },
      onToggleExpand: (rowId: number) => latestCallbacksRef.current.onToggleExpand(rowId),
      onOriginSystemFilledChange: (hasSystem: boolean) =>
        latestCallbacksRef.current.onOriginSystemFilledChange?.(hasSystem),
      onFieldBlur: (
        row: expedition.Row,
        fieldName: 'systemName' | 'poiName' | 'notes',
        fieldValue: string,
      ) => latestCallbacksRef.current.onFieldBlur(row, fieldName, fieldValue),
      onCopySystem: (rowId: number, systemName: string) =>
        latestCallbacksRef.current.onCopySystem(rowId, systemName),
      onToggleFcStop: (row: expedition.Row) => latestCallbacksRef.current.onToggleFcStop(row),
      onOverrideFcArrived: (rowView: expedition.RowView) =>
        latestCallbacksRef.current.onOverrideFcArrived(rowView),
      onOpenCarrierRoutePrompt: (rowIndex: number, targetRow: expedition.Row) =>
        latestCallbacksRef.current.onOpenCarrierRoutePrompt(rowIndex, targetRow),
      onHoldStart: (rowIndex: number) => latestCallbacksRef.current.onRowHoldStart(rowIndex),
      onHoldEnd: () => latestCallbacksRef.current.onRowHoldEnd(),
      onDragStart: (rowIndex: number, rowId: number, event: DragEvent) =>
        latestCallbacksRef.current.onRowDragStart(rowIndex, rowId, event),
      onDragEnd: () => latestCallbacksRef.current.onRowDragEnd(),
    }),
    [],
  )
  const currentExpedition = plan.expedition
  const totalRows = plan.rows?.length ?? 0
  const heldRowView = heldOrDraggedIndex !== null ? plan.rows?.[heldOrDraggedIndex] : null
  const grabbedSystemName = heldRowView?.row?.systemName ?? ''

  return (
    <table
      style={{ minWidth: `${tableMinWidthPixels}px` }}
      className="w-full table-fixed border-separate border-spacing-0 text-left"
    >
      <PlannerTableHeader
        isPlanning={isPlanning}
        showNotes={showNotes}
        showCarrierStop={showCarrierStop}
        onToggleShowNotes={onToggleShowNotes}
        onToggleShowCarrierStop={onToggleShowCarrierStop}
      />

      <tbody className="text-xs">
        {tableItems.map((item) => {
          if (item.kind === 'collapsed_visited') {
            return (
              <PlannerCollapsedVisitedRow
                key={item.key}
                visibleColumnCount={visibleColumnCount}
                waypointCount={item.waypointCount}
                regionCount={item.regionCount}
                firstWaypointNumber={item.firstWaypointNumber}
                firstSystemName={item.firstSystemName}
                lastWaypointNumber={item.lastWaypointNumber}
                lastSystemName={item.lastSystemName}
                onDragOver={onClearDragOverGap}
              />
            )
          }

          if (item.kind === 'drop_slot') {
            const slotDropProps = getSlotDropProps(item.gapIndex, item.targetIndex, item.preview)
            return (
              <PlannerDropSlotRow
                key={item.key}
                gapIndex={item.gapIndex}
                visibleColumnCount={visibleColumnCount}
                grabbedSystemName={grabbedSystemName}
                preview={item.preview}
                isDragTarget={dragOverGapIndex === item.gapIndex && draggedIndex !== null}
                onDragOver={slotDropProps.onDragOver}
                onDrop={slotDropProps.onDrop}
              />
            )
          }

          const rowView = item.rowView
          const row = rowView.row
          const index = item.index
          const isLocked = isCompleted
          const isBeingDragged = heldOrDraggedIndex === index
          const topGapHighlight =
            draggedIndex !== null &&
            dragOverGapIndex === index &&
            !dropSlotGaps.has(index)
          const bottomGapHighlight =
            draggedIndex !== null &&
            index === totalRows - 1 &&
            dragOverGapIndex === totalRows &&
            !dropSlotGaps.has(totalRows)

          if (item.kind === 'region') {
            return (
              <PlannerRegionRow
                key={item.key}
                expeditionId={currentExpedition.id}
                rowView={rowView}
                index={index}
                visibleColumnCount={visibleColumnCount}
                isPlanning={isPlanning}
                isLocked={isLocked}
                isBeingDragged={isBeingDragged}
                topGapHighlight={topGapHighlight}
                bottomGapHighlight={bottomGapHighlight}
                leftBorderColor={regionBorderColorByRowId.get(row.id)}
                lastVisitedIndex={lastVisitedIndex}
                insertMenuOpen={insertMenuAfterId === row.id}
                rowStyle={rowStyle}
                dropProps={getRowDropProps(index)}
                onChangeRegion={stableRowHandlers.onChangeRegion}
                onToggleInsertMenu={stableRowHandlers.onToggleInsertMenu}
                onInsert={stableRowHandlers.onInsert}
                onDelete={stableRowHandlers.onDelete}
                onHoldStart={stableRowHandlers.onHoldStart}
                onHoldEnd={stableRowHandlers.onHoldEnd}
                onDragStart={stableRowHandlers.onDragStart}
                onDragEnd={stableRowHandlers.onDragEnd}
              />
            )
          }

          if (item.kind === 'drawer') {
            return (
              <tr key={item.key} style={rowStyle}>
                <td colSpan={visibleColumnCount} className="p-0">
                  <WaypointDetailsDrawer
                    details={detailsById[row.id] ?? null}
                    loading={Boolean(loadingDetails[row.id])}
                    rowView={rowView}
                    modeId={rowView.legModeId}
                    modeColor={rowView.legModeColor}
                    modesList={modesList}
                    onChangeMode={(nextModeId) => onLegModeChange(row.id, nextModeId)}
                  />
                </td>
              </tr>
            )
          }

          const isExpanded = Boolean(expandedRowIds[row.id])
          const isNextFcStop = Boolean(
            row.fcStop && !row.fcArrived && nextFcStopEntry && row.id === nextFcStopEntry.row.id,
          )
          const isNextWaypoint = Boolean(
            rowView.isNextWaypoint || (plan.nextWaypointRowId && row.id === plan.nextWaypointRowId),
          )
          const hasActiveCarrierRoute = Boolean(
            activeCarrierRoute &&
              (isManualCarrierRoute ? isNextFcStop : activeCarrierTargetRowId === row.id),
          )
          // Manual logging leaves the target stop editable until arrival is logged;
          // an active Spansh route locks the target stop and all preceding carrier stops.
          const isSpanshRoutedFcStop = Boolean(
            activeCarrierRoute && !isManualCarrierRoute && activeCarrierTargetRowId === row.id,
          )
          const isFcStopLocked = Boolean(
            activeCarrierRoute &&
              routedCarrierStopIndex >= 0 &&
              (isManualCarrierRoute
                ? index < routedCarrierStopIndex
                : index <= routedCarrierStopIndex),
          )

          return (
            <WaypointRowItem
              key={item.key}
              rowView={rowView}
              index={index}
              isPlanning={isPlanning}
              isLocked={isLocked}
              isFcStopLocked={isFcStopLocked}
              isActivelyRoutedFcStop={isSpanshRoutedFcStop}
              lastFcArrivedIndex={lastFcArrivedIndex}
              isBeingDragged={isBeingDragged}
              topGapHighlight={topGapHighlight}
              bottomGapHighlight={bottomGapHighlight}
              isExpanded={isExpanded}
              isNextFcStop={isNextFcStop && !activeCarrierRoute}
              isNextWaypoint={isNextWaypoint}
              playerSystem={plan.playerSystem}
              hasActiveCarrierRoute={hasActiveCarrierRoute}
              isSystemCopied={copiedSystemRowId === row.id}
              insertMenuOpen={insertMenuAfterId === row.id}
              lastVisitedIndex={lastVisitedIndex}
              rowStyle={rowStyle}
              dropProps={getRowDropProps(index)}
              showNotes={showNotes}
              showCarrierStop={showCarrierStop}
              externalLinkInara={externalLinkInara}
              externalLinkEdsm={externalLinkEdsm}
              externalLinkGec={externalLinkGec}
              onOriginSystemFilledChange={
                rowView.number === 1 && onOriginSystemFilledChange
                  ? stableRowHandlers.onOriginSystemFilledChange
                  : undefined
              }
              onToggleExpand={stableRowHandlers.onToggleExpand}
              onFieldBlur={stableRowHandlers.onFieldBlur}
              onCopySystem={stableRowHandlers.onCopySystem}
              onToggleFcStop={stableRowHandlers.onToggleFcStop}
              onOverrideFcArrived={stableRowHandlers.onOverrideFcArrived}
              onOpenCarrierRoutePrompt={stableRowHandlers.onOpenCarrierRoutePrompt}
              onToggleInsertMenu={stableRowHandlers.onToggleInsertMenu}
              onInsert={stableRowHandlers.onInsert}
              onDelete={stableRowHandlers.onDelete}
              onHoldStart={stableRowHandlers.onHoldStart}
              onHoldEnd={stableRowHandlers.onHoldEnd}
              onDragStart={stableRowHandlers.onDragStart}
              onDragEnd={stableRowHandlers.onDragEnd}
            />
          )
        })}
      </tbody>
    </table>
  )
}
