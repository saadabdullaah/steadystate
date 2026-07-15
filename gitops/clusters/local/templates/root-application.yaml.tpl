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
  destination:
    server: https://kubernetes.default.svc
    namespace: argocd
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - RespectIgnoreDifferences=true
  ignoreDifferences:
    - group: argoproj.io
      kind: Application
      jsonPointers:
        - /metadata/finalizers
        - /status
{{- end }}
