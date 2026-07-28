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

Backup evidence inventories logical object keys through mini's container-local
filer HTTP API. It never searches `/data` for filenames because SeaweedFS
stores filer metadata and object chunks inside LevelDB and volume files.
Assertions are scoped to the Database lifetime's UID-derived Barman server
prefix and require both base-backup and WAL keys. The filer port remains
unpublished, and this read-only inventory uses no S3 credential.

The focused foundation gate keeps Prometheus and Kyverno enabled but omits
Loki, Tempo, Alloy, and OTel, which Phase 5 acceptance validates separately.
This reserves the hosted runner for the storage controllers, PostgreSQL, and
external backup service without weakening admission enforcement. Exact kind
container OOM state, working set, and Docker disk use are captured if the
Kubernetes API becomes unavailable.

Team default-deny applies to CNPG. Database networking adds only application
and Prometheus ingress plus four egress paths: SeaweedFS S3, the Kubernetes
API Service, private RFC1918 endpoints on the kube-apiserver port required by
CNPG's in-Pod manager, and same-Cluster PostgreSQL/manager ports for
multi-instance operation. Calico evaluates the kind API path after Service
DNAT, so a kube-system Pod selector cannot match the host-network endpoint.
CoreDNS is provided by the Team-wide DNS policy. Omitting either API form
causes initdb Jobs to time out and be recreated indefinitely even though
admission and image pulls succeed.

The data graph is full-profile-only. The reusable Application leaf omits
`databaseRef`; standard-profile tenant rendering uses that leaf unchanged,
while the full profile adds `databaseRef.name=orders` and retains
Team/Database/Application waves. Cert-manager leader
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

Database health follows the public Kubernetes condition contract:
`ConfigurationReady`, `ClusterReady`, `BackupHealthy`, and `Ready`.
`DatabaseReady` is reserved for the Application condition that reports the
state of its referenced Database. The controller removes that obsolete
condition from Database status if it encounters state written by an earlier
pre-release build.

Hosted backup/restore automation never places the application password on a
process command line. Administrative Pod-local SQL uses the container's
`postgres` operating-system identity and PostgreSQL peer authentication.
Workload seed and checksum statements immediately `SET ROLE app`; the
privileged WAL switch remains a separate explicit administrative operation.

Deletion creates and waits for one deterministic final Backup before removing
the data plane; external objects remain. The final Backup deliberately has no
owner reference to the already-terminating Database and is deleted explicitly
after completion. Its dynamic object is initialized with the complete desired
GVK, namespace, and deterministic name before `CreateOrUpdate`, just like
owned unstructured children, while intentionally retaining no owner reference.
A failed final backup holds the finalizer.
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
  Checkov excludes only that byte-exact source fixture, while checksum and
  rendered-leaf tests remain blocking.
- Recovery is reviewable Git intent, never a patch to generated CNPG objects.
- Every schema-tested unstructured child is created with its desired identity
  before `CreateOrUpdate`, and its metadata uses JSON-compatible maps. This is
  a controller contract covered by an absent-child regression test, not an
  assumption delegated to the dynamic client.
- The PostgreSQL operand uses the frozen semantic tag and digest together.
  CloudNativePG needs the tag to reason about upgrades, while the digest keeps
  the admitted bytes immutable.
- Hosted objectives are RTO at most 30 minutes and a confirmed archive RPO
  boundary at most five minutes.
- The payments GitOps sample uses a 4 GiB Team memory quota so the normal
  database and the temporary hosted compatibility database can coexist during
  the foundation proof. ResourceQuota is an admission ceiling rather than a
  reservation, so this capacity does not raise the measured working set.
- Team default-deny requires an explicit CloudNativePG control path. Each
  Database admits only the `cloudnative-pg` operator Pod from `cnpg-system` to
  its selected instance Pods on manager TCP 8000, allowing authoritative
  status extraction without exposing PostgreSQL or the manager to other
  platform workloads.
