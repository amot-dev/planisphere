import { describe, expect, it } from 'vitest'
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
})

