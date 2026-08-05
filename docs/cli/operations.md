# platformctl operations

## Verify a release

Download the archive, `checksums.txt`, and
`checksums.txt.sigstore.json` from the draft or published GitHub release.
Verify the archive checksum first. Then verify the keyless checksum signature
against the exact tagged release workflow and GitHub OIDC issuer:

```powershell
cosign verify-blob `
  --bundle checksums.txt.sigstore.json `
  --certificate-identity "https://github.com/saadabdullaah/steadystate/.github/workflows/platformctl-release.yml@refs/tags/v0.8.0" `
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" `
  checksums.txt

gh attestation verify .\platformctl_0.8.0_windows_amd64.zip `
  --repo saadabdullaah/steadystate
```

Each archive also has an SPDX JSON SBOM. The GitHub build-provenance
attestation covers all six archives, their SBOMs, and the checksum manifest.

## Upgrade and rollback

There is no silent auto-update. Download and verify the requested version,
retain the previous binary, and replace it only after `platformctl version`
and `platformctl doctor` succeed. To roll back, restore the retained binary
and its matching configuration backup.

The v0.8 CLI reads the v0.7 and v0.8 `v1alpha1` platform resources. Git writes
require the v0.8 ChangeRequest, TenantCatalog, and renderer contracts. Unknown
newer schemas fail with an upgrade message. Configuration writes create a
`.bak` file and never silently rewrite an unknown schema.

## Diagnostics

`platformctl app doctor NAME` reports nine dependency-ordered checks:

1. context, API access, and Team ownership;
2. Git desired, Argo synchronized, and active revisions;
3. Application generation, status, and provenance;
4. Kyverno admission and PolicyReports;
5. Deployment or Rollout and AnalysisRuns;
6. Gateway and HTTPRoute acceptance;
7. Prometheus, SLO, and alerts;
8. Loki logs and Tempo traces;
9. Database readiness and backups.

Every result is `Pass`, `Warning`, `Fail`, or `Unknown` and includes sanitized
evidence references and a concrete remediation. A confirmed failure exits
with code 5. An unavailable dependency is reported separately as `Unknown`.

## Break glass

Promotion and abortion are direct Rollout actions and are not the normal
delivery path. Both require a human reason and exact Application-name
confirmation; there is no bare `--yes` bypass.

```powershell
platformctl app promote demo `
  --reason "approved incident mitigation" `
  --confirm demo

platformctl app abort demo `
  --reason "candidate error budget exhausted" `
  --confirm demo
```

The CLI refuses non-canary, stable, already-aborted, spec-paused, or stale
Rollouts. It creates a private local JSON audit record before mutation, uses a
resource-version JSON-patch test, and emits attempted/completed/failed
Kubernetes Events. These laptop-grade records are useful operational evidence
but are not an immutable enterprise audit log. A recovery Git pull request may
still be required after either action.
