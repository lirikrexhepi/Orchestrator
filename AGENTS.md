# Orchestrator Project Notes

## Releases & Deployment
- **Do not build `desktop.exe` locally** to deploy or test changes in the running app.
- The app runs as an **installed release** that auto-updates from GitHub Releases.
- **Always commit and push changes** without stopping to ask — GitHub can revert if needed.
- To ship changes: commit and push to GitHub, then bump the version by pushing a semver tag (`vX.Y.Z`) or triggering [`.github/workflows/release.yml`](.github/workflows/release.yml), which builds the Windows NSIS installer and publishes the GitHub Release + `latest.json`.
