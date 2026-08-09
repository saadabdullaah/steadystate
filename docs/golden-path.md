# Developer golden path

This runbook is the supported Phase 8 path from an empty checkout to a reviewed,
signed, observable, database-backed service. It does not require hand-written
Kubernetes YAML and does not place the delivery App key on the workstation.

## 1. Prepare the workstation

Install Git, Docker Desktop/Engine, PowerShell, and GitHub CLI. Authenticate the
GitHub CLI to the repository, then build or install `platformctl` and create a
non-secret context:

```powershell
platformctl config init --checkout D:\SteadyState
platformctl context set local --checkout D:\SteadyState --profile full
platformctl context use local
platformctl doctor
platformctl cluster up
```

Use `platformctl config view` to verify the redacted result. The configuration
must never contain a GitHub token, GitHub App key, SOPS identity, registry
credential, kubeconfig content, or Database password.

## 2. Scaffold through review

```powershell
platformctl init xyz --template full-stack --create-team --with-database --plan
platformctl init xyz --template full-stack --create-team --with-database
```

The first command is local and deterministic. The second displays the same diff,
asks for confirmation, and dispatches `platform-change.yml`. The CLI prints the
run URL and returns without waiting for CI or merge. Use
`platformctl request status REQUEST_ID` or `request watch REQUEST_ID` only when
you intentionally want to follow it.

Scaffold PR A may change only the catalog, Team/Database desired-state leaves,
and `services/xyz`. It contains no Application, so a merge cannot deploy an
unpublished image. Review its App author, request/proposal/render metadata,
changed paths, generated tests, VERSION, lockfile, and Dockerfiles before merge.

## 3. Publish and activate

Merging PR A triggers `service-release.yml`. For every changed component it:

1. validates the descriptor and strict VERSION;
2. race-tests Go and tests/builds the pinned React toolchain;
3. builds the `linux/amd64` image and scans it with Trivy;
4. publishes immutable component/version and component/source-SHA tags;
5. generates an SPDX JSON SBOM;
6. signs the digest and attests the SBOM with GitHub OIDC;
7. verifies the exact main-branch workflow identity and issuer; and
8. opens App-authored activation PR B.

The shared `ghcr.io/saadabdullaah/steadystate-services` package must be public
for anonymous pull, signature, and attestation verification. Never add
`latest`, overwrite a tag, or activate a partially published release.

After PR B passes all five required checks, review its exact image tags and
merge it. Argo creates the Applications; SteadyState owns the Rollouts,
Services, route, monitoring, policy, and database binding.

## 4. Verify and operate

```powershell
platformctl team status xyz
platformctl database status xyz
platformctl database backups xyz
platformctl app status xyz
platformctl app status xyz-api
platformctl app rollout xyz-api
platformctl app provenance xyz-api
platformctl app logs xyz-api --historical --since 15m
platformctl app traces xyz-api
platformctl app slo xyz-api
platformctl app policy xyz-api
platformctl app doctor xyz-api
```

The web route serves the embedded React application. `/api/` is a same-origin
proxy to the internal API Service on its declared port 80 (the Service forwards
to container port 8080), so no new CORS or route-path API is required.
The API receives the Database connection only through explicit SecretKeyRefs.
Kyverno admits only the signed/attested images, Argo Rollouts executes the
metric-gated canary, and status reports the active digest and Git revision.

`app doctor` is the first incident command. It checks dependencies in order and
returns sanitized Pass/Warning/Fail/Unknown results with remediation. Use
`app promote` or `app abort` only for a reviewed incident: both require a reason
and exact target confirmation and produce Events plus a local redacted audit
record. A Git recovery change remains required when desired state is unhealthy.

## 5. Retire safely

```powershell
platformctl service retire xyz --team xyz --plan
platformctl service retire xyz --team xyz
```

Approval PR C marks only `xyz` resources Retiring, records one deletion UUID,
and removes their prune protection. Merge it, wait for its exact revision to be
visible in Argo, and then run:

```powershell
platformctl service finalize xyz --team xyz `
  --deletion-request APPROVAL_UUID `
  --approval-revision MERGED_APPROVAL_SHA
```

Finalization PR D removes the catalog entry, leaves, and generated source. After
review and merge, Argo background-prunes the CRs so SteadyState finalizers can
keep backup dependencies available. The platform deletes Applications,
completes the Database final backup, removes the Team namespace, and retains the
external archive. Do not manually remove `Prune=false`, patch finalizers, or use
force deletion to accelerate a normal backup.

## Compatibility

The v0.8 CLI reads v0.7/v0.8 platform resources. Git writes require matching
v0.8 ChangeRequest, catalog, and renderer contracts. A newer unknown schema
fails with an upgrade message. Configuration migration creates a backup; the
CLI never silently auto-updates. Windows and Linux support the full lifecycle.
macOS binaries support configuration, Git/read/diagnostic operations and treat
local kind lifecycle as best-effort.
