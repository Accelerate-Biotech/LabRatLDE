---
protocol: qpcr-ddct
version: 3
owner: molecular-biology
instrument_kind: qpcr
reference_gene: GAPDH
targets: [IL6, TNF]
params:
  cycles: 40
  anneal_temp_c: 60
  reaction_volume_ul: 20
acceptance:
  ntc_cq_min: 35
  efficiency_range: [0.9, 1.1]
---

# Relative quantification by ΔΔCt

## Materials
- SYBR Green master mix (record lot as a `reagent` lineage node)
- Primer pairs for targets and reference gene
- cDNA samples (each a `sample` lineage node)

## Procedure
1. Thaw master mix on ice, protected from light.
2. Prepare a master mix for *n* + 10% reactions.
3. Plate triplicates per sample per gene; include NTC and no-RT controls.
4. Seal, spin 1 min at 1000 × g, load instrument.
5. Run: 95 °C 2 min; 40 × (95 °C 15 s, 60 °C 60 s); melt curve.

## Acceptance
- NTC Cq ≥ 35 (or undetermined).
- Single melt peak per primer pair.
- Efficiency between 90% and 110%.
