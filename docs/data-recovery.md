# Data and recovery runbook

Phase 7 is a local/full-profile recovery system. It proves that a kind cluster
can be destroyed and rebuilt while PostgreSQL backups survive in an exact
host-side SeaweedFS named volume. It is not a multi-host or high-availability
database service.

## Start and verify the data profile

The ignored `.artifacts/secrets/steadystate.agekey` must contain the age
identity matching the committed SOPS recipient. Hosted runs use the
`SOPS_AGE_KEY` repository secret.

```powershell
.\scripts\dev.ps1 tools
.\scripts\dev.ps1 bootstrap -Profile full
.\scripts\dev.ps1 start-backup-store
.\scripts\dev.ps1 build-images
.\scripts\dev.ps1 load-images
.\scripts\dev.ps1 deploy-gitops -Profile full
.\scripts\dev.ps1 verify-data
```

Verify the declared resources without printing Secret data:

```powershell
kubectl get database -n team-payments
kubectl get cluster,scheduledbackup,backup,objectstore -n team-payments
kubectl get application -n team-payments demo
Invoke-WebRequest http://127.0.0.1:8080/orders -Headers @{Host='demo.team-payments.steadystate.localtest.me'}
```

`Database.status.connectionSecretName` is a reference, not a credential. Do
not add Secret values, database URIs, SQL values, or S3 keys to diagnostics or
evidence.

The backup-store lifecycle declares health from SeaweedFS mini's internal
master endpoint at `127.0.0.1:9333/cluster/status`. The authenticated S3 root
on port `8333` is intentionally not used as a health probe. Only port `8333`
is published, and it remains bound to host loopback.

## Normal backup and restore

Each Database lifetime writes to the UID-derived
`status.backupServerName`. To restore, create a new Database whose immutable
`spec.recovery.sourceServerName` is the prior value. An optional
`targetTime` must be RFC3339 UTC. Do not patch the generated CNPG Cluster or
set `ObjectStore.spec.serverName`.

```yaml
spec:
  backups:
    enabled: true
    schedule: "0 0 2 * * *"
    retention: 7d
  recovery:
    sourceServerName: orders-01234567-89a
    targetTime: "2026-07-19T12:00:00Z"
```

The restored Database receives a new UID-derived write archive. This prevents
Barman from treating an old non-empty archive as the destination for a new
cluster lifetime.

## Whole-cluster drill

The unattended acceptance runner seeds 100 orders, confirms a base backup and
WAL switch, starts the RTO clock immediately before kind deletion, commits
recovery intent to an ephemeral Git branch, rebuilds the full profile, and
requires an exact canonical checksum.

```powershell
.\scripts\dev.ps1 phase7-acceptance -Phase7AcceptanceStage Prepare
.\scripts\dev.ps1 phase7-acceptance -Phase7AcceptanceStage Test
.\scripts\dev.ps1 phase7-acceptance -Phase7AcceptanceStage Finalize
```

Those stages require the workflow-created GitHub App token and ephemeral
branch; the hosted `Phase 7 acceptance` workflow is the supported unattended
entry point. Its gates are RTO at most 30 minutes, an archive boundary no more
than five minutes before failure, exact order checksum, a firing-and-clearing
backup-freshness alert, and retained external objects after final deletion.
The retirement stage first waits for the tenant Argo Application's desired,
compared, and successful-operation revisions to match the exact commit that
removes Database intent. Tenant pruning remains disabled, so the Application
is expected to remain OutOfSync until the drill then requests finalizer-driven
deletion. This prevents an older desired revision from recreating the Database.

Exact-main run `30910727236` passed this complete path with 100 acknowledged
orders, matching source/restored SHA-256 checksums, a 12.58-minute RTO, a
zero-minute measured archive boundary, ten WAL objects, and 33 retained
objects after the final backup and Database deletion.

## Safe stop, cleanup, and emergencies

This preserves backup data:

```powershell
.\scripts\dev.ps1 undeploy-gitops
.\scripts\dev.ps1 destroy
.\scripts\dev.ps1 stop-backup-store
```

Only an explicit purge removes the exact named backup volume:

```powershell
.\scripts\backup-store.ps1 -Action Stop -PurgeData
```

Normal Database deletion waits for a final Barman Backup. When that backup
fails, repair SeaweedFS/network/credentials and allow reconciliation to
continue. The emergency escape hatch:

```powershell
kubectl annotate database -n team-payments orders steadystate.dev/force-delete=true --overwrite
```

accepts possible data loss, skips the final-backup gate, and is never added
automatically. External objects already written to SeaweedFS are retained.
