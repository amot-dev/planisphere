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
    expect(leaderboards).toHaveLength(9)

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
})
