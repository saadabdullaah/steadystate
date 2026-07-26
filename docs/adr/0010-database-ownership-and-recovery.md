# ADR-0010: Database ownership and declarative recovery

## Status

Accepted for Phase 7.

## Decision

SteadyState exposes a namespaced
`platform.steadystate.dev/v1alpha1 Database` instead of exposing
CloudNativePG or Barman resources to tenants. The operator owns the CNPG
Cluster, Barman ObjectStore, scheduled/final Backups, namespaced credential
copy, monitoring objects, and NetworkPolicies. Argo owns only Team, Database,
and Application desired state.

CloudNativePG `1.30.0` and Barman Cloud Plugin `0.13.0` are installed from
checksum-pinned charts. Their resources are schema-tested unstructured
objects because their current Go modules require Go 1.26 while SteadyState
remains on Go 1.25.12. Exact chart-rendered CRDs are vendored for envtest.

SeaweedFS `4.39` runs outside kind on the exact `steadystate-backup` Docker
network and `steadystate-backup-data` named volume. Its host port is
loopback-only. Container readiness comes from mini's internal master
`/cluster/status` endpoint rather than the authenticated S3 root; the master
port is not published. Stop preserves the volume; purge is explicit.
SeaweedFS is not an officially tested Barman target, so a hosted
provision/WAL/base-backup/delete/restore/checksum compatibility gate is
mandatory and has no silent fallback.

The data graph is full-profile-only. Standard-profile tenant rendering omits
the Database source and removes the sample Application's `databaseRef`, while
the full profile retains Team/Database/Application waves. Cert-manager leader
election is explicitly moved from its chart default in `kube-system` to the
dedicated `cert-manager` Namespace so the platform AppProject does not gain an
unrelated destination.

`ObjectStore.spec.serverName` is absent. Each Database lifetime sends its
UID-derived server name through Barman plugin parameters for writes. Recovery
selects an earlier server through the external-cluster plugin parameter and
writes new backups under the new lifetime's server name, avoiding a non-empty
archive conflict.

Application database bindings are same-namespace and readiness-gated. Six
explicit SecretKeyRefs are injected. Values never enter status, ConfigMaps,
Events, logs, or evidence. The Database controller copies only the two
required S3 keys from the SOPS-bootstrapped platform Secret.

Deletion creates and waits for one deterministic final Backup before removing
the data plane; external objects remain. The final Backup deliberately has no
owner reference to the already-terminating Database and is deleted explicitly
after completion. A failed final backup holds the finalizer.
`steadystate.dev/force-delete=true` is the explicit, data-loss-capable
emergency escape hatch. Team deletion orders Applications, Databases/final
backups, then Namespace.

The controller watches all Backup objects by their referenced CNPG Cluster,
including manually requested backups. It publishes backup-health and
last-success timestamps on its existing metrics endpoint. Database-owned
alerts use those controller-observed metrics instead of deprecated CNPG
plugin status metrics; failure is fast, while stale-success age remains a
25-hour production guard.

## Consequences

- The one-instance sample proves recovery and makes no HA claim.
- Application deletion never deletes its referenced Database.
- Backup durability survives kind destruction but still trusts one host and
  named Docker volume.
- The controller requires Secret write access to create deterministic
  Database-owned credential copies. A path-specific, expiring scanner
  exception records this trust rather than hiding it.
- The checksum-pinned upstream local-path manifest retains its root helper and
  writable filesystem. Path-specific, expiring exceptions apply only to that
  isolated platform add-on; Team workloads receive no policy exemption.
- Recovery is reviewable Git intent, never a patch to generated CNPG objects.
- Hosted objectives are RTO at most 30 minutes and a confirmed archive RPO
  boundary at most five minutes.
