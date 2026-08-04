{{- if .Values.bootstrapRoot }}
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: steadystate-root
  namespace: argocd
spec:
  project: root
  source:
    repoURL: {{ .Values.repoURL | quote }}
    targetRevision: {{ .Values.rootTargetRevision | quote }}
    path: gitops/clusters/local
    helm:
      parameters:
        - name: gitRevision
          value: "$ARGOCD_APP_REVISION"
        - name: enableTelemetryPipeline
          value: {{ .Values.enableTelemetryPipeline | quote }}
        - name: enableSecurity
          value: {{ .Values.enableSecurity | quote }}
        - name: enableDataFoundation
          value: {{ .Values.enableDataFoundation | quote }}
        - name: enableTenantWorkloads
          value: {{ .Values.enableTenantWorkloads | quote }}
        - name: monitoringKubeStateMetricsEnabled
          value: {{ .Values.monitoringKubeStateMetricsEnabled | quote }}
        - name: backupStoreEndpoint
          value: {{ .Values.backupStoreEndpoint | quote }}
  destination:
    server: https://kubernetes.default.svc
    namespace: argocd
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    retry:
      limit: 12
      backoff:
        duration: 5s
        factor: 2
        maxDuration: 30s
    syncOptions:
      - RespectIgnoreDifferences=true
  ignoreDifferences:
    - group: argoproj.io
      kind: Application
      jsonPointers:
        - /metadata/finalizers
        - /status
{{- end }}
