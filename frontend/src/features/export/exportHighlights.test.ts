import { describe, expect, it, vi } from 'vitest'
import { exporter } from '../../api/models'
import {
  buildCanonicalJsonSnapshot,
  computeAllHighlightLeaderboards,
  selectHighlightCards,
} from './exportHighlights'
import { buildSampleExportData } from './exportTestFixtures'

describe('exportHighlights', () => {
  it('ranks visited legs including IN SYSTEM while excluding unvisited in-progress legs', () => {
    const data = buildSampleExportData()
    const fixedNowMs = Date.parse('2026-04-05T12:00:00Z')
    const leaderboards = computeAllHighlightLeaderboards(data, { nowMs: fixedNowMs })

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
    const data = buildSampleExportData()
    // Zero out all death losses to confirm costliestMistake hides itself cleanly.
    data.rowDetails[1].realization.cartoLostDeath = 0
    data.rowDetails[1].realization.organicLostDeath = 0

    const boards = computeAllHighlightLeaderboards(data)
    const top1 = selectHighlightCards(boards, ['mostLucrative', 'costliestMistake'], 1)
    expect(top1).toHaveLength(1)
    expect(top1[0].entries).toHaveLength(1)

    const top3 = selectHighlightCards(boards, ['mostLucrative'], 3)
    expect(top3[0].entries).toHaveLength(3)
  })

  it('always embeds full ordered highlight rankings in canonical JSON snapshots', () => {
    const data = buildSampleExportData()
    const parsed = JSON.parse(buildCanonicalJsonSnapshot(data))
    expect(parsed.schemaVersion).toBe(1)
    expect(Array.isArray(parsed.highlightsFullOrder)).toBe(true)
    const longest = parsed.highlightsFullOrder.find(
      (board: { cardId: string }) => board.cardId === 'longestDistance',
    )
    expect(longest.entries).toHaveLength(3)
  })

  it('filters by cardIds and slices to maxDepth before formatting, and reuses Go highlightsFullOrder when toggles use defaults', () => {
    const baseData = buildSampleExportData()
    const fixedNowMs = Date.parse('2026-04-05T12:00:00Z')

    const toLocaleSpy = vi.spyOn(Number.prototype, 'toLocaleString')
    let scopedCalls = 0
    let fullCalls = 0
    try {
      const scopedBoards = computeAllHighlightLeaderboards(baseData, {
        nowMs: fixedNowMs,
        cardIds: ['mostLucrative'],
        maxDepth: 1,
      })
      scopedCalls = toLocaleSpy.mock.calls.length
      expect(scopedBoards).toHaveLength(1)
      expect(scopedBoards[0].cardId).toBe('mostLucrative')
      expect(scopedBoards[0].entries).toHaveLength(1)
      // Only winner row (1 primary + 2 secondary credit values = 3 format calls) should be formatted.
      expect(scopedCalls).toBe(3)

      toLocaleSpy.mockClear()
      computeAllHighlightLeaderboards(baseData, { nowMs: fixedNowMs })
      fullCalls = toLocaleSpy.mock.calls.length
      expect(fullCalls).toBeGreaterThan(scopedCalls * 5)
    } finally {
      toLocaleSpy.mockRestore()
    }

    // Hydrate through Wails exporter.ExpeditionExport with Go-populated highlightsFullOrder.
    const fullPrecomputed = computeAllHighlightLeaderboards(baseData, { nowMs: fixedNowMs })
    const hydratedPayload = exporter.ExpeditionExport.createFrom({
      ...baseData,
      highlightsFullOrder: fullPrecomputed.map((board) =>
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

    // Default 'estimated' toggle reuses Go highlightsFullOrder directly and slices to maxDepth=1.
    const reusedDefault = computeAllHighlightLeaderboards(hydratedPayload, {
      lucrativeMetric: 'estimated',
      distanceMetric: 'total',
      cardIds: ['mostLucrative', 'mostJumps'],
      maxDepth: 1,
    })
    expect(reusedDefault).toHaveLength(2)
    expect(reusedDefault[0].entries).toHaveLength(1)
    expect(reusedDefault[0].entries[0].formattedPrimary).toBe('105,000,000 Cr [GO]')

    // Switching lucrativeMetric to 'realized' recomputes mostLucrative in TS (95,000,000 Cr) while reusing mostJumps.
    const recomputedRealized = computeAllHighlightLeaderboards(hydratedPayload, {
      lucrativeMetric: 'realized',
      distanceMetric: 'total',
      cardIds: ['mostLucrative', 'mostJumps'],
      maxDepth: 1,
    })
    expect(recomputedRealized[0].entries[0].formattedPrimary).toBe('95,000,000 Cr')
    expect(recomputedRealized[1].entries[0].formattedPrimary).toBe('50 jumps')

    // Switching distanceMetric to 'ship' recomputes longestDistance using ship distance (Leg #2 has 1,000 Ly ship vs 1,200 Ly total).
    const shipDistanceBoards = computeAllHighlightLeaderboards(hydratedPayload, {
      distanceMetric: 'ship',
      cardIds: ['longestDistance'],
      maxDepth: 2,
    })
    expect(shipDistanceBoards[0].entries[1].primaryValue).toBe(1000)
  })
})
