import { renderToString } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { SyncStatusButton } from './SyncStatusButton'

describe('SyncStatusButton', () => {
  it('renders READY, BUSY, PAUSING (preserving busy task tooltip), and PAUSED states', () => {
    const readyHtml = renderToString(
      <SyncStatusButton
        activeStatusMessage={null}
        syncPauseState="active"
        hasPendingSyncUpdates={false}
        onToggleSyncPause={() => {}}
      />,
    )
    expect(readyHtml).toContain('READY')
    expect(readyHtml).toContain('aria-pressed="false"')
    expect(readyHtml).toContain('Live sync active. Click to pause.')

    const busyHtml = renderToString(
      <SyncStatusButton
        activeStatusMessage="COMPUTING TELEMETRY"
        syncPauseState="active"
        hasPendingSyncUpdates={false}
        onToggleSyncPause={() => {}}
      />,
    )
    expect(busyHtml).toContain('BUSY')
    expect(busyHtml).toContain('Background task: COMPUTING TELEMETRY. Click to pause once ready.')

    const pausingHtml = renderToString(
      <SyncStatusButton
        activeStatusMessage="COMPUTING TELEMETRY"
        syncPauseState="pausing"
        hasPendingSyncUpdates={false}
        onToggleSyncPause={() => {}}
      />,
    )
    expect(pausingHtml).toContain('PAUSING')
    expect(pausingHtml).toContain('aria-pressed="true"')
    expect(pausingHtml).toContain(
      'Background task: COMPUTING TELEMETRY. Will pause once ready (click to cancel).',
    )

    const pausedPendingHtml = renderToString(
      <SyncStatusButton
        activeStatusMessage={null}
        syncPauseState="paused"
        hasPendingSyncUpdates={true}
        onToggleSyncPause={() => {}}
      />,
    )
    expect(pausedPendingHtml).toContain('PAUSED')
    expect(pausedPendingHtml).toContain('aria-pressed="true"')
    expect(pausedPendingHtml).toContain('bg-ed-amber')
    expect(pausedPendingHtml).toContain(
      'Live sync paused (updates waiting). Click to resume and refresh.',
    )
  })
})
