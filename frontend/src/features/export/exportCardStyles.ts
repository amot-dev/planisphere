/** Fixed logical width in CSS pixels for every self-contained export card (2x DPI = 1920px PNG). */
export const EXPORT_CARD_WIDTH = 960

/**
 * Self-contained cockpit stylesheet shared by the Live Preview and SVG foreignObject
 * 2x PNG rasterizer. Every text color is explicitly declared on .ed-exp-root and
 * headings so isolated SVG foreignObject documents never inherit default browser black.
 */
export const EXPORT_CARD_CSS = `
.ed-exp-root, .ed-exp-root * {
  box-sizing: border-box;
  border-radius: 0;
}
.ed-exp-root {
  width: 960px;
  background-color: #0b0907;
  color: #f4ece4;
  border: 1px solid #3d2816;
  font-family: "Rajdhani", "Bahnschrift", "DIN Alternate", "Inter", "Segoe UI", -apple-system, sans-serif;
  line-height: 1.4;
  letter-spacing: 0.015em;
}
.ed-exp-mono {
  font-family: "JetBrains Mono", "Fira Code", "Cascadia Mono", "Consolas", ui-monospace, monospace;
  font-variant-numeric: tabular-nums;
}
.ed-exp-amber { color: #ff7100; }
.ed-exp-cyan { color: #00d2ff; }
.ed-exp-emerald { color: #00e5a3; }
.ed-exp-danger { color: #ff3b2f; }
.ed-exp-main { color: #f4ece4; }
.ed-exp-muted { color: #a6907c; }
.ed-exp-dim { color: #6e5b4b; }
.ed-exp-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  padding: 14px 18px;
  background-color: #140e09;
  border-bottom: 1px solid #3d2816;
  border-top: 2px solid #ff7100;
}
.ed-exp-header-cyan { border-top-color: #00d2ff; }
.ed-exp-header-emerald { border-top-color: #00e5a3; }
.ed-exp-eyebrow {
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.14em;
  color: #a6907c;
}
.ed-exp-title-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 2px;
}
.ed-exp-title {
  margin: 0;
  font-size: 17px;
  font-weight: 800;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: #f4ece4;
}
.ed-exp-badge {
  display: inline-block;
  font-size: 10px;
  font-weight: 700;
  padding: 2px 7px;
  border: 1px solid #3d2816;
  background-color: #110c08;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: #f4ece4;
}
.ed-exp-badge-cyan {
  border-color: #00d2ff;
  color: #00d2ff;
  background-color: #08151c;
}
.ed-exp-dates {
  text-align: right;
  font-size: 11px;
  color: #d6c7b8;
  flex-shrink: 0;
}
.ed-exp-strip {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
  padding: 8px 18px;
  background-color: #110c08;
  border-bottom: 1px solid #2e1f12;
  font-size: 11px;
  color: #d6c7b8;
}
.ed-exp-sep { color: #3d2816; }
.ed-exp-grid {
  display: grid;
  gap: 10px;
  padding: 14px 18px;
}
.ed-exp-cols-1 { grid-template-columns: 1fr; }
.ed-exp-cols-2 { grid-template-columns: repeat(2, 1fr); }
.ed-exp-cols-3 { grid-template-columns: repeat(3, 1fr); }
.ed-exp-cols-4 { grid-template-columns: repeat(4, 1fr); }
.ed-exp-tile {
  background-color: #110c08;
  border: 1px solid #2e1f12;
  padding: 10px 12px;
}
.ed-exp-left-amber { border-left: 2px solid #ff7100; }
.ed-exp-left-cyan { border-left: 2px solid #00d2ff; }
.ed-exp-left-emerald { border-left: 2px solid #00e5a3; }
.ed-exp-left-danger { border-left: 2px solid #ff3b2f; }
.ed-exp-top-amber { border-top: 2px solid #ff7100; }
.ed-exp-top-cyan { border-top: 2px solid #00d2ff; }
.ed-exp-top-emerald { border-top: 2px solid #00e5a3; }
.ed-exp-top-danger { border-top: 2px solid #ff3b2f; }
.ed-exp-kpi-label {
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: #a6907c;
}
.ed-exp-kpi-value {
  font-size: 16px;
  font-weight: 700;
  margin-top: 4px;
  color: #f4ece4;
}
.ed-exp-kpi-sub {
  font-size: 11px;
  color: #a6907c;
  margin-top: 3px;
}
.ed-exp-diff {
  font-size: 10px;
  color: #00d2ff;
  margin-top: 6px;
  padding-top: 5px;
  border-top: 1px solid #2e1f12;
}
.ed-exp-stack {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 0 18px 14px;
}
.ed-exp-stack-only {
  padding-top: 14px;
}
.ed-exp-bd-card {
  background-color: #110c08;
  border: 1px solid #2e1f12;
  padding: 10px 14px;
}
.ed-exp-bd-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 12px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: #f4ece4;
  padding-bottom: 6px;
  margin-bottom: 6px;
  border-bottom: 1px solid #2e1f12;
}
.ed-exp-bd-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 11px;
  padding: 2px 0;
  color: #d6c7b8;
}
.ed-exp-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 12px;
  color: #f4ece4;
}
.ed-exp-table th {
  text-align: left;
  padding: 8px 10px;
  background-color: #110c08;
  border-bottom: 1px solid #3d2816;
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: #a6907c;
}
.ed-exp-table td {
  padding: 7px 10px;
  border-bottom: 1px solid #24180e;
  vertical-align: top;
  color: #f4ece4;
}
.ed-exp-region-row td {
  background-color: #140e09;
  color: #ff7100;
  font-weight: 700;
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.12em;
}
.ed-exp-hl-entry {
  padding: 6px 0;
  border-top: 1px solid #24180e;
  margin-top: 6px;
}
.ed-exp-hl-row {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  gap: 8px;
  font-size: 12px;
}
.ed-exp-hl-hero {
  font-size: 18px;
  font-weight: 700;
  margin-top: 4px;
}
.ed-exp-empty {
  padding: 28px 18px;
  text-align: center;
  font-size: 12px;
  color: #a6907c;
  text-transform: uppercase;
  letter-spacing: 0.08em;
}
.ed-exp-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 8px 18px;
  background-color: #090705;
  border-top: 1px solid #24180e;
  font-size: 10px;
  color: #6e5b4b;
  text-transform: uppercase;
  letter-spacing: 0.1em;
}
`

