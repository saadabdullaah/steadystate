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
