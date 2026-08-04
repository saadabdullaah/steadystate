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
    argocd.argoproj.io/ignore-healthcheck: "true"
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
        parameters:
          - name: kubeStateMetrics.enabled
            value: {{ .Values.monitoringKubeStateMetricsEnabled | quote }}
    - repoURL: {{ .Values.repoURL | quote }}
      targetRevision: {{ .Values.gitRevision | quote }}
      ref: values
    - repoURL: {{ .Values.repoURL | quote }}
      targetRevision: {{ .Values.gitRevision | quote }}
      path: gitops/platform/observability
  destination:
    server: https://kubernetes.default.svc
    namespace: monitoring
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - ServerSideApply=true
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
{{- if .Values.enableTelemetryPipeline }}
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: loki
  namespace: argocd
  annotations:
    argocd.argoproj.io/sync-wave: "-14"
spec:
  project: platform
  sources:
    - repoURL: https://grafana-community.github.io/helm-charts
      chart: loki
      targetRevision: {{ .Values.lokiChartVersion | quote }}
      helm:
        releaseName: loki
        valueFiles:
          - $values/gitops/platform/loki/values.yaml
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
  name: tempo
  namespace: argocd
  annotations:
    argocd.argoproj.io/sync-wave: "-13"
spec:
  project: platform
  sources:
    - repoURL: https://grafana.github.io/helm-charts
      chart: tempo
      targetRevision: {{ .Values.tempoChartVersion | quote }}
      helm:
        releaseName: tempo
        valueFiles:
          - $values/gitops/platform/tempo/values.yaml
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
  name: otel-collector
  namespace: argocd
  annotations:
    argocd.argoproj.io/sync-wave: "-12"
spec:
  project: platform
  sources:
    - repoURL: https://open-telemetry.github.io/opentelemetry-helm-charts
      chart: opentelemetry-collector
      targetRevision: {{ .Values.otelCollectorChartVersion | quote }}
      helm:
        releaseName: otel-collector
        valueFiles:
          - $values/gitops/platform/otel-collector/values.yaml
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
  name: alloy
  namespace: argocd
  annotations:
    argocd.argoproj.io/sync-wave: "-11"
spec:
  project: platform
  sources:
    - repoURL: https://grafana.github.io/helm-charts
      chart: alloy
      targetRevision: {{ .Values.alloyChartVersion | quote }}
      helm:
        releaseName: alloy
        valueFiles:
          - $values/gitops/platform/alloy/values.yaml
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
{{- end }}
{{- if .Values.enableDataFoundation }}
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: data-namespaces
  namespace: argocd
  annotations:
    argocd.argoproj.io/sync-wave: "-10"
spec:
  project: platform
  source:
    repoURL: {{ .Values.repoURL | quote }}
    targetRevision: {{ .Values.gitRevision | quote }}
    path: gitops/platform/data-namespaces
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
  name: local-path-storage
  namespace: argocd
  annotations:
    argocd.argoproj.io/sync-wave: "-9"
spec:
  project: platform
  source:
    repoURL: {{ .Values.repoURL | quote }}
    targetRevision: {{ .Values.gitRevision | quote }}
    path: gitops/platform/local-path
  destination:
    server: https://kubernetes.default.svc
    namespace: local-path-storage
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - ApplyOutOfSyncOnly=true
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: cert-manager
  namespace: argocd
  annotations:
    argocd.argoproj.io/sync-wave: "-8"
spec:
  project: platform
  sources:
    - repoURL: https://charts.jetstack.io
      chart: cert-manager
      targetRevision: {{ .Values.certManagerChartVersion | quote }}
      helm:
        releaseName: cert-manager
        valueFiles:
          - $values/gitops/platform/cert-manager/values.yaml
    - repoURL: {{ .Values.repoURL | quote }}
      targetRevision: {{ .Values.gitRevision | quote }}
      ref: values
  destination:
    server: https://kubernetes.default.svc
    namespace: cert-manager
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - ServerSideApply=true
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: cloudnative-pg
  namespace: argocd
  annotations:
    argocd.argoproj.io/sync-wave: "-7"
spec:
  project: platform
  sources:
    - repoURL: https://cloudnative-pg.github.io/charts
      chart: cloudnative-pg
      targetRevision: {{ .Values.cloudNativePGChartVersion | quote }}
      helm:
        releaseName: cloudnative-pg
        valueFiles:
          - $values/gitops/platform/cloudnative-pg/values.yaml
    - repoURL: {{ .Values.repoURL | quote }}
      targetRevision: {{ .Values.gitRevision | quote }}
      ref: values
  destination:
    server: https://kubernetes.default.svc
    namespace: cnpg-system
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - ServerSideApply=true
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: barman-cloud
  namespace: argocd
  annotations:
    argocd.argoproj.io/sync-wave: "-6"
    # The plugin can report Degraded briefly while cert-manager issues its
    # serving certificate. Keep the parent wave pending so automated sync can
    # recover; the child Application still exposes its exact native health.
    steadystate.dev/transient-degraded-as-progressing: "true"
