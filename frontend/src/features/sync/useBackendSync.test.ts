import { describe, expect, it } from 'vitest'
import { app, expedition } from '../../api/models'
import { isExpeditionExportEnabled } from './useBackendSync'

describe('isExpeditionExportEnabled', () => {
  it('requires activeExpeditionId equality on both plan and sidebarStats branches', () => {
    const completedPlan = expedition.PlanView.createFrom({
      expedition: { id: 1, name: 'Completed Exp', status: 'completed' },
      rows: [],
      modes: [],
    })
    const planningPlan = expedition.PlanView.createFrom({
      expedition: { id: 2, name: 'Planning Exp', status: 'planning' },
      rows: [],
      modes: [],
    })
    const activePlan = expedition.PlanView.createFrom({
      expedition: { id: 2, name: 'Active Exp', status: 'active' },
      rows: [],
      modes: [],
    })
    const departedStats = app.SidebarStats.createFrom({ expeditionId: 1, departed: true })
    const undepartedStats = app.SidebarStats.createFrom({ expeditionId: 2, departed: false })
    const activeDepartedStats = app.SidebarStats.createFrom({ expeditionId: 2, departed: true })

    // Stale completed plan or stale departed stats from expedition 1 must not enable export on expedition 2.
    expect(isExpeditionExportEnabled(2, completedPlan, undepartedStats)).toBe(false)
    expect(isExpeditionExportEnabled(2, planningPlan, departedStats)).toBe(false)
    expect(isExpeditionExportEnabled(0, completedPlan, departedStats)).toBe(false)

    // Matching activeExpeditionId enables export when departed or completed.
    expect(isExpeditionExportEnabled(1, completedPlan, undepartedStats)).toBe(true)
    expect(isExpeditionExportEnabled(2, activePlan, activeDepartedStats)).toBe(true)
  })
})
