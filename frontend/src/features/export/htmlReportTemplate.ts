import { ExpeditionExportData } from './exportTypes'

const HTML_ENTITY_PREFIX = String.fromCharCode(38)

/**
 * Self-contained cockpit and print stylesheet inlined into exported HTML reports.
 */
export const REPORT_STYLESHEET = `
*{box-sizing:border-box;border-radius:0}
body{margin:0;background:#050506;color:#f4ece4;font-family:"Rajdhani","Bahnschrift","DIN Alternate","Segoe UI",sans-serif;line-height:1.4}
.mono{font-family:"JetBrains Mono","Consolas",ui-monospace,monospace;font-variant-numeric:tabular-nums}
.amber{color:#ff7100}.cyan{color:#00d2ff}.emerald{color:#00e5a3}.danger{color:#ff3b2f}.muted{color:#a6907c}.small{font-size:11px}
.report-topbar{display:flex;justify-content:space-between;align-items:center;padding:18px 24px;background:#140e09;border-bottom:2px solid #ff7100}
.eyebrow{font-size:10px;font-weight:700;letter-spacing:0.14em;color:#a6907c;text-transform:uppercase}
.report-title{margin:2px 0 0;font-size:20px;text-transform:uppercase;letter-spacing:0.08em}
.topbar-meta{display:flex;gap:16px;font-size:11px;color:#d6c7b8}
.report-main{max-width:1020px;margin:20px auto;padding:0 16px;display:flex;flex-direction:column;gap:20px}
.card{background:#0b0907;border:1px solid #3d2816;break-inside:avoid}
.block-header{display:flex;justify-content:space-between;align-items:center;padding:14px 18px;background:#140e09;border-bottom:1px solid #3d2816}
.accent-amber{border-top:2px solid #ff7100}.accent-cyan{border-top:2px solid #00d2ff}.accent-emerald{border-top:2px solid #00e5a3}
.accent-left-amber{border-left:2px solid #ff7100}.accent-left-cyan{border-left:2px solid #00d2ff}.accent-left-emerald{border-left:2px solid #00e5a3}.accent-left-danger{border-left:2px solid #ff3b2f}
.accent-top-amber{border-top:2px solid #ff7100}.accent-top-cyan{border-top:2px solid #00d2ff}.accent-top-emerald{border-top:2px solid #00e5a3}.accent-top-danger{border-top:2px solid #ff3b2f}
.block-title-row{display:flex;align-items:center;gap:8px;flex-wrap:wrap}
.block-title{margin:2px 0 0;font-size:15px;text-transform:uppercase;letter-spacing:0.08em}
.badge{font-size:10px;font-weight:700;padding:1px 6px;border:1px solid #3d2816;text-transform:uppercase}
.badge-cyan{border-color:#00d2ff;color:#00d2ff}
.header-dates{text-align:right;font-size:11px}
.strip-bar{display:flex;flex-wrap:wrap;gap:10px;padding:8px 18px;background:#110c08;border-bottom:1px solid #2e1f12;font-size:11px}
.strip-bar-bottom{border-top:1px solid #2e1f12;border-bottom:none}
.sep{color:#3d2816}
.kpi-grid{display:grid;gap:10px;padding:14px 18px}
.kpi-grid-top{padding-bottom:10px}
.kpi-grid-bottom{padding-top:0}
.cols-1{grid-template-columns:1fr}.cols-2{grid-template-columns:repeat(2,1fr)}.cols-3{grid-template-columns:repeat(3,1fr)}.cols-4{grid-template-columns:repeat(4,1fr)}
.kpi-tile{background:#110c08;border:1px solid #2e1f12;padding:10px 12px}
.kpi-label{font-size:10px;font-weight:700;text-transform:uppercase;letter-spacing:0.1em;color:#a6907c}
.kpi-val{font-size:16px;font-weight:700;margin-top:4px}
.kpi-sub{font-size:11px;color:#a6907c;margin-top:3px}
.diff-pill{font-size:10px;color:#00d2ff;margin-top:4px;padding-top:4px;border-top:1px solid #2e1f12}
.card-stack{display:flex;flex-direction:column;gap:10px;padding:0 18px 16px}
.bd-card{background:#110c08;border:1px solid #2e1f12;padding:10px 14px}
.bd-head{display:flex;justify-content:space-between;font-size:12px;font-weight:700;text-transform:uppercase;padding-bottom:6px;margin-bottom:6px;border-bottom:1px solid #2e1f12}
.bd-row{display:flex;justify-content:space-between;font-size:11px;padding:2px 0}
.wp-table{width:100%;border-collapse:collapse;font-size:12px}
.wp-table th{text-align:left;padding:8px 10px;background:#110c08;border-bottom:1px solid #3d2816;font-size:10px;text-transform:uppercase;color:#a6907c}
.wp-table td{padding:7px 10px;border-bottom:1px solid #24180e;vertical-align:top}
.region-row td{background:#140e09;color:#ff7100;font-weight:700;font-size:11px;text-transform:uppercase;letter-spacing:0.12em}
.hl-grid{display:grid;grid-template-columns:repeat(2,1fr);gap:12px;padding:14px 18px}
.hl-card{background:#110c08;border:1px solid #2e1f12;padding:10px 12px}
.hl-entry{padding:6px 0;border-top:1px solid #24180e;margin-top:6px}
.hl-row{display:flex;justify-content:space-between;gap:8px;font-size:12px}
.empty-banner{padding:24px 18px;text-align:center;font-size:11px;color:#a6907c}
.report-footer{text-align:center;padding:16px;font-size:10px;color:#6e5b4b}
@media print{
  body{background:#fff;color:#111}
  .card,.kpi-tile,.bd-card,.hl-card{background:#fff;border-color:#ccc;color:#111}
  .report-topbar,.block-header,.strip-bar,.wp-table th,.region-row td{background:#f5f5f5;color:#111}
  .amber,.cyan,.emerald,.danger,.muted{color:#222}
}
`

