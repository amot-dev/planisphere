import { createElement } from 'react'
import { renderToString } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { expedition } from '../../../api/models'
import { WaypointRowItem, type WaypointRowHandlers } from './WaypointRowItem'

const defaultRowHandlers: WaypointRowHandlers = {
  onToggleExpand: () => {},
  onFieldBlur: () => {},
  onCopySystem: () => {},
  onToggleFcStop: () => {},
  onOpenCarrierRoutePrompt: () => {},
  onToggleInsertMenu: () => {},
  onInsert: () => {},
  onDelete: () => {},
  onDragStart: () => {},
  onDragEnd: () => {},
}

function renderRowItem(
  props: Partial<Parameters<typeof WaypointRowItem>[0]> & { rowView: expedition.RowView },
) {
  const defaultProps: Parameters<typeof WaypointRowItem>[0] = {
    index: 1,
    isPlanning: false,
    isLocked: false,
    isBeingDragged: false,
    isExpanded: false,
    isSystemCopied: false,
    insertMenuOpen: false,
    lastVisitedIndex: 0,
    rowStyle: {},
    dropProps: {},
    handlers: defaultRowHandlers,
    ...props,
  }
  return renderToString(
    createElement(
      'table',
      null,
      createElement('tbody', null, createElement(WaypointRowItem, defaultProps)),
    ),
  )
}

describe('WaypointCarrierStopCell Rules and Badges', () => {
  it('renders carrier stop elements with uniform h-5 height inside h-7 container for both active stop and add button', () => {
    const fcStopRowView = expedition.RowView.createFrom({
      row: {
        id: 20,
        expeditionId: 1,
        rowType: 'waypoint',
        systemName: 'Colonia',
        seq: 19,
        fcStop: true,
        fcArrived: true,
      },
      number: 20,
      visitState: 'visited',
      permissions: {
        systemLocked: true,
        canAddFcStop: false,
        canRemoveFcStop: false,
        canOverrideFcBadge: false,
      },
    })

    const html = renderRowItem({
      rowView: fcStopRowView,
      index: 19,
      isLocked: true,
      lastVisitedIndex: 20,
    })

    expect(html).toContain('ARRIVED')
    expect(html).toContain('h-5 px-1.5 inline-flex items-center justify-center')
    expect(html).toContain('h-7 inline-flex items-center justify-center gap-1.5 whitespace-nowrap')

    const lockedFcRowHtml = renderRowItem({
      rowView: expedition.RowView.createFrom({
        row: {
          id: 501,
          expeditionId: 1,
          rowType: 'waypoint',
          systemName: 'Rohini',
          fcStop: true,
          fcArrived: false,
        },
        number: 2,
        visitState: 'future',
        permissions: {
          systemLocked: false,
          canAddFcStop: false,
          canRemoveFcStop: false,
          canOverrideFcBadge: false,
          isFcStopLocked: true,
          hasActiveCarrierRoute: true,
        },
      }),
      index: 1,
      lastVisitedIndex: 0,
    })

    expect(lockedFcRowHtml).toContain('ROUTING')
    expect(lockedFcRowHtml).not.toContain('Remove Fleet Carrier stop designation')
    expect(lockedFcRowHtml).not.toContain('+ FC STOP')
  })

  it('enforces Fleet Carrier stop rules: + FC STOP after lastFcArrivedIndex, remove only on PENDING, and lock active route target', () => {
    const visitedAfterFcFrontierHtml = renderRowItem({
      rowView: expedition.RowView.createFrom({
        row: {
          id: 61,
          expeditionId: 1,
          rowType: 'waypoint',
          systemName: 'Gandharvi',
          visitedAt: '3312-01-12T00:00:00Z',
          fcStop: false,
        },
        number: 3,
        visitState: 'visited',
        permissions: {
          systemLocked: true,
          canAddFcStop: true,
          canRemoveFcStop: true,
        },
      }),
      index: 2,
      lastVisitedIndex: 2,
    })
    expect(visitedAfterFcFrontierHtml).toContain('+ FC STOP')

    const visitedBeforeFcFrontierHtml = renderRowItem({
      rowView: expedition.RowView.createFrom({
        row: {
          id: 60,
          expeditionId: 1,
          rowType: 'waypoint',
          systemName: 'Rohini',
          visitedAt: '3312-01-10T00:00:00Z',
          fcStop: false,
        },
        number: 2,
        visitState: 'visited',
        permissions: {
          systemLocked: true,
          canAddFcStop: false,
          canRemoveFcStop: true,
        },
      }),
      index: 1,
      lastVisitedIndex: 2,
    })
    expect(visitedBeforeFcFrontierHtml).not.toContain('+ FC STOP')

    const arrivedStopHtml = renderRowItem({
      rowView: expedition.RowView.createFrom({
        row: {
          id: 62,
          expeditionId: 1,
          rowType: 'waypoint',
          systemName: 'Colonia',
          fcStop: true,
          fcArrived: true,
        },
        number: 4,
        visitState: 'future',
        permissions: {
          systemLocked: true,
          systemLockReason: 'Locked while Fleet Carrier is marked arrived at this waypoint',
          canAddFcStop: false,
          canRemoveFcStop: false,
          canOverrideFcBadge: true,
        },
      }),
      index: 3,
      lastVisitedIndex: 2,
      handlers: { ...defaultRowHandlers, onOverrideFcArrived: () => {} },
    })
    expect(arrivedStopHtml).not.toContain('placeholder="Star System"')
    expect(arrivedStopHtml).toContain('title="Locked while Fleet Carrier is marked arrived at this waypoint"')
    expect(arrivedStopHtml).toContain('ARRIVED')
    expect(arrivedStopHtml).toContain('Carrier has arrived at this waypoint (click to override)')
    expect(arrivedStopHtml).not.toContain('Remove Fleet Carrier stop designation')

    const pendingStopHtml = renderRowItem({
      rowView: expedition.RowView.createFrom({
        row: {
          id: 63,
          expeditionId: 1,
          rowType: 'waypoint',
          systemName: 'Sagittarius A*',
          fcStop: true,
          fcArrived: false,
        },
        number: 5,
        visitState: 'future',
        permissions: {
          systemLocked: false,
          canAddFcStop: true,
          canRemoveFcStop: true,
          canOverrideFcBadge: true,
          isNextFcStop: true,
        },
      }),
      index: 4,
      lastVisitedIndex: 3,
    })
    expect(pendingStopHtml).toContain('PENDING')
    expect(pendingStopHtml).toContain('ROUTE')
    expect(pendingStopHtml).toContain('Remove Fleet Carrier stop designation')

    const activelyRoutedStopHtml = renderRowItem({
      rowView: expedition.RowView.createFrom({
        row: {
          id: 64,
          expeditionId: 1,
          rowType: 'waypoint',
          systemName: 'Beagle Point',
          fcStop: true,
          fcArrived: false,
        },
        number: 6,
        visitState: 'future',
        permissions: {
          systemLocked: true,
          systemLockReason: 'Locked while Fleet Carrier route to this waypoint is active',
          canAddFcStop: false,
          canRemoveFcStop: false,
          canOverrideFcBadge: false,
          isFcStopLocked: true,
          hasActiveCarrierRoute: true,
          isSpanshRoutedFcStop: true,
        },
      }),
      index: 5,
      lastVisitedIndex: 3,
    })
    expect(activelyRoutedStopHtml).toContain('ROUTING')
    expect(activelyRoutedStopHtml).toContain(
      'title="Locked while Fleet Carrier route to this waypoint is active"',
    )
  })
})
