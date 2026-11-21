import type { SyncPauseState } from '../../features/sync/useSyncPause'

/**
 * SyncStatusButtonProps defines the live task status, pause lifecycle state,
 * pending update flag, and click handler for the top-bar sync toggle button.
 */
export interface SyncStatusButtonProps {
  activeStatusMessage: string | null
  syncPauseState: SyncPauseState
  hasPendingSyncUpdates: boolean
  onToggleSyncPause: () => void
}

interface StatusPresentation {
  label: string
  tooltip: string
  buttonClasses: string
  indicatorClasses: string
}

function resolveStatusPresentation(
  activeStatusMessage: string | null,
  syncPauseState: SyncPauseState,
  hasPendingSyncUpdates: boolean,
): StatusPresentation {
  if (syncPauseState === 'paused') {
    return {
      label: 'PAUSED',
      tooltip: hasPendingSyncUpdates
        ? 'Live sync paused (updates waiting). Click to resume and refresh.'
        : 'Live sync paused. Click to resume.',
      buttonClasses:
        'bg-ed-header border-ed-border-strong text-ed-text-muted hover:border-ed-amber hover:text-ed-text-main',
      indicatorClasses: hasPendingSyncUpdates ? 'bg-ed-amber' : 'bg-ed-text-dim',
    }
  }

  if (syncPauseState === 'pausing') {
    const taskDescription = activeStatusMessage ?? 'IN PROGRESS'
    return {
      label: 'PAUSING',
      tooltip: `Background task: ${taskDescription}. Will pause once ready (click to cancel).`,
      buttonClasses: 'bg-ed-amber-tint border-ed-amber text-ed-amber',
      indicatorClasses: 'bg-ed-amber animate-ping',
    }
  }

  if (activeStatusMessage) {
    return {
      label: 'BUSY',
      tooltip: `Background task: ${activeStatusMessage}. Click to pause once ready.`,
      buttonClasses: 'bg-ed-amber-tint border-ed-amber text-ed-amber',
      indicatorClasses: 'bg-ed-amber animate-ping',
    }
  }

  return {
    label: 'READY',
    tooltip: 'Live sync active. Click to pause.',
    buttonClasses: 'bg-ed-cyan-tint border-ed-cyan-border text-ed-cyan hover:border-ed-cyan',
    indicatorClasses: 'bg-ed-cyan',
  }
}

/**
 * SyncStatusButton renders the interactive cockpit sync readout that doubles
 * as a manual pause/resume toggle for live frontend backend synchronization.
 */
export function SyncStatusButton({
  activeStatusMessage,
  syncPauseState,
  hasPendingSyncUpdates,
  onToggleSyncPause,
}: SyncStatusButtonProps) {
  const { label, tooltip, buttonClasses, indicatorClasses } = resolveStatusPresentation(
    activeStatusMessage,
    syncPauseState,
    hasPendingSyncUpdates,
  )

  return (
    <button
      type="button"
      onClick={onToggleSyncPause}
      aria-pressed={syncPauseState !== 'active'}
      title={tooltip}
      className={`h-[30px] flex items-center gap-2 px-2.5 border font-mono-tabular text-[11px] uppercase tracking-wider transition-colors cursor-pointer ${buttonClasses}`}
    >
      <span className={`w-2 h-2 ${indicatorClasses}`} />
      <span className="font-bold">{label}</span>
    </button>
  )
}

