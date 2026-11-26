import { OpenURL, SetLiveSyncPaused } from '../../wailsjs/go/app/App'

export {
  AddRow,
  BrowseDBPath,
  BrowseEDDiscoveryDB,
  BrowseJournalDir,
  BrowseWaypointImportFile,
  CancelCarrierRoutePlot,
  ClearActiveCarrierRouteWithUndo,
  CloneExpedition,
  CompleteCarrierRouteArrival,
  CompleteExpedition,
  CompleteSetup,
  CompleteSpanshCarrierJump,
  CreateExpedition,
  CreateExpeditionFromImport,
  CreateExpeditionWithWaypoints,
  CreateMode,
  DeleteExpedition,
  DeleteMode,
  DeleteRow,
  GetExpeditionExportData,
  GetIngestStatus,
  GetModeUsage,
  GetModesState,
  GetPlan,
  GetRegionOptions,
  GetRowDropPreviews,
  GetSettings,
  GetSidebarStats,
  GetStartCandidates,
  GetWaypointDetails,
  ImportEDDiscoveryHistory,
  ListCommanderLoadGamesOnDate,
  ListExpeditions,
  LogManualCarrierJump,
  MergeCommanders,
  MoveRow,
  OpenManualCarrierRoute,
  OpenURL,
  OverrideCarrierStopArrival,
  ParseWaypointImport,
  PickExportSavePath,
  PrepareCarrierRoutePrompt,
  RedoExpedition,
  RefillCarrierDepot,
  RefreshGEC,
  ReopenExpedition,
  RescanJournals,
  ResetCommanderOverrides,
  SaveCarrierManualDraftInputs,
  SearchWaypoints,
  SetActiveExpedition,
  SetCareerBaselineScope,
  SetCommanderEnabled,
  SetDatabasePath,
  SetExpeditionFlightMode,
  SetExternalLinks,
  SetJournalDebounceMs,
  SetJournalDir,
  SetLegMode,
  SetLiveSyncPaused,
  SetOnlineLookups,
  SetTritiumPricePerTonne,
  SetupConfigureJournals,
  SplitCommander,
  StartCarrierRoute,
  StartExpedition,
  StartExpeditionScoped,
  SwitchCarrierRouteToManual,
  UndoExpedition,
  UnmergeCommanderAlias,
  UpdateExpedition,
  UpdateExpeditionCarrierConfig,
  UpdateExpeditionScoped,
  UpdateMode,
  UpdateRow,
  WriteExportOutput,
} from '../../wailsjs/go/app/App'

/**
 * openExternalUrl launches an HTTP or HTTPS link in the user's system browser
 * via Wails and logs any validation or launch error without throwing.
 */
export function openExternalUrl(targetUrl: string): void {
  OpenURL(targetUrl).catch(console.error)
}

/**
 * setLiveSyncPaused pauses or resumes backend live journal tailing and
 * unsolicited background UI notifications when the Wails bridge is available.
 */
export function setLiveSyncPaused(paused: boolean): void {
  if (typeof window === 'undefined' || !window.go?.app?.App?.SetLiveSyncPaused) {
    return
  }
  SetLiveSyncPaused(paused).catch(console.error)
}
