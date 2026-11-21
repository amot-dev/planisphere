import { OpenURL } from '../../wailsjs/go/app/App'

export {
  AddRow,
  BrowseDBPath,
  BrowseEDDiscoveryDB,
  BrowseJournalDir,
  ClearActiveCarrierRoute,
  CloneExpedition,
  CompleteExpedition,
  CompleteSetup,
  CreateExpedition,
  CreateMode,
  DeleteExpedition,
  DeleteMode,
  DeleteRow,
  GetActiveCarrierRoute,
  GetCarrierDepotInfo,
  GetCarrierLastLocation,
  GetIngestStatus,
  GetModesState,
  GetPlan,
  GetRegionOptions,
  GetRowDropPreviews,
  GetSettings,
  GetSidebarStats,
  GetStartCandidates,
  GetWaypointDetails,
  ImportEDDiscoveryHistory,
  ListExpeditions,
  MoveRow,
  OpenURL,
  OverrideCarrierStopArrival,
  PlotCarrierRoute,
  RecordCarrierJump,
  RefreshGEC,
  ReopenExpedition,
  RescanJournals,
  SaveActiveCarrierRoute,
  SetActiveExpedition,
  SetCommanderEnabled,
  SetDatabasePath,
  SetExternalLinks,
  SetJournalDebounceMs,
  SetJournalDir,
  SetLegMode,
  SetOnlineLookups,
  SetTritiumPricePerTonne,
  SetupConfigureJournals,
  StartExpedition,
  SwitchMode,
  UpdateExpedition,
  UpdateExpeditionCarrierConfig,
  UpdateMode,
  UpdateRow,
} from '../../wailsjs/go/app/App'

/**
 * openExternalUrl launches an HTTP or HTTPS link in the user's system browser
 * via Wails and logs any validation or launch error without throwing.
 */
export function openExternalUrl(targetUrl: string): void {
  OpenURL(targetUrl).catch(console.error)
}

