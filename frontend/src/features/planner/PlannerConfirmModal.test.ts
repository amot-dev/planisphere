import { createElement } from 'react'
import { renderToString } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { PlannerConfirmModal } from './PlannerConfirmModal'

describe('PlannerConfirmModal', () => {
  it('renders delete visited, reset carrier arrival on move, and manual state-repair override modals', () => {
    const deleteVisitedHtml = renderToString(
      createElement(PlannerConfirmModal, {
        confirmState: {
          kind: 'delete_waypoint',
          rowId: 12,
          waypointNumber: 2,
          systemName: 'Alpha Centauri',
          visitedAtGalactic: '3312-01-10 10:00 UTC',
          isArrivedFcStop: false,
        },
        onConfirm: () => {},
        onCancel: () => {},
      }),
    )
    expect(deleteVisitedHtml).toContain('Delete Visited Waypoint')
    expect(deleteVisitedHtml).toContain('3312-01-10 10:00 UTC')
    expect(deleteVisitedHtml).toContain('DELETE WAYPOINT')

    const deleteArrivedFcHtml = renderToString(
      createElement(PlannerConfirmModal, {
        confirmState: {
          kind: 'delete_waypoint',
          rowId: 14,
          waypointNumber: 3,
          systemName: 'Colonia',
          isArrivedFcStop: true,
        },
        onConfirm: () => {},
        onCancel: () => {},
      }),
    )
    expect(deleteArrivedFcHtml).toContain('Delete Arrived Carrier Stop')
    expect(deleteArrivedFcHtml).toContain('ARRIVED')
    expect(deleteArrivedFcHtml).toContain('DELETE WAYPOINT')

    const deleteVisitedAndArrivedFcHtml = renderToString(
      createElement(PlannerConfirmModal, {
        confirmState: {
          kind: 'delete_waypoint',
          rowId: 16,
          waypointNumber: 4,
          systemName: 'Sagittarius A*',
          visitedAtGalactic: '3312-02-01 18:00 UTC',
          isArrivedFcStop: true,
        },
        onConfirm: () => {},
        onCancel: () => {},
      }),
    )
    expect(deleteVisitedAndArrivedFcHtml).toContain('Delete Visited Carrier Stop')
    expect(deleteVisitedAndArrivedFcHtml).toContain('3312-02-01 18:00 UTC')
    expect(deleteVisitedAndArrivedFcHtml).toContain('ARRIVED')

    const moveResetFcHtml = renderToString(
      createElement(PlannerConfirmModal, {
        confirmState: {
          kind: 'move_reset_fc',
          rowId: 15,
          fromIndex: 1,
          toIndex: 4,
          waypointNumber: 3,
          systemName: 'Colonia',
        },
        onConfirm: () => {},
        onCancel: () => {},
      }),
    )
    expect(moveResetFcHtml).toContain('Reset Fleet Carrier Arrival')
    expect(moveResetFcHtml).toContain('MOVE AND RESET TO PENDING')

    const overrideFcHtml = renderToString(
      createElement(PlannerConfirmModal, {
        confirmState: {
          kind: 'override_fc_arrival',
          rowId: 18,
          waypointNumber: 4,
          systemName: 'Rohini',
          targetArrived: true,
          cascadedCount: 2,
        },
        onConfirm: () => {},
        onCancel: () => {},
      }),
    )
    expect(overrideFcHtml).toContain('Override Fleet Carrier Arrival Status')
    expect(overrideFcHtml).toContain('Manual State Repair Override')
    expect(overrideFcHtml).toContain('2 preceding pending carrier stops will also be marked ARRIVED')
    expect(overrideFcHtml).toContain('MARK AS ARRIVED')
  })
})

