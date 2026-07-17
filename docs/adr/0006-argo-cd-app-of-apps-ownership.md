# ADR-0006: Use Argo CD App-of-Apps with Explicit Ownership Boundaries

## Context

SteadyState needs a Git commit to drive platform and application delivery without human `kubectl`, while preserving the operator as the only reconciler of generated workload resources. A monorepo validation branch must also deploy one coherent revision; allowing child Argo Applications to resolve branches independently can mix commits during a sync. Argo's default health and resource tracking do not understand SteadyState status, finalizers, or the intentional operator ownership of generated children.

The local environment has one shared Envoy Gateway, limited resources, and no external identity provider. Team pruning is not yet safe because Git-driven tenant decommissioning has not been designed.

## Decision

Install the checksum-pinned Argo CD v3.4.2 non-HA manifest. Remove the five unused Dex objects, run the server in local HTTP-only mode, and expose `argocd.localtest.me` through an HTTPRoute on the shared Gateway. Credentials remain local and are never written to workflow logs or acceptance evidence.

Use `gitops/clusters/local` as a small Helm app-of-apps root. The root's resolved `$ARGOCD_APP_REVISION` is passed as `gitRevision`, and every child targets that exact value. Kustomize leaves remain the authoritative plain desired state for platform configuration, Teams, and SteadyState Applications. They substitute the resolved revision into `steadystate.dev/source-revision` so runtime status can report the exact Git object delivered.

Apply deterministic sync waves: AppProjects at `-30`, Argo configuration at `-20`, the operator at `-10`, the tenant child at `0`, its Team CR at `-1`, and its SteadyState Application at `0`. Do not use `CreateNamespace`; the Team controller must create and establish the managed Namespace before the namespaced Application is admitted.

Use three least-privilege AppProjects. The root project permits only AppProjects and Argo Applications in `argocd`. The platform project permits only the kinds required by Argo configuration and `config/default`. The tenant project permits only cluster-scoped Teams and namespaced SteadyState Applications from this repository into `team-*`.

Root and platform Applications use automated prune and self-heal. Tenant Applications use automated self-heal without prune. Argo ignores controller-written status and finalizers with `RespectIgnoreDifferences=true`. Annotation resource tracking is frozen. Tenant orphan warnings are disabled because the operator, not Argo, owns generated Deployments, Services, ConfigMaps, and HTTPRoutes.

Install Lua health customizations for Team, SteadyState Application, and Argo Application. Health requires current observed generations. A current Team maps `Ready=True` to Healthy and `Ready=False` to Degraded. A SteadyState Application maps `Phase=Healthy` plus `Ready=True` to Healthy and `Phase=Degraded` to Degraded. Argo Applications forward child health so the root waits truthfully.

The SteadyState operator remains the sole field manager for generated workload children. Argo manages only platform installation resources and the Team/Application CR desired state. The operator writes only owned resources, finalizers, and status; it never writes CR spec.

## Consequences

One root sync produces a revision-consistent graph, and hosted branch validation cannot mix commits. Argo health reflects Kubernetes control-plane truth instead of object existence. Operator restarts do not cause Argo to prune or rewrite data-plane children, and ordinary child drift remains the operator's responsibility.

The local Argo server has no Dex or TLS termination of its own and must not be exposed beyond the loopback-bound development Gateway. Team deletion through Git remains intentionally unsupported because tenant pruning is disabled. Adding platform components in later phases requires an explicit AppProject kind review and sync wave rather than broadening permissions preemptively.
