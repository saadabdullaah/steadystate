# Engineering Log

## 2026-07-13 — Phase 0 foundation

### Done

- Established Windows Git and PowerShell as the primary local workflow.
- Protected private planning material from Git tracking.
- Selected a maintained Gateway API implementation after validating dependency lifecycle.
- Defined pinned tools, cluster profiles, CI gates, and failure diagnostics.

### Blocked

- Full local cluster verification requires upgrading the current Docker Desktop 20.10/cgroup-v1 engine; the Kubernetes 1.36 node image correctly fails the compatibility gate.
- Remote repository governance requires renewed GitHub CLI authentication.

### Next

- Validate the downloaded toolchain.
- Run minimal and standard cluster acceptance tests.
- Enable the final GitHub ruleset after required check names have completed once.