spec:
  project: platform
  sources:
    - repoURL: https://cloudnative-pg.github.io/charts
      chart: plugin-barman-cloud
      targetRevision: {{ .Values.barmanCloudChartVersion | quote }}
      helm:
        releaseName: barman-cloud
        valueFiles:
          - $values/gitops/platform/barman-cloud/values.yaml
    - repoURL: {{ .Values.repoURL | quote }}
      targetRevision: {{ .Values.gitRevision | quote }}
      ref: values
  destination:
    server: https://kubernetes.default.svc
    namespace: cnpg-system
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - ServerSideApply=true
{{- end }}
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: steadystate-operator
  namespace: argocd
  annotations:
    # Full-profile external CRDs must exist before manager discovery registers
    # Database child watches. Standard profiles retain the historical wave.
    argocd.argoproj.io/sync-wave: {{ if .Values.enableDataFoundation }}"-5"{{ else }}"-10"{{ end }}
spec:
  project: platform
  source:
    repoURL: {{ .Values.repoURL | quote }}
    targetRevision: {{ .Values.gitRevision | quote }}
    path: config/default
    kustomize:
      images:
        - {{ .Values.operatorImage | quote }}
      patches:
        - target:
            group: apps
            version: v1
            kind: Deployment
            name: controller-manager
          patch: |-
            - op: add
              path: /spec/template/spec/containers/0/env
              value:
                - name: BACKUP_STORE_ENDPOINT
                  value: {{ .Values.backupStoreEndpoint | quote }}
  destination:
    server: https://kubernetes.default.svc
    namespace: steadystate-system
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
{{- if .Values.enableSecurity }}
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: kyverno
  namespace: argocd
  annotations:
    # Admission must become available before the optional telemetry pipeline.
    # This prevents a full-profile telemetry startup burst from delaying the
    # fail-closed webhook and every later data-foundation wave.
    argocd.argoproj.io/sync-wave: "-16"
    argocd.argoproj.io/compare-options: ServerSideDiff=true,IncludeMutationWebhook=true
spec:
  project: platform
  sources:
    - repoURL: https://kyverno.github.io/kyverno/
      chart: kyverno
      targetRevision: {{ .Values.kyvernoChartVersion | quote }}
      helm:
        releaseName: kyverno
        skipTests: true
        valueFiles:
          - $values/gitops/platform/kyverno/values.yaml
    - repoURL: {{ .Values.repoURL | quote }}
      targetRevision: {{ .Values.gitRevision | quote }}
      ref: values
  destination:
    server: https://kubernetes.default.svc
    namespace: kyverno
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - ServerSideApply=true
      - RespectIgnoreDifferences=true
  ignoreDifferences:
    # Kyverno injects the serving CA into its conversion webhook CRDs at
    # runtime. The certificate is controller-owned and must not cause Argo
    # self-heal loops.
    - group: apiextensions.k8s.io
      kind: CustomResourceDefinition
      jqPathExpressions:
        - .spec.conversion.webhook.clientConfig.caBundle
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: kyverno-policies
  namespace: argocd
  annotations:
    argocd.argoproj.io/sync-wave: "-15"
spec:
  project: platform
  source:
    repoURL: {{ .Values.repoURL | quote }}
    targetRevision: {{ .Values.gitRevision | quote }}
    path: gitops/platform/kyverno-policies
  destination:
    server: https://kubernetes.default.svc
    namespace: kyverno
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - ServerSideApply=true
{{- end }}
{{- if .Values.enableTenantWorkloads }}
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
{{- if .Values.enableDataFoundation }}
    - repoURL: {{ .Values.repoURL | quote }}
      targetRevision: {{ .Values.gitRevision | quote }}
      path: gitops/databases/orders
      kustomize:
        commonAnnotations:
          steadystate.dev/source-revision: "$ARGOCD_APP_REVISION"
        commonAnnotationsEnvsubst: true
{{- end }}
    - repoURL: {{ .Values.repoURL | quote }}
      targetRevision: {{ .Values.gitRevision | quote }}
      path: gitops/applications/demo
      kustomize:
        commonAnnotations:
          steadystate.dev/source-revision: "$ARGOCD_APP_REVISION"
        commonAnnotationsEnvsubst: true
{{- if .Values.enableDataFoundation }}
        patches:
          - target:
              group: platform.steadystate.dev
              version: v1alpha1
              kind: Application
              name: demo
            patch: |-
              - op: add
                path: /spec/databaseRef
                value:
                  name: orders
{{- end }}
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
      kind: Database
      jsonPointers:
        - /metadata/finalizers
        - /status
    - group: platform.steadystate.dev
      kind: Application
      jsonPointers:
        - /metadata/finalizers
        - /status
{{- end }}
