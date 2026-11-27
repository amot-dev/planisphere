import { type ComponentProps } from 'react'
import { renderToString } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { PlannerConfirmModal } from './PlannerConfirmModal'

type ConfirmState = ComponentProps<typeof PlannerConfirmModal>['confirmState']

describe('PlannerConfirmModal', () => {
  it.each<{
    caseLabel: string
    confirmState: ConfirmState
    expectedText: string[]
  }>([
    {
      caseLabel: 'delete visited waypoint',
      confirmState: {
        kind: 'delete_waypoint',
        rowId: 12,
        waypointNumber: 2,
        systemName: 'Alpha Centauri',
        visitedAtGalactic: '3312-01-10 10:00 UTC',
        isArrivedFcStop: false,
      },
      expectedText: ['Delete Visited Waypoint', '3312-01-10 10:00 UTC', 'DELETE WAYPOINT'],
    },
    {
      caseLabel: 'delete arrived carrier stop',
      confirmState: {
        kind: 'delete_waypoint',
        rowId: 14,
        waypointNumber: 3,
        systemName: 'Colonia',
        isArrivedFcStop: true,
      },
      expectedText: ['Delete Arrived Carrier Stop', 'ARRIVED', 'DELETE WAYPOINT'],
    },
    {
      caseLabel: 'delete visited and arrived carrier stop',
      confirmState: {
        kind: 'delete_waypoint',
        rowId: 16,
        waypointNumber: 4,
        systemName: 'Sagittarius A*',
        visitedAtGalactic: '3312-02-01 18:00 UTC',
        isArrivedFcStop: true,
      },
      expectedText: ['Delete Visited Carrier Stop', '3312-02-01 18:00 UTC', 'ARRIVED'],
    },
    {
      caseLabel: 'reset carrier arrival on move',
      confirmState: {
        kind: 'move_reset_fc',
        rowId: 15,
        fromIndex: 1,
        toIndex: 4,
        waypointNumber: 3,
        systemName: 'Colonia',
      },
      expectedText: ['Reset Fleet Carrier Arrival', 'MOVE AND RESET TO PENDING'],
    },
    {
      caseLabel: 'manual state-repair carrier arrival override',
      confirmState: {
        kind: 'override_fc_arrival',
        rowId: 18,
        waypointNumber: 4,
        systemName: 'Rohini',
        targetArrived: true,
        cascadedCount: 2,
      },
      expectedText: [
        'Override Fleet Carrier Arrival Status',
        'Manual State Repair Override',
        '2 preceding pending carrier stops will also be marked ARRIVED',
        'MARK AS ARRIVED',
      ],
    },
  ])('renders $caseLabel confirmation modal', ({ confirmState, expectedText }) => {
    const renderedHtml = renderToString(
      <PlannerConfirmModal
        confirmState={confirmState}
        onConfirm={() => {}}
        onCancel={() => {}}
      />,
    )
    for (const expectedSubstring of expectedText) {
      expect(renderedHtml).toContain(expectedSubstring)
    }
  })
})
