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

## Application remains Progressing

```powershell
kubectl get application -n steadystate-demo demo -o yaml
kubectl get deployment,service,configmap,httproute -n steadystate-demo
kubectl describe deployment -n steadystate-demo demo
kubectl describe httproute -n steadystate-demo demo
```

`Ready=True` requires the Deployment to report its current generation available and the HTTPRoute to report both `Accepted=True` and `ResolvedRefs=True` for the shared Gateway.

## HTTPRoute is rejected

Inspect the route's parent conditions, the shared Gateway, and namespace permissions:

```powershell
kubectl get httproute -n steadystate-demo demo -o yaml
kubectl get gateway -n steadystate-system steadystate -o yaml
kubectl get referencegrant -A
kubectl get events -n steadystate-demo --sort-by=.lastTimestamp
```

The expected parent is `steadystate-system/steadystate`. The operator reports rejected or unresolved references as `Ready=False` rather than treating object creation as success.

## Application deletion is stuck

```powershell
kubectl get application -n steadystate-demo demo -o jsonpath='{.metadata.finalizers}'
kubectl logs -n steadystate-system deployment/steadystate-controller-manager --all-containers --tail=200
kubectl get deployment,service,configmap,httproute -n steadystate-demo -l app.kubernetes.io/managed-by=steadystate
```

Do not remove the finalizer manually until controller logs have been captured. Phase 1 has no external cleanup; a healthy controller releases the finalizer and Kubernetes garbage collection removes all owned children.

## NetworkPolicy proof fails

The test asserts that `calico-node` exists, proves connectivity before applying a deny policy, expects a timeout afterward, and verifies DNS remains usable. If traffic still succeeds, inspect Calico status rather than accepting a false isolation claim.

## Reset

```powershell
.\scripts\dev.ps1 diagnostics
.\scripts\dev.ps1 destroy
.\scripts\dev.ps1 bootstrap -Profile minimal
```
