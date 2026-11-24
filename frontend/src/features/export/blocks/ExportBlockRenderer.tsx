import { memo } from 'react'
import {
  ExpeditionExportData,
  ExportBlockConfig,
} from '../exportTypes'
import { DetailsBlock } from './DetailsBlock'
import { HighlightsBlock } from './HighlightsBlock'
import { SummaryBlock } from './SummaryBlock'
import { WaypointsBlock } from './WaypointsBlock'

/** Props for the unified ExportBlockRenderer dispatcher component. */
export interface ExportBlockRendererProps {
  exportData: ExpeditionExportData
  config: ExportBlockConfig
}

/**
 * ExportBlockRenderer dispatches any ExportBlockConfig to its matching
 * self-contained cockpit card component for both Live Preview and PNG export.
 */
export const ExportBlockRenderer = memo(function ExportBlockRenderer({
  exportData,
  config,
}: ExportBlockRendererProps) {
  switch (config.kind) {
    case 'summary':
      return <SummaryBlock exportData={exportData} config={config} />
    case 'details':
      return <DetailsBlock exportData={exportData} config={config} />
    case 'waypoints':
      return <WaypointsBlock exportData={exportData} config={config} />
    case 'highlights':
      return <HighlightsBlock exportData={exportData} config={config} />
  }
})
