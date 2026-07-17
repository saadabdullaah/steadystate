apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: argocd-configuration
  namespace: argocd
  annotations:
    argocd.argoproj.io/sync-wave: "-20"
spec:
  project: platform
  source:
    repoURL: {{ .Values.repoURL | quote }}
    targetRevision: {{ .Values.gitRevision | quote }}
    path: gitops/platform
  destination:
    server: https://kubernetes.default.svc
    namespace: argocd
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: monitoring
  namespace: argocd
  annotations:
    argocd.argoproj.io/sync-wave: "-18"
spec:
  project: platform
  sources:
    - repoURL: https://prometheus-community.github.io/helm-charts
      chart: kube-prometheus-stack
      targetRevision: {{ .Values.kubePrometheusStackChartVersion | quote }}
      helm:
        releaseName: monitoring
        valueFiles:
          - $values/gitops/platform/monitoring/values.yaml
    - repoURL: {{ .Values.repoURL | quote }}
      targetRevision: {{ .Values.gitRevision | quote }}
      ref: values
  destination:
    server: https://kubernetes.default.svc
    namespace: monitoring
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: argo-rollouts
  namespace: argocd
  annotations:
    argocd.argoproj.io/sync-wave: "-17"
spec:
  project: platform
  sources:
    - repoURL: https://argoproj.github.io/argo-helm
      chart: argo-rollouts
      targetRevision: {{ .Values.argoRolloutsChartVersion | quote }}
      helm:
        releaseName: argo-rollouts
        valueFiles:
          - $values/gitops/platform/rollouts/values.yaml
    - repoURL: {{ .Values.repoURL | quote }}
      targetRevision: {{ .Values.gitRevision | quote }}
      ref: values
  destination:
    server: https://kubernetes.default.svc
    namespace: argo-rollouts
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: steadystate-operator
  namespace: argocd
  annotations:
    argocd.argoproj.io/sync-wave: "-10"
spec:
  project: platform
  source:
    repoURL: {{ .Values.repoURL | quote }}
    targetRevision: {{ .Values.gitRevision | quote }}
    path: config/default
    kustomize:
      images:
        - {{ .Values.operatorImage | quote }}
  destination:
    server: https://kubernetes.default.svc
    namespace: steadystate-system
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: payments
  namespace: argocd
  annotations:
    argocd.argoproj.io/sync-wave: "0"
spec:
  project: tenant
  sources:
    - repoURL: {{ .Values.repoURL | quote }}
      targetRevision: {{ .Values.gitRevision | quote }}
      path: gitops/teams/payments
      kustomize:
        commonAnnotations:
          steadystate.dev/source-revision: "$ARGOCD_APP_REVISION"
        commonAnnotationsEnvsubst: true
    - repoURL: {{ .Values.repoURL | quote }}
      targetRevision: {{ .Values.gitRevision | quote }}
      path: gitops/applications/demo
      kustomize:
        commonAnnotations:
          steadystate.dev/source-revision: "$ARGOCD_APP_REVISION"
        commonAnnotationsEnvsubst: true
  destination:
    server: https://kubernetes.default.svc
    namespace: team-payments
  syncPolicy:
    automated:
      selfHeal: true
    syncOptions:
      - RespectIgnoreDifferences=true
  ignoreDifferences:
    - group: platform.steadystate.dev
      kind: Team
      jsonPointers:
        - /metadata/finalizers
        - /status
    - group: platform.steadystate.dev
      kind: Application
      jsonPointers:
        - /metadata/finalizers
        - /status
