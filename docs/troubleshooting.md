# Troubleshooting

## Docker engine is stopped

Start Docker Desktop, wait until the engine is ready, and run:

```powershell
docker info
.\scripts\dev.ps1 doctor
```

## Docker is too old or uses cgroup v1

The Kubernetes 1.35.5 kind profile requires Docker Engine 24 or newer with cgroup v2. Upgrade Docker Desktop, confirm it uses the WSL2 Linux engine, restart it, and rerun `doctor`. SteadyState fails before cluster creation rather than silently falling back to another Kubernetes release.

## Port 8080 or 8443 is occupied

```powershell
Get-NetTCPConnection -LocalPort 8080 -State Listen
.\scripts\dev.ps1 bootstrap -HttpPort 9080 -HttpsPort 9443
```

Use the same overrides for subsequent smoke, diagnostics, and destroy commands.

## Nodes remain NotReady

With kindnet disabled, nodes remain NotReady until Calico is healthy:

```powershell
kubectl get pods -n tigera-operator
kubectl get pods -n calico-system -o wide
kubectl get tigerastatus
```

VPNs and corporate proxies can interfere with image pulls and pod networking. Capture diagnostics before destroying the cluster.

## Gateway is not programmed

```powershell
kubectl get gatewayclass,gateway,httproute -A
kubectl describe gateway -n steadystate-smoke steadystate
kubectl get pods -n envoy-gateway-system
kubectl get events -A --sort-by=.lastTimestamp
```

## Partial bootstrap

Bootstrap retains a failed cluster and writes diagnostics under `.artifacts/diagnostics/`. Correct the cause and rerun bootstrap; the operation reconciles existing state.

## Operator deployment is unavailable

```powershell
kubectl get deployment -n steadystate-system steadystate-controller-manager
kubectl logs -n steadystate-system deployment/steadystate-controller-manager --all-containers
kubectl auth can-i --as system:serviceaccount:steadystate-system:controller-manager get applications.platform.steadystate.dev --all-namespaces
```

Confirm both Phase 1 images were built and loaded into the named kind cluster before deploying the operator. Phase 1 intentionally uses local `IfNotPresent` images; registry publication starts later.

If a Team stops at `RBACReady=False`, confirm the install-time `steadystate-team-owner` ClusterRole exists and inspect the operator log for Kubernetes binding prevention. The manager must have resource-name-scoped `bind` permission for only that ClusterRole. It must not have `escalate` permission or receive the Team owner's Secret and Pod execution permissions cluster-wide.

## Application remains Progressing

```powershell
kubectl get application -n team-payments demo -o yaml
kubectl get deployment,service,configmap,httproute -n team-payments
kubectl describe deployment -n team-payments demo
kubectl describe httproute -n team-payments demo
```

`Ready=True` requires the Deployment to report its current generation available and the HTTPRoute to report both `Accepted=True` and `ResolvedRefs=True` for the shared Gateway.

## Application reports RepositoryNotAllowed or NamespaceNotManaged

```powershell
kubectl get team payments -o yaml
kubectl get namespace team-payments -o yaml
kubectl get application -n team-payments demo -o jsonpath='{.spec.image.repository}'
```

The Namespace must be named `team-<team>`, carry `steadystate.dev/team=<team>`, and contain the exact current Team UID annotation. The image repository must match one of `spec.allowedRepositories`; matching is anchored, case-sensitive, and uses Go path-style globs. Do not bypass the guard by editing Namespace ownership metadata. Correct the Team or Application specification and the dependency watches will reconcile the Application.

## HTTPRoute is rejected

Inspect the route's parent conditions, the shared Gateway, and namespace permissions:

```powershell
kubectl get httproute -n team-payments demo -o yaml
kubectl get gateway -n steadystate-system steadystate -o yaml
kubectl get referencegrant -A
kubectl get events -n team-payments --sort-by=.lastTimestamp
```

The expected parent is `steadystate-system/steadystate`. The operator reports rejected or unresolved references as `Ready=False` rather than treating object creation as success.

## Application deletion is stuck

```powershell
kubectl get application -n team-payments demo -o jsonpath='{.metadata.finalizers}'
kubectl logs -n steadystate-system deployment/steadystate-controller-manager --all-containers --tail=200
kubectl get deployment,service,configmap,httproute -n team-payments -l app.kubernetes.io/managed-by=steadystate
```

Do not remove the finalizer manually until controller logs have been captured. Phase 1 has no external cleanup; a healthy controller releases the finalizer and Kubernetes garbage collection removes all owned children.

## NetworkPolicy proof fails

The test asserts that `calico-node` exists, proves connectivity before applying a deny policy, expects a timeout afterward, and verifies DNS remains usable. If traffic still succeeds, inspect Calico status rather than accepting a false isolation claim.

## Phase 2 isolation acceptance fails

Run the suite on a standard-profile cluster only after building, loading, deploying, and testing the operator:

```powershell
.\scripts\dev.ps1 test-isolation -Profile standard -EvidencePath .artifacts/phase2/acceptance.json
.\scripts\dev.ps1 diagnostics
```

No evidence file is written when an assertion fails. Preserve the cluster until diagnostics are captured. For a direct-network failure, verify all `calico-node` replicas are Ready and inspect the three generated Team NetworkPolicies. For RBAC, compare `kubectl auth can-i` using `system:serviceaccount:team-orders:steadystate-team-owner` in both namespaces. For quota rejection, inspect `steadystate-quota` usage and the admission error. If Team deletion stalls, inspect the Team condition, Namespace ownership annotation, Application finalizers, and controller logs before changing any finalizer manually.

## Argo CD route or login fails

```powershell
kubectl get httproute -n argocd argocd -o yaml
kubectl get service,pods -n argocd
kubectl get gateway -n steadystate-system steadystate -o yaml
Invoke-WebRequest http://argocd.localtest.me:8080
```

The local route intentionally uses HTTP-only server mode through the loopback-bound shared Gateway. Confirm `argocd.localtest.me` resolves to `127.0.0.1`, the route reports `Accepted=True` and `ResolvedRefs=True`, and the `argocd` Namespace permits shared-Gateway traffic. The local username is `admin`. Retrieve the initial password directly from `argocd-initial-admin-secret` only in your interactive terminal; never paste it into workflow output, diagnostics, issues, or evidence.

## Argo sync is OutOfSync or Progressing

```powershell
kubectl get applications.argoproj.io -n argocd
kubectl get application.argoproj.io -n argocd steadystate-root -o yaml
kubectl logs -n argocd deployment/argocd-repo-server --tail=200
kubectl logs -n argocd statefulset/argocd-application-controller --tail=200
.\scripts\dev.ps1 verify-gitops
```

All child Applications must target the exact revision resolved by the root. Check `status.sync.revision`, the root Helm `gitRevision` parameter, project destination and kind restrictions, and sync-wave ordering. Do not add `CreateNamespace`: the Team controller must create and mark the tenant Namespace before the namespaced Application is applied. Render failures should be reproduced with `verify-gitops` before changing live objects.

## Argo health disagrees with Kubernetes status

```powershell
kubectl get team payments -o yaml
kubectl get application.platform.steadystate.dev -n team-payments demo -o yaml
kubectl get configmap -n argocd argocd-cm -o yaml
kubectl get application.argoproj.io -n argocd payments -o yaml
```

Lua health treats a stale or missing `observedGeneration` as Progressing. A current Team requires `Ready=True`; a current SteadyState Application requires `Phase=Healthy` and `Ready=True`. `Phase=Degraded` must appear as Degraded in Argo. Confirm the health customizations remain in `argocd-cm` and that the Argo Application forwards child health.

## Runtime digest or Git revision is missing

```powershell
kubectl get pods -n team-payments -l app.kubernetes.io/name=demo -o jsonpath='{range .items[*].status.containerStatuses[*]}{.name}{" "}{.imageID}{"\n"}{end}'
kubectl get application.platform.steadystate.dev -n team-payments demo -o jsonpath='{.metadata.annotations.steadystate\.dev/source-revision}{"\n"}{.status.resolvedImageDigest}{"\n"}{.status.resolvedGitRevision}{"\n"}'
```

The active release is promoted only when ready desired Pods resolve to exactly one canonical SHA-256 digest. No digest keeps the Application Progressing; conflicting digests make it Degraded while preserving the last healthy tuple. The source annotation must be a full lowercase 40- or 64-character Git object ID. A revision-only change updates status without mutating or restarting the workload.

## Public demo image cannot be pulled

