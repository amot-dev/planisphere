import { describe, expect, it } from 'vitest'
import { exporter } from '../../api/models'
import {
  computeAllHighlightLeaderboards,
  selectHighlightCards,
} from './exportHighlights'
import { buildSampleExportData } from '../../test/fixtures/exportFixtures'

describe('exportHighlights', () => {
  it('selects canonical Go leaderboards including IN SYSTEM while excluding unvisited legs', () => {
    const data = buildSampleExportData()
    const leaderboards = computeAllHighlightLeaderboards(data)
    expect(leaderboards).toHaveLength(11)

    const mostLucrative = leaderboards.find((board) => board.cardId === 'mostLucrative')!
    expect(mostLucrative.entries.map((entry) => entry.waypointNumber)).toEqual([4, 2, 3])
    expect(mostLucrative.entries[0].inSystem).toBe(true)

    const shortest = leaderboards.find((board) => board.cardId === 'shortestDistance')!
    // Unvisited row #5 (100 Ly in-progress) must be excluded; WP #4 (600 Ly) wins shortest.
    expect(shortest.entries.map((entry) => entry.waypointNumber)).toEqual([4, 2, 3])

    const furthestFromSol = leaderboards.find((board) => board.cardId === 'furthestFromSol')!
    // WP #4 at (4000, 0, 3000) = 5,000 Ly; WP #5 is unvisited and excluded.
    expect(furthestFromSol.entries[0].waypointNumber).toBe(4)
    expect(furthestFromSol.entries[0].primaryValue).toBeCloseTo(5000, 3)

    const costliest = leaderboards.find((board) => board.cardId === 'costliestMistake')!
    expect(costliest.entries).toHaveLength(1)
    expect(costliest.entries[0].waypointNumber).toBe(3)
    expect(costliest.entries[0].primaryValue).toBe(3_000_000)
  })

  it('slices selected highlight leaderboards to Top 1 / Top 3 / Top 5 and omits empty cards', () => {
    const baseData = buildSampleExportData()
    const dataWithoutLosses = buildSampleExportData({
      rowDetails: baseData.rowDetails.map((detail, index) =>
        index === 1
          ? ({
              ...detail,
              realization: { ...detail.realization, cartoLostDeath: 0, organicLostDeath: 0 },
            } as typeof detail)
          : detail,
      ),
    })

    const boards = computeAllHighlightLeaderboards(dataWithoutLosses)
    const top1 = selectHighlightCards(boards, ['mostLucrative', 'costliestMistake'], 1)
    expect(top1).toHaveLength(1)
    expect(top1[0].entries).toHaveLength(1)

    const top3 = selectHighlightCards(boards, ['mostLucrative'], 3)
    expect(top3[0].entries).toHaveLength(3)
  })

  it('routes variant toggles to highlightsVariants while preserving canonical cardId values on hydrated Wails snapshots', () => {
    const baseData = buildSampleExportData()
    const hydratedPayload = exporter.ExpeditionExport.createFrom({
      ...baseData,
      highlightsFullOrder: (baseData.highlightsFullOrder ?? []).map((board) =>
        board.cardId === 'mostLucrative'
          ? {
              ...board,
              entries: board.entries.map((entry, entryIndex) =>
                entryIndex === 0 ? { ...entry, formattedPrimary: '105,000,000 Cr [GO]' } : entry,
              ),
            }
          : board,
      ),
    })

    expect(computeAllHighlightLeaderboards(hydratedPayload, { cardIds: [] })).toEqual([])

    const reusedDefault = computeAllHighlightLeaderboards(hydratedPayload, {
      lucrativeMetric: 'estimated',
      distanceMetric: 'total',
      cardIds: ['mostLucrative', 'mostJumps'],
      maxDepth: 1,
    })
    expect(reusedDefault).toHaveLength(2)
    expect(reusedDefault[0].cardId).toBe('mostLucrative')
    expect(reusedDefault[0].entries).toHaveLength(1)
    expect(reusedDefault[0].entries[0].formattedPrimary).toBe('105,000,000 Cr [GO]')

    // Switching lucrativeMetric to 'realized' selects Go variant 'mostLucrativeRealized' under canonical cardId 'mostLucrative'.
    const realizedBoards = computeAllHighlightLeaderboards(hydratedPayload, {
      lucrativeMetric: 'realized',
      distanceMetric: 'total',
      cardIds: ['mostLucrative', 'mostJumps'],
      maxDepth: 1,
    })
    expect(realizedBoards[0].cardId).toBe('mostLucrative')
    expect(realizedBoards[0].title).toBe('Most Lucrative Leg')
    expect(realizedBoards[0].entries[0].formattedPrimary).toBe('95,000,000 Cr')
    expect(realizedBoards[1].entries[0].formattedPrimary).toBe('50 jumps')

    // Switching distanceMetric to 'ship' selects Go variants 'longestShipDistance' and 'shortestShipDistance'.
    const shipDistanceBoards = computeAllHighlightLeaderboards(hydratedPayload, {
      distanceMetric: 'ship',
      cardIds: ['longestDistance', 'shortestDistance'],
      maxDepth: 2,
    })
    expect(shipDistanceBoards.map((board) => board.cardId)).toEqual([
      'longestDistance',
      'shortestDistance',
    ])
    expect(shipDistanceBoards[0].entries[1].primaryValue).toBe(1000)
    expect(shipDistanceBoards[1].entries[0].primaryValue).toBe(600)
  })

  it('routes durationMetric and pace toggles to Go variant boards while keeping canonical cardIds', () => {
    const baseData = buildSampleExportData()
    const paceData = buildSampleExportData({
      rowDetails: baseData.rowDetails.map((detail) => {
        if (detail.rowId === 2) {
          return {
            ...detail,
            inGameSeconds: 7200,
            inGameDuration: '2h 00m',
            transitInGameSeconds: 3600,
            transitInGameDuration: '1h 00m',
          }
        }
        if (detail.rowId === 3) {
          return {
            ...detail,
            inGameSeconds: 10_800,
            inGameDuration: '3h 00m',
            transitInGameSeconds: 7200,
            transitInGameDuration: '2h 00m',
          }
        }
        return detail
      }) as typeof baseData.rowDetails,
    })

    const inGameDurationBoards = computeAllHighlightLeaderboards(paceData, {
      durationMetric: 'inGame',
      cardIds: ['longestDuration'],
      maxDepth: 2,
    })
    expect(inGameDurationBoards[0].cardId).toBe('longestDuration')
    expect(inGameDurationBoards[0].entries.map((entry) => entry.waypointNumber)).toEqual([3, 2])
    expect(inGameDurationBoards[0].entries[0].formattedPrimary).toBe('3h 00m')

    const earningEstimated = computeAllHighlightLeaderboards(paceData, {
      lucrativeMetric: 'estimated',
      cardIds: ['highestEarningPace'],
      maxDepth: 1,
    })
    expect(earningEstimated[0].cardId).toBe('highestEarningPace')
    expect(earningEstimated[0].entries[0].formattedPrimary).toBe('26,000,000 Cr/h')

    const earningRealized = computeAllHighlightLeaderboards(paceData, {
      lucrativeMetric: 'realized',
      cardIds: ['highestEarningPace'],
      maxDepth: 1,
    })
    expect(earningRealized[0].cardId).toBe('highestEarningPace')
    expect(earningRealized[0].entries[0].formattedPrimary).toBe('24,000,000 Cr/h')

    const transitTotal = computeAllHighlightLeaderboards(paceData, {
      distanceMetric: 'total',
      cardIds: ['fastestTransitPace'],
      maxDepth: 2,
    })
    expect(transitTotal[0].entries[1].formattedPrimary).toBe('1,200.0 Ly/h')

    const transitShip = computeAllHighlightLeaderboards(paceData, {
      distanceMetric: 'ship',
      cardIds: ['fastestTransitPace'],
      maxDepth: 2,
    })
    expect(transitShip[0].cardId).toBe('fastestTransitPace')
    expect(transitShip[0].entries[1].formattedPrimary).toBe('1,000.0 Ly/h')
  })
})
