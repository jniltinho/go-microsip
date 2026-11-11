## New major finding

**MAJOR — The CGO-disabled compile scenario still includes the GUI.**  
**Location:** `change/specs/modular-core/spec.md:16–18`; `change/design.md` D1–D2; `change/tasks.md` 3.1.

**Problem:** D2 defines `internal/gui` as a front end, so the spec requires it to compile with C interoperability disabled. However, D1 permits Wails’s Linux native dependency, while task 3.1 checks CGO-disabled compilation only for core, CLI, MCP and control. The acceptance contracts therefore differ: completing the scheduled checks does not establish the specification’s broader guarantee.

**Concrete fix:** Limit the CGO-disabled compilation scenario to core, CLI, MCP and control, matching D1 and task 3.1. Retain the project-source prohibition outside the audio adapter and validate complete application artifacts with the selected per-OS build settings.

No other new blocking or major problems identified. Library capabilities, licences and distribution compatibility remain **unverified**, with feasibility appropriately gated by G0 rather than treated as established defects.

Not checked: implementation, builds or external sources; audio and packaging feasibility remain pending G0 evidence.

APPROVE WITH CHANGES