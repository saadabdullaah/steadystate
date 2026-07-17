# Troubleshooting

## Docker engine is stopped

Start Docker Desktop, wait until the engine is ready, and run:

```powershell
docker info
.\scripts\dev.ps1 doctor
```

## Docker is too old or uses cgroup v1

The Kubernetes 1.36 kind profile requires Docker Engine 24 or newer with cgroup v2. Upgrade Docker Desktop, confirm it uses the WSL2 Linux engine, restart it, and rerun `doctor`. SteadyState fails before cluster creation rather than silently falling back to an older Kubernetes release.

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

## Reset

```powershell
.\scripts\dev.ps1 diagnostics
.\scripts\dev.ps1 destroy
.\scripts\dev.ps1 bootstrap -Profile minimal
```
