PROFILE ?= minimal
CLUSTER_NAME ?= steadystate
HTTP_PORT ?= 8080
HTTPS_PORT ?= 8443
GIT_REVISION ?= main

.PHONY: doctor tools check-versions generate manifests verify-generated lint test test-envtest run build-images load-images deploy-operator test-operator demo-self-heal test-isolation undeploy-operator deploy-gitops test-gitops undeploy-gitops verify-gitops verify-progressive-delivery test-progressive-delivery bootstrap smoke test-network-policy diagnostics destroy

doctor tools check-versions generate manifests verify-generated lint test test-envtest run build-images load-images deploy-operator test-operator demo-self-heal test-isolation undeploy-operator deploy-gitops test-gitops undeploy-gitops verify-gitops verify-progressive-delivery test-progressive-delivery smoke test-network-policy diagnostics destroy:
	pwsh -NoProfile -File scripts/dev.ps1 $@ -Profile $(PROFILE) -ClusterName $(CLUSTER_NAME) -HttpPort $(HTTP_PORT) -HttpsPort $(HTTPS_PORT) -GitRevision $(GIT_REVISION)

bootstrap:
	pwsh -NoProfile -File scripts/dev.ps1 bootstrap -Profile $(PROFILE) -ClusterName $(CLUSTER_NAME) -HttpPort $(HTTP_PORT) -HttpsPort $(HTTPS_PORT)
