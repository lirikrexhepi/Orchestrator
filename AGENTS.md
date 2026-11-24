# Orchestrator Project Notes

## Releases & Deployment
- **Do not build `desktop.exe` locally** to deploy or test changes in the running app.
- The app runs as an **installed release** that auto-updates from GitHub Releases.
- **Always commit and push changes** without stopping to ask — GitHub can revert if needed.
- To ship changes: commit and push to GitHub, then bump the version by pushing an annotated semver tag (`git tag -a vX.Y.Z -m "- Fix ...\n- Add ..."`) or running `scripts/release.ps1 X.Y.Z -Notes @(...)` / triggering [`.github/workflows/release.yml`](.github/workflows/release.yml). Release tags require a short bullet list of fixes/changes, which is published into `latest.json` and shown in the in-app Update Ready popover.

## UI Design Guidelines & Personal Preferences
- **No unnecessary borders, boxy backgrounds, or colored gradients**:
  - Do NOT wrap tabs, toggles, or widgets in pill boxes with borders or background fill cards unless explicitly requested.
  - For segmented toggles, tabs, or selections (like Today / Week / Month): use clean, typography-driven states: pure white (`text-white font-medium`) for active/selected, and muted white-gray (`text-white/40 hover:text-white/70`) for inactive/unselected. No surrounding borders, no box backgrounds.
- **Never introduce unauthorized accent or brand colors**:
  - Do NOT introduce new accent or provider colors (such as random blues, oranges, purples, or greens) without explicit user permission.
  - Maintain a clean monochromatic / subtle grayscale dark aesthetic: pure white, semi-transparent white opacities (`white/90`, `white/60`, `white/40`, `white/15`), and deep neutrals (`#101012`, `#141518`).
- **Restrained, elegant sizing**:
  - Avoid oversized UI elements, huge cards, or bulky grids. Keep sizing compact, refined, and understated.
  - For activity heatmaps: keep commit/activity squares small, dense, and compact with tight gaps (e.g. 8px with 2px gap), showing an extended dense history (>365 days / 53 columns), with clean minimal white/gray indicators (e.g. small subtle text indicating where the year starts).

