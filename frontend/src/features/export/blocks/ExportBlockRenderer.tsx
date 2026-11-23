import {
  ExpeditionExportData,
  ExportBlockConfig,
  ExportTelemetrySnapshot,
} from '../exportTypes'
import { DetailsBlock } from './DetailsBlock'
import { HighlightsBlock } from './HighlightsBlock'
import { SummaryBlock } from './SummaryBlock'
import { WaypointsBlock } from './WaypointsBlock'

/** Props for the unified ExportBlockRenderer dispatcher component. */
export interface ExportBlockRendererProps {
  exportData: ExpeditionExportData
  config: ExportBlockConfig
  customRangeSnapshot?: ExportTelemetrySnapshot
}

/**
 * ExportBlockRenderer dispatches any ExportBlockConfig to its matching
 * self-contained cockpit card component for both Live Preview and PNG export.
 */
export function ExportBlockRenderer({
  exportData,
  config,
  customRangeSnapshot,
}: ExportBlockRendererProps) {
  switch (config.kind) {
    case 'summary':
      return (
        <SummaryBlock
          exportData={exportData}
          config={config}
          customRangeSnapshot={customRangeSnapshot}
        />
      )
    case 'details':
      return (
        <DetailsBlock
          exportData={exportData}
          config={config}
          customRangeSnapshot={customRangeSnapshot}
        />
      )
    case 'waypoints':
      return <WaypointsBlock exportData={exportData} config={config} />
    case 'highlights':
      return <HighlightsBlock exportData={exportData} config={config} />
  }
}

