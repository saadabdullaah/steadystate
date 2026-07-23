apiVersion: argoproj.io/v1alpha1
kind: AppProject
metadata:
  name: root
  namespace: argocd
  annotations:
    argocd.argoproj.io/sync-wave: "-30"
spec:
  description: App-of-apps resources in the Argo CD namespace only.
  sourceRepos:
    - {{ .Values.repoURL | quote }}
  destinations:
    - server: https://kubernetes.default.svc
      namespace: argocd
  namespaceResourceWhitelist:
    - group: argoproj.io
      kind: AppProject
    - group: argoproj.io
      kind: Application
---
apiVersion: argoproj.io/v1alpha1
kind: AppProject
metadata:
  name: platform
  namespace: argocd
  annotations:
    argocd.argoproj.io/sync-wave: "-30"
spec:
  description: Argo configuration, metrics, logs, traces, Rollouts, and the SteadyState operator.
  sourceRepos:
    - {{ .Values.repoURL | quote }}
    - https://argoproj.github.io/argo-helm
    - https://prometheus-community.github.io/helm-charts
    - https://grafana-community.github.io/helm-charts
    - https://grafana.github.io/helm-charts
    - https://open-telemetry.github.io/opentelemetry-helm-charts
    - https://kyverno.github.io/kyverno/
  destinations:
    - server: https://kubernetes.default.svc
      namespace: argocd
    - server: https://kubernetes.default.svc
      namespace: steadystate-system
    - server: https://kubernetes.default.svc
      namespace: monitoring
    - server: https://kubernetes.default.svc
      namespace: argo-rollouts
    - server: https://kubernetes.default.svc
      namespace: kyverno
  clusterResourceWhitelist:
    - group: ""
      kind: Namespace
    - group: apiextensions.k8s.io
      kind: CustomResourceDefinition
    - group: rbac.authorization.k8s.io
      kind: ClusterRole
    - group: rbac.authorization.k8s.io
      kind: ClusterRoleBinding
    - group: policies.kyverno.io
      kind: ImageValidatingPolicy
    - group: policies.kyverno.io
      kind: ValidatingPolicy
  namespaceResourceWhitelist:
    - group: ""
      kind: ConfigMap
    - group: ""
      kind: Secret
    - group: ""
      kind: ServiceAccount
    - group: ""
      kind: Service
    - group: apps
      kind: Deployment
    - group: apps
      kind: DaemonSet
    - group: apps
      kind: StatefulSet
    - group: gateway.networking.k8s.io
      kind: HTTPRoute
    - group: networking.k8s.io
      kind: NetworkPolicy
    - group: rbac.authorization.k8s.io
      kind: Role
    - group: rbac.authorization.k8s.io
      kind: RoleBinding
    - group: monitoring.coreos.com
      kind: Alertmanager
    - group: monitoring.coreos.com
      kind: Prometheus
    - group: monitoring.coreos.com
      kind: ServiceMonitor
    - group: policy
      kind: PodDisruptionBudget
---
apiVersion: argoproj.io/v1alpha1
kind: AppProject
metadata:
  name: tenant
  namespace: argocd
  annotations:
    argocd.argoproj.io/sync-wave: "-30"
spec:
  description: Team boundaries and namespaced SteadyState Applications only.
  sourceRepos:
    - {{ .Values.repoURL | quote }}
  destinations:
    - server: https://kubernetes.default.svc
      namespace: team-*
  clusterResourceWhitelist:
    - group: platform.steadystate.dev
      kind: Team
  namespaceResourceWhitelist:
    - group: platform.steadystate.dev
      kind: Application
  orphanedResources:
    warn: false
