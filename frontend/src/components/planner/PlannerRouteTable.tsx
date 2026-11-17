import type { CSSProperties, DragEvent } from 'react'
import { app, expedition, modes } from '../../../wailsjs/go/models'
import type { NextCarrierStopEntry } from './carrierRouteHelpers'
import { PlannerCollapsedVisitedRow, PlannerDropSlotRow } from './PlannerDragPreviewRows'
import type { PlannerTableItem } from './plannerPlanHelpers'
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
  modesList: modes.Mode[]
  isPlanning: boolean
  isCompleted: boolean
  lastVisitedIndex: number
  lastFcArrivedIndex: number
  visibleColumnCount: number
  tableMinWidthPixels: number
  showNotes: boolean
  showCarrierStop: boolean
  expandedRowIds: Record<number, boolean>
  detailsById: Record<number, expedition.WaypointDetails>
  loadingDetails: Record<number, boolean>
  copiedSystemRowId: number | null
  insertMenuAfterId: number | null
  heldOrDraggedIndex: number | null
  draggedIndex: number | null
  dragOverGapIndex: number | null
  activeCarrierRoute: app.CarrierRouteResult | null
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
  modesList,
  isPlanning,
  isCompleted,
  lastVisitedIndex,
  lastFcArrivedIndex,
  visibleColumnCount,
  tableMinWidthPixels,
  showNotes,
  showCarrierStop,
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
  const currentExpedition = plan.expedition
  const totalRows = plan.rows?.length ?? 0
  const heldRowView = heldOrDraggedIndex !== null ? plan.rows?.[heldOrDraggedIndex] : null
  const grabbedSystemName = heldRowView?.row?.systemName ?? ''
  const dropSlotGaps = new Set<number>()
  for (const item of tableItems) {
    if (item.kind === 'drop_slot') {
      dropSlotGaps.add(item.gapIndex)
    }
  }

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
                lastVisitedIndex={lastVisitedIndex}
                insertMenuOpen={insertMenuAfterId === row.id}
                rowStyle={rowStyle}
                dropProps={getRowDropProps(index)}
                onChangeRegion={onChangeRegion}
                onToggleInsertMenu={() =>
                  onSetInsertMenuAfterId(insertMenuAfterId === row.id ? null : row.id)
                }
                onInsert={(insertType) => onInsert(row.id, insertType)}
                onDelete={() => onDeleteRow(rowView)}
                onHoldStart={() => onRowHoldStart(index)}
                onHoldEnd={onRowHoldEnd}
                onDragStart={(event) => onRowDragStart(index, row.id, event)}
                onDragEnd={onRowDragEnd}
              />
            )
          }

          if (item.kind === 'drawer') {
            return (
              <tr key={item.key} style={rowStyle}>
                <td colSpan={visibleColumnCount} className="p-0 border-b border-[#24180e]">
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
              copiedSystemRowId={copiedSystemRowId}
              insertMenuOpen={insertMenuAfterId === row.id}
              lastVisitedIndex={lastVisitedIndex}
              rowStyle={rowStyle}
              dropProps={getRowDropProps(index)}
              showNotes={showNotes}
              showCarrierStop={showCarrierStop}
              onOriginSystemFilledChange={
                rowView.number === 1 ? onOriginSystemFilledChange : undefined
              }
              onToggleExpand={onToggleExpand}
              onFieldBlur={onFieldBlur}
              onCopySystem={onCopySystem}
              onToggleFcStop={onToggleFcStop}
              onOverrideFcArrived={onOverrideFcArrived}
              onOpenCarrierRoutePrompt={onOpenCarrierRoutePrompt}
              onToggleInsertMenu={(id) =>
                onSetInsertMenuAfterId(insertMenuAfterId === id ? null : id)
              }
              onInsert={onInsert}
              onDelete={() => onDeleteRow(rowView)}
              onHoldStart={onRowHoldStart}
              onHoldEnd={onRowHoldEnd}
              onDragStart={onRowDragStart}
              onDragEnd={onRowDragEnd}
            />
          )
        })}
      </tbody>
    </table>
  )
}
