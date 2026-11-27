// @vitest-environment happy-dom
import { describe, expect, it } from 'vitest'
import { mountTestRoot } from '../../../test/render'
import { WaypointImportSection } from './WaypointImportSection'

describe('WaypointImportSection', () => {
  it('omits format badges, preserves duplicate summary, and suppresses invalid warning while parsing', () => {
    const spanshView = mountTestRoot(
      <WaypointImportSection
        rawImportText={'"System Name"\n"Sol"\n"Sol"\n"Colonia"'}
        parsedImport={{
          format: 'spansh-csv',
          systems: ['Sol', 'Colonia'],
          collapsedDuplicateCount: 1,
          skippedLineCount: 0,
        }}
        loadedFileNote="Loaded from route.csv"
        loadingFile={false}
        disabled={false}
        onChangeText={() => {}}
        onLoadFile={() => {}}
        onClear={() => {}}
      />,
    )

    expect(spanshView.container.textContent).toContain('2 WAYPOINTS READY')
    expect(spanshView.container.textContent).toContain('1 consecutive duplicate collapsed')
    expect(spanshView.container.textContent).toContain('Loaded from route.csv')
    expect(spanshView.container.textContent).toContain('#01 Sol -> #02 Colonia')
    expect(spanshView.container.textContent).not.toContain('SPANSH CSV')
    expect(spanshView.container.textContent).not.toContain('PLAIN LIST')
    spanshView.unmount()

    const parsingView = mountTestRoot(
      <WaypointImportSection
        rawImportText="Sol"
        parsedImport={{
          format: 'empty',
          systems: [],
          collapsedDuplicateCount: 0,
          skippedLineCount: 0,
        }}
        loadedFileNote=""
        loadingFile={false}
        parsing={true}
        disabled={false}
        onChangeText={() => {}}
        onLoadFile={() => {}}
        onClear={() => {}}
      />,
    )

    expect(parsingView.container.textContent).not.toContain('No valid star systems detected')
  })
})
