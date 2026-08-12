# SteadyState Portal

The SteadyState portal is a polished local-owner view over the same contracts
used by `platformctl`. It is embedded in the signed CLI binary, listens only on
`127.0.0.1`, and adds no Pod, database, controller, or remote service.

## Start and stop

```powershell
platformctl config init --checkout . --profile full
platformctl platform up
platformctl portal
```

Use `platformctl portal --port 9088` for a fixed free loopback port or
`--no-open` when another process will open the printed one-time URL. Press
Ctrl+C to stop only the portal. The platform continues running. Use
`platformctl platform verify` for a bounded readiness check and
`platformctl platform down` for safe teardown that preserves external backup
data.

The browser receives a host-only, HttpOnly, SameSite=Strict session after it
consumes the 60-second one-time launch token. Refreshing a clean portal URL is
safe for eight hours while that process remains alive. Restart the portal if
the session expires.

## Navigate and inspect

- **Overview** summarizes dependencies, catalog counts, health, progress, and recent operational truth.
- **Teams** shows ownership, quota boundary, applications, databases, and services.
- **Services** connects a golden template and source path to its signed components and lifecycle.
- **Application** presents conditions, active provenance, canary state, SLOs, logs, traces, policy, and diagnosis.
- **Database** presents readiness, storage, the connection Secret name only, archive identity, backups, and recovery.
- **Requests** follows the typed GitHub broker and pull request boundary.
- **Readiness** reports copyable remediation without changing infrastructure.

Use Ctrl/Cmd+K to focus navigation search. Theme follows the system by default
and can be fixed to light or dark. Status text and icons accompany color,
keyboard focus stays visible, and reduced-motion preferences are respected.

## Make a normal change

Open **Changes**, choose a typed operation, and enter only contract fields. The
backend refreshes `origin/main`, constructs the existing `ChangeRequest`, and
renders the exact allowlisted diff. Review its operation, base SHA, proposal
digest, render digest, paths, and warnings. Submission re-renders and rejects
stale or changed plans before dispatching the default-branch broker. The
GitHub App—not the browser—creates the branch and pull request.

Plans expire after ten minutes and are single-use. The browser never submits
raw YAML, source files, paths, workflow names, actor identity, shell text, or
Base64 proposals.

## Create and deliver a service

`service.scaffold` explains the reviewed path: scaffold PR A creates catalog
state and generated source without a live Application; service release then
tests, scans, publishes, signs, and SPDX-attests the image and opens activation
PR B. Merging activation delivers through Argo, Kyverno, Rollouts, and the
SteadyState operator. Arbitrary source and templates are not accepted.

## Retire, restore, and break glass

Delete and finalize are separate reviewed changes. Finalization remains locked
until the approval revision is merged and visible in Argo. Database and Team
finalizers complete the final backup and ordered cleanup. Force proposals need
the explicit data-loss contract; the portal never removes finalizers.

Database restore always creates a new Database lifetime while reading the
selected prior archive. Promote and abort appear only for a current canary.
They require a human reason and exact name, bind to Rollout UID/resource
version, and write Kubernetes Events plus a redacted local audit record. A
recovery Git change may still be required.

## Trust boundary

The runtime is self-hosted. GitHub remains the reviewed change ledger, image
registry, workflow broker, and signing identity. The browser receives no
GitHub token, App key, kubeconfig, SOPS identity, or Kubernetes Secret.
`platformctl` retains the configured cluster-admin context, so the host account
and browser process are trusted. This is not a multi-user authorization service.