Verify the exact immutable tag at the [SteadyState demo-app package](https://github.com/saadabdullaah/steadystate/pkgs/container/steadystate-demo-app). The package must remain public for anonymous kind pulls. Do not substitute `latest`. If a semver or SHA tag already exists with a different digest, the release workflow intentionally fails closed; investigate the registry metadata instead of overwriting the tag.

## Operator-generated children appear as Argo orphans

This is expected. The tenant AppProject sets `orphanedResources.warn: false` because Argo owns only Team and SteadyState Application CRs. The operator exclusively owns generated Deployments, Services, ConfigMaps, and HTTPRoutes. Do not add those children to GitOps state or enable Argo pruning for the tenant Application.

## Operator Pod is replaced during GitOps operation

```powershell
kubectl get application.argoproj.io -n argocd payments -o yaml
kubectl get application.platform.steadystate.dev -n team-payments demo -o yaml
kubectl get deployment,service,configmap,httproute -n team-payments -o wide
kubectl rollout status deployment/steadystate-controller-manager -n steadystate-system --timeout=180s
kubectl logs -n steadystate-system deployment/steadystate-controller-manager --all-containers --tail=200
```

The tenant Argo Application and data-plane resources should remain Healthy and retain their UIDs while the operator Deployment replaces its Pod. After restart, the controller must reconcile without generation or resource-version drift. Capture diagnostics before manually changing finalizers or managed children.

## Canary rollout is stuck Progressing

```powershell
kubectl get rollout,analysisrun,analysistemplate -n team-payments
kubectl argo rollouts get rollout demo -n team-payments
kubectl get httproute -n team-payments demo -o yaml
kubectl get pods -n team-payments -l app.kubernetes.io/name=demo -o wide
```

Confirm the desired Pod is Ready, the HTTPRoute has stable and canary backends, and the Gateway plugin's in-progress label is present while weights are changing. A canary step waits for its pause, Prometheus scrape, and two successful analysis measurements. Do not patch weights or Service selectors: those fields are temporarily owned by the Gateway plugin and Rollouts. Run `verify-progressive-delivery` if the generated structure or plugin permissions look wrong.

## Application reports ProgressiveDeliveryUnavailable

The standalone operator supports rolling Applications without installing Argo Rollouts or Prometheus Operator CRDs. Canary Applications require all five progressive APIs. Install them through `deploy-gitops`, verify them with `verify-progressive-delivery`, and restart the operator so startup discovery registers their watches:

```powershell
kubectl get crd rollouts.argoproj.io analysisruns.argoproj.io analysistemplates.argoproj.io
kubectl get crd servicemonitors.monitoring.coreos.com prometheusrules.monitoring.coreos.com
kubectl rollout restart deployment/steadystate-controller-manager -n steadystate-system
kubectl rollout status deployment/steadystate-controller-manager -n steadystate-system --timeout=180s
```

Do not install partial CRD sets or change a rolling Application merely to clear the condition. Missing progressive APIs must not stop the rolling controller or mutate canary children.

## Analysis is Failed, Error, or Inconclusive

```powershell
kubectl get analysisrun -n team-payments -o yaml
kubectl get servicemonitor,prometheusrule -n team-payments -o yaml
kubectl get pods,service -n monitoring
kubectl logs -n monitoring statefulset/prometheus-kube-prometheus-stack-prometheus --tail=200
kubectl logs -n argo-rollouts deployment/argo-rollouts --tail=200
```

Failed success-rate or latency queries usually mean the candidate received real failing traffic. Empty request/latency vectors deliberately fail safe; ensure load reaches the Gateway and the ServiceMonitor target is up. Missing restart vectors are treated as zero. Two consecutive Prometheus provider errors cannot promote a candidate. With `automaticRollback: true`, expect stable traffic restoration followed by `Phase=Degraded`, reason `CanaryAnalysisFailed`, until Git restores the healthy tag. With automatic rollback disabled, use `kubectl argo rollouts promote` or `abort` only after reviewing the AnalysisRun.

## Prometheus cannot scrape a Team application

```powershell
kubectl get networkpolicy -n team-payments steadystate-allow-monitoring -o yaml
kubectl get servicemonitor -n team-payments -o yaml
kubectl get endpointslice -n team-payments -l kubernetes.io/service-name=demo
kubectl get pods -n monitoring --show-labels
```

The Team policy permits only Prometheus Pods from the `monitoring` Namespace to the named `http` port. Preserve the Namespace and Pod selectors installed by GitOps. The base Service remains present in canary mode specifically for metrics and reversible migration; deleting or repointing it creates an analysis outage and the operator will repair it.

## Rollback restored traffic but the Application remains Degraded

This is expected and truthful. Argo Rollouts aborts the candidate and restores stable traffic, but Git still requests the failed image. Verify that the last healthy tuple is unchanged, then merge a Git recovery commit restoring the healthy tag:

```powershell
kubectl get application.platform.steadystate.dev -n team-payments demo -o jsonpath='{.status.phase}{"\n"}{.status.activeVersion}{"\n"}{.status.resolvedImageDigest}{"\n"}{.status.resolvedGitRevision}{"\n"}'
kubectl get rollout -n team-payments demo -o yaml
```

Do not force the status to Healthy or manually rewrite the Rollout. The recovery commit updates the active Git revision after the stable image is again the desired state.

## Rolling/canary migration does not finish

```powershell
kubectl get deployment,rollout,replicaset,pod -n team-payments -o wide
kubectl get service,httproute -n team-payments -o yaml
kubectl logs -n steadystate-system deployment/steadystate-controller-manager --all-containers --tail=300
```

Rolling-to-canary creates and verifies the Rollout beside the serving Deployment before switching the route. Canary-to-rolling scales and verifies the Deployment before switching back to the base Service. A controller restart resumes from live readiness. Preserve both workload controllers and Services while diagnosing; deleting the apparent "old" object can remove the migration anchor. If a Rollout was deleted while a failed candidate remained desired, the operator must reconstruct the last active version rather than promote the bad tag.

## Root sync waits on monitoring for an unrelated tenant commit

The monitoring child carries `argocd.argoproj.io/ignore-healthcheck: "true"` so its transient chart health refresh cannot gate every tenant-only revision. This does not waive platform readiness: `deploy-gitops`, `test-gitops`, and Phase 4 preparation explicitly require monitoring and all other platform children to be Synced and Healthy before delivery testing. If the root still waits, render the root chart and confirm the annotation is present before changing sync waves.

## Reset

```powershell
.\scripts\dev.ps1 diagnostics
.\scripts\dev.ps1 destroy
.\scripts\dev.ps1 bootstrap -Profile minimal
```
