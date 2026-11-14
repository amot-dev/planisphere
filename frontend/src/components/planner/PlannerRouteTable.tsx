import type { CSSProperties, DragEvent } from 'react'
import { app, expedition, modes } from '../../../wailsjs/go/models'
import type { NextCarrierStopEntry } from './carrierRouteHelpers'
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
  visibleColumnCount: number
  tableMinWidthPixels: number
  showNotes: boolean
  showCarrierStop: boolean
  expandedRowIds: Record<number, boolean>
  detailsById: Record<number, expedition.WaypointDetails>
  loadingDetails: Record<number, boolean>
  copiedSystemRowId: number | null
  insertMenuAfterId: number | null
  draggedIndex: number | null
  dragOverIndex: number | null
  activeCarrierRoute: app.CarrierRouteResult | null
  activeCarrierTargetRowId: number
  isManualCarrierRoute: boolean
  nextFcStopEntry: NextCarrierStopEntry | null
  routedCarrierStopIndex: number
  getRowDropProps: (
    index: number,
    isDraggable: boolean,
  ) => {
    onDragOver?: (event: DragEvent) => void
    onDrop?: (event: DragEvent) => void
  }
  onToggleShowNotes: () => void
  onToggleShowCarrierStop: () => void
  onChangeRegion: (rowId: number, regionId: number) => void
  onSetInsertMenuAfterId: (rowId: number | null) => void
  onInsert: (afterRowId: number, rowType: 'waypoint' | 'region') => void
  onMoveRow: (rowId: number, fromIndex: number, toIndex: number) => void
  onDeleteRow: (rowId: number) => void
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
  onOpenCarrierRoutePrompt: (rowIndex: number, targetRow: expedition.Row) => void
  onRowDragStart: (dragRowIndex: number, dragRowId: number, event: DragEvent) => void
  onRowDragEnd: () => void
}

/**
 * PlannerRouteTable renders the expedition flight plan table header and rows,
 * dispatching region banners, waypoint rows, and expanded leg telemetry drawers.
 */
export function PlannerRouteTable({
  plan,
  tableItems,
  modesList,
  isPlanning,
  isCompleted,
  lastVisitedIndex,
  visibleColumnCount,
  tableMinWidthPixels,
  showNotes,
  showCarrierStop,
  expandedRowIds,
  detailsById,
  loadingDetails,
  copiedSystemRowId,
  insertMenuAfterId,
  draggedIndex,
  dragOverIndex,
  activeCarrierRoute,
  activeCarrierTargetRowId,
  isManualCarrierRoute,
  nextFcStopEntry,
  routedCarrierStopIndex,
  getRowDropProps,
  onToggleShowNotes,
  onToggleShowCarrierStop,
  onChangeRegion,
  onSetInsertMenuAfterId,
  onInsert,
  onMoveRow,
  onDeleteRow,
  onLegModeChange,
  onToggleExpand,
  onOriginSystemFilledChange,
  onFieldBlur,
  onCopySystem,
  onToggleFcStop,
  onOpenCarrierRoutePrompt,
  onRowDragStart,
  onRowDragEnd,
}: PlannerRouteTableProps) {
  const currentExpedition = plan.expedition

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
          const rowView = item.rowView
          const row = rowView.row
          const index = item.index
          const isLocked = isCompleted || index <= lastVisitedIndex
          const isDraggable = !isLocked
          const isBeingDragged = draggedIndex === index
          const isDragTarget =
            dragOverIndex === index && draggedIndex !== null && draggedIndex !== index

          if (item.kind === 'region') {
            return (
              <PlannerRegionRow
                key={item.key}
                expeditionId={currentExpedition.id}
                rowView={rowView}
                index={index}
                visibleColumnCount={visibleColumnCount}
                isLocked={isLocked}
                isBeingDragged={isBeingDragged}
                isDragTarget={isDragTarget}
                lastVisitedIndex={lastVisitedIndex}
                totalRows={plan.rows?.length ?? 0}
                insertMenuOpen={insertMenuAfterId === row.id}
                rowStyle={rowStyle}
                dropProps={getRowDropProps(index, isDraggable)}
                onChangeRegion={onChangeRegion}
                onToggleInsertMenu={() =>
                  onSetInsertMenuAfterId(insertMenuAfterId === row.id ? null : row.id)
                }
                onInsert={(insertType) => onInsert(row.id, insertType)}
                onMove={(targetIndex) => onMoveRow(row.id, index, targetIndex)}
                onDelete={() => onDeleteRow(row.id)}
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
          // an active Spansh route locks through routedCarrierStopIndex while hops are in progress.
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
              isBeingDragged={isBeingDragged}
              isDragTarget={isDragTarget}
              isExpanded={isExpanded}
              isNextFcStop={isNextFcStop && !activeCarrierRoute}
              isNextWaypoint={isNextWaypoint}
              playerSystem={plan.playerSystem}
              hasActiveCarrierRoute={hasActiveCarrierRoute}
              copiedSystemRowId={copiedSystemRowId}
              insertMenuOpen={insertMenuAfterId === row.id}
              lastVisitedIndex={lastVisitedIndex}
              totalRows={plan.rows?.length ?? 0}
              rowStyle={rowStyle}
              dropProps={getRowDropProps(index, isDraggable)}
              showNotes={showNotes}
              showCarrierStop={showCarrierStop}
              onOriginSystemFilledChange={
                rowView.number === 1 ? onOriginSystemFilledChange : undefined
              }
              onToggleExpand={onToggleExpand}
              onFieldBlur={onFieldBlur}
              onCopySystem={onCopySystem}
              onToggleFcStop={onToggleFcStop}
              onOpenCarrierRoutePrompt={onOpenCarrierRoutePrompt}
              onToggleInsertMenu={(id) =>
                onSetInsertMenuAfterId(insertMenuAfterId === id ? null : id)
              }
              onInsert={onInsert}
              onMove={(id, targetIndex) => onMoveRow(id, index, targetIndex)}
              onDelete={onDeleteRow}
              onDragStart={onRowDragStart}
              onDragEnd={onRowDragEnd}
            />
          )
        })}
      </tbody>
    </table>
  )
}
