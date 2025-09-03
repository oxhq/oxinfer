Oxinfer Working Directory (.oxinfer)
-----------------------------------

This hidden folder stores local, generated artifacts so they don’t clutter the repo:

- performance_reports/: JSON reports from performance validation runs
- cache/v1/: file indexing cache (per‑project key)

Notes
- The folder is git‑ignored by default; only this README is tracked.
- You can change the cache location via either:
  - CLI flag: --cache-dir <absolute path>
  - Env var: OXINFER_CACHE_DIR
  The CLI flag takes precedence and is forwarded downstream.
- It is safe to delete .oxinfer to clear caches and reports (they will be recreated).

