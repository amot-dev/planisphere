import { useEffect, useRef, useState } from 'react'
import { SetExternalLinks } from '../../api/backend'
import { app } from '../../api/models'
import { SettingsCard } from '../../components/ui/SettingsCard'
import {
  normalizeBinaryMode,
  normalizeEdsmMode,
  type BinaryExternalLinkMode,
  type EdsmExternalLinkMode,
} from '../../domain/externalLinks'

interface SegmentedOption<ModeValue extends string> {
  value: ModeValue
  label: string
  title?: string
}

const BINARY_LINK_OPTIONS: ReadonlyArray<SegmentedOption<BinaryExternalLinkMode>> = [
  { value: 'on', label: 'On' },
  { value: 'off', label: 'Off' },
]

const EDSM_LINK_OPTIONS: ReadonlyArray<SegmentedOption<EdsmExternalLinkMode>> = [
  { value: 'on', label: 'On' },
  {
    value: 'if_no_gec',
    label: 'If No GEC',
    title: 'Show EDSM when a waypoint has no GEC entry, or when GEC links are Off',
  },
  { value: 'off', label: 'Off' },
]

function SegmentedLinkRow<ModeValue extends string>({
  serviceLabel,
  selectedValue,
  options,
  onSelect,
}: {
  serviceLabel: string
  selectedValue: ModeValue
  options: ReadonlyArray<SegmentedOption<ModeValue>>
  onSelect: (nextValue: ModeValue) => void
}) {
  return (
    <div className="px-3 py-2 flex items-center justify-between gap-3">
      <span className="font-bold uppercase tracking-wider text-ed-text-main">{serviceLabel}</span>
      <div
        role="group"
        aria-label={`${serviceLabel} external link visibility`}
        className="inline-flex items-center gap-1 shrink-0"
      >
        {options.map((optionItem) => {
          const isSelected = selectedValue === optionItem.value
          return (
            <button
              key={optionItem.value}
              type="button"
              aria-pressed={isSelected}
              title={optionItem.title}
              onClick={() => {
                if (!isSelected) {
                  onSelect(optionItem.value)
                }
              }}
              className={`px-2.5 py-1 text-[10px] font-mono-tabular font-bold uppercase tracking-wider border transition-colors cursor-pointer whitespace-nowrap ${
                isSelected
                  ? 'bg-ed-amber-tint text-ed-amber border-ed-amber'
                  : 'bg-ed-surface text-ed-text-muted border-ed-border-dim hover:text-ed-text-main hover:border-ed-border-strong'
              }`}
            >
              {optionItem.label}
            </button>
          )
        })}
      </div>
    </div>
  )
}

/**
 * ExternalLinksSectionProps defines the current settings and callbacks for
 * configuring waypoint external link buttons inside Terminal Configuration.
 */
export interface ExternalLinksSectionProps {
  settings: app.Settings | null
  onSetError: (errorMessage: string) => void
}

/**
 * ExternalLinksSection renders segmented visibility controls for Inara, EDSM,
 * and EDAstro Galactic Exploration Catalog buttons on expedition waypoints.
 */
export function ExternalLinksSection({
  settings,
  onSetError,
}: ExternalLinksSectionProps) {
  const configuredInara = normalizeBinaryMode(settings?.externalLinkInara)
  const configuredEdsm = normalizeEdsmMode(settings?.externalLinkEdsm)
  const configuredGec = normalizeBinaryMode(settings?.externalLinkGec)

  const [inaraMode, setInaraMode] = useState<BinaryExternalLinkMode>(configuredInara)
  const [edsmMode, setEdsmMode] = useState<EdsmExternalLinkMode>(configuredEdsm)
  const [gecMode, setGecMode] = useState<BinaryExternalLinkMode>(configuredGec)

  const draftModesRef = useRef({
    inara: configuredInara,
    edsm: configuredEdsm,
    gec: configuredGec,
  })
  const requestSequenceRef = useRef(0)

  useEffect(() => {
    draftModesRef.current = {
      inara: configuredInara,
      edsm: configuredEdsm,
      gec: configuredGec,
    }
    setInaraMode(configuredInara)
    setEdsmMode(configuredEdsm)
    setGecMode(configuredGec)
  }, [configuredInara, configuredEdsm, configuredGec])

  async function commitExternalLinks(
    nextInara: BinaryExternalLinkMode,
    nextEdsm: EdsmExternalLinkMode,
    nextGec: BinaryExternalLinkMode,
  ) {
    onSetError('')
    draftModesRef.current = { inara: nextInara, edsm: nextEdsm, gec: nextGec }
    setInaraMode(nextInara)
    setEdsmMode(nextEdsm)
    setGecMode(nextGec)

    const currentSequence = ++requestSequenceRef.current
    try {
      const updatedSettings = await SetExternalLinks(nextInara, nextEdsm, nextGec)
      if (currentSequence === requestSequenceRef.current) {
        const savedInara = normalizeBinaryMode(updatedSettings.externalLinkInara)
        const savedEdsm = normalizeEdsmMode(updatedSettings.externalLinkEdsm)
        const savedGec = normalizeBinaryMode(updatedSettings.externalLinkGec)
        draftModesRef.current = { inara: savedInara, edsm: savedEdsm, gec: savedGec }
        setInaraMode(savedInara)
        setEdsmMode(savedEdsm)
        setGecMode(savedGec)
      }
    } catch (saveError) {
      if (currentSequence === requestSequenceRef.current) {
        draftModesRef.current = {
          inara: configuredInara,
          edsm: configuredEdsm,
          gec: configuredGec,
        }
        setInaraMode(configuredInara)
        setEdsmMode(configuredEdsm)
        setGecMode(configuredGec)
      }
      onSetError(String(saveError))
    }
  }

  return (
    <SettingsCard title="External Links">
      <div className="divide-y divide-ed-border-subtle border border-ed-border bg-ed-void">
        <SegmentedLinkRow
          serviceLabel="Inara"
          selectedValue={inaraMode}
          options={BINARY_LINK_OPTIONS}
          onSelect={(nextInara) =>
            void commitExternalLinks(
              nextInara,
              draftModesRef.current.edsm,
              draftModesRef.current.gec,
            )
          }
        />
        <SegmentedLinkRow
          serviceLabel="EDSM"
          selectedValue={edsmMode}
          options={EDSM_LINK_OPTIONS}
          onSelect={(nextEdsm) =>
            void commitExternalLinks(
              draftModesRef.current.inara,
              nextEdsm,
              draftModesRef.current.gec,
            )
          }
        />
        <SegmentedLinkRow
          serviceLabel="GEC"
          selectedValue={gecMode}
          options={BINARY_LINK_OPTIONS}
          onSelect={(nextGec) =>
            void commitExternalLinks(
              draftModesRef.current.inara,
              draftModesRef.current.edsm,
              nextGec,
            )
          }
        />
      </div>
    </SettingsCard>
  )
}