/** Escapes HTML special characters for safe text and attribute interpolation. */
export function escapeHtml(rawText: string | number | null | undefined): string {
  const text = String(rawText ?? '')
  return text
    .replaceAll(HTML_ENTITY_PREFIX, `${HTML_ENTITY_PREFIX}amp;`)
    .replaceAll('<', `${HTML_ENTITY_PREFIX}lt;`)
    .replaceAll('>', `${HTML_ENTITY_PREFIX}gt;`)
    .replaceAll('"', `${HTML_ENTITY_PREFIX}quot;`)
    .replaceAll("'", `${HTML_ENTITY_PREFIX}#39;`)
}

/** Wraps rendered cart section markup inside the standalone HTML report shell. */
export function renderHtmlReportDocument(
  exportData: ExpeditionExportData,
  renderedSections: string,
): string {
  const expeditionName = escapeHtml(exportData.plan.expedition.name)
  return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<title>${expeditionName} — Planisphere Expedition Report</title>
<style>${REPORT_STYLESHEET}</style>
</head>
<body>
<header class="report-topbar">
  <div>
    <div class="eyebrow">PLANISPHERE // ASTROMETRIC TELEMETRY REPORT</div>
    <h1 class="report-title">${expeditionName}</h1>
  </div>
  <div class="topbar-meta mono">
    <span>STATUS: ${escapeHtml(exportData.plan.expedition.status.toUpperCase())}</span>
    <span>EXPORTED: ${escapeHtml(exportData.exportedAtGalactic)}</span>
  </div>
</header>
<main class="report-main">
${renderedSections}
</main>
<footer class="report-footer mono">
  PLANISPHERE v${escapeHtml(exportData.appVersion || 'dev')} // EXPORTED ${escapeHtml(exportData.exportedAtGalactic)}
</footer>
</body>
</html>`
}
