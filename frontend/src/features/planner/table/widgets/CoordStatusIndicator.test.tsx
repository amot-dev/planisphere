// @vitest-environment happy-dom
import { act } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { makeRowView } from '../../../../test/fixtures'
import { mountTestRoot } from '../../../../test/render'
import { CoordStatusIndicator } from './CoordStatusIndicator'

describe('CoordStatusIndicator', () => {
  it('makes green journal CoordStatusIndicator clickable with two-line tooltip while leaving EDSM non-clickable', () => {
    const onInspectSystemVisits = vi.fn()
    const journalRowView = makeRowView({
      row: { id: 72, systemName: 'Sol', hasCoords: true, coordsSource: 'journal', x: 0, y: 0, z: 0 },
      number: 1,
      detectedRegion: 'Inner Orion Spur',
      visitCount: 3,
      visitState: 'visited',
    })
    const edsmRowView = makeRowView({
      row: {
        id: 73,
        systemName: 'Beagle Point',
        hasCoords: true,
        coordsSource: 'edsm',
        x: -1111.6,
        y: -134.2,
        z: 65269.8,
      },
      number: 2,
      detectedRegion: 'The Abyss',
      visitCount: 0,
      visitState: 'future',
    })

    const mounted = mountTestRoot(
      <div>
        <CoordStatusIndicator rowView={journalRowView} onInspectVisits={onInspectSystemVisits} />
        <CoordStatusIndicator rowView={edsmRowView} onInspectVisits={onInspectSystemVisits} />
      </div>,
    )

    const journalButton = mounted.container.querySelector(
      'button[aria-label="Search visit history for Sol"]',
    ) as HTMLButtonElement | null
    expect(journalButton).toBeTruthy()
    expect(journalButton?.title).toBe(
      'Inner Orion Spur (0.0, 0.0, 0.0) via Journal (3 Visits)\nClick to search visit history',
    )

    act(() => {
      journalButton?.click()
    })
    expect(onInspectSystemVisits).toHaveBeenCalledTimes(1)
    expect(onInspectSystemVisits).toHaveBeenCalledWith('Sol')

    expect(
      mounted.container.querySelector('button[aria-label="Search visit history for Beagle Point"]'),
    ).toBeNull()
    expect(
      mounted.container.querySelector(
        'span[title="The Abyss (-1111.6, -134.2, 65269.8) via EDSM (Never Visited)"]',
      ),
    ).toBeTruthy()
  })
})
