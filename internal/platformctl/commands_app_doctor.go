package platformctl

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func runApplicationDoctor(ctx context.Context, selected Context, client *ClusterClient, namespace, name string, application *unstructured.Unstructured) []DoctorCheck {
	checks := make([]DoctorCheck, 0, 9)
	add := func(name, status, details, remediation string, evidence ...string) {
		checks = append(checks, DoctorCheck{Name: name, Status: status, Details: Redact(details), Evidence: evidence, Remediation: remediation})
	}
	team := strings.TrimPrefix(namespace, "team-")
	catalog, catalogErr := LoadCatalog(selected.CheckoutPath)
	if catalogErr != nil {
		add("ContextAndOwnership", "Fail", ErrorMessage(catalogErr), "Repair the configured checkout and catalog.", "file://gitops/clusters/local/catalog/tenants.yaml")
	} else if catalogNamespaceValue, err := catalogNamespace(catalog, "Application", name); err != nil || catalogNamespaceValue != namespace {
		add("ContextAndOwnership", "Fail", "Application is not owned by the selected Git catalog and Team namespace", "Use a context whose catalog owns this Application.", "kubernetes://"+namespace+"/applications/"+name)
	} else {
		add("ContextAndOwnership", "Pass", "context, API access, catalog ownership, and Team namespace agree", "", "file://gitops/clusters/local/catalog/tenants.yaml", "kubernetes://"+namespace+"/applications/"+name)
	}

	desiredRevision := application.GetAnnotations()["steadystate.dev/source-revision"]
	resolvedRevision, _, _ := unstructured.NestedString(application.Object, "status", "resolvedGitRevision")
	argo, argoErr := client.Get(ctx, argoAppGVR, "argocd", team)
	if argoErr != nil {
		add("GitOpsRevision", "Unknown", ErrorMessage(argoErr), "Run platformctl request status and inspect the tenant Argo Application.", "kubernetes://argocd/applications/"+team)
	} else {
		argoRevision, _, _ := unstructured.NestedString(argo.Object, "status", "sync", "revision")
		if desiredRevision == "" || resolvedRevision == "" || argoRevision == "" {
			add("GitOpsRevision", "Warning", "one or more desired, active, or Argo revisions are unresolved", "Wait for Argo and the operator to reconcile, then rerun doctor.", "kubernetes://argocd/applications/"+team)
		} else if desiredRevision != argoRevision || resolvedRevision != desiredRevision {
			add("GitOpsRevision", "Fail", fmt.Sprintf("revision mismatch desired=%s active=%s argo=%s", desiredRevision, resolvedRevision, argoRevision), "Inspect Argo sync and submit a recovery Git change; do not patch the Application.", "kubernetes://argocd/applications/"+team)
		} else {
			add("GitOpsRevision", "Pass", "Git desired, Argo synchronized, and active revisions agree at "+resolvedRevision, "", "kubernetes://argocd/applications/"+team)
		}
	}

	ready := doctorCondition(application, "Ready")
	if application.GetGeneration() != nestedInt64(application, "status", "observedGeneration") {
		add("ApplicationAndProvenance", "Fail", "status is stale for the current Application generation", "Wait for the operator or inspect its logs.", "kubernetes://"+namespace+"/applications/"+name)
	} else if ready.Status != "Pass" {
		add("ApplicationAndProvenance", ready.Status, ready.Details, conditionRemediation("Ready"), "kubernetes://"+namespace+"/applications/"+name)
	} else {
		version, _, _ := unstructured.NestedString(application.Object, "status", "activeVersion")
		digest, _, _ := unstructured.NestedString(application.Object, "status", "resolvedImageDigest")
		if version == "" || digest == "" || resolvedRevision == "" {
			add("ApplicationAndProvenance", "Warning", "Application is Ready but its active provenance tuple is incomplete", "Inspect the application Pods and image resolver status.", "kubernetes://"+namespace+"/applications/"+name)
		} else {
			add("ApplicationAndProvenance", "Pass", "current-generation Application is Ready with an active version, digest, and revision", "", "kubernetes://"+namespace+"/applications/"+name)
		}
	}

	security := doctorCondition(application, "SecurityPolicyReady")
	policyReports, policyErr := client.List(ctx, policyReportGVR, namespace, "app.kubernetes.io/instance="+name)
	if policyErr != nil {
		add("AdmissionAndPolicy", "Unknown", ErrorMessage(policyErr), "Verify Kyverno availability and PolicyReport permissions.", "kubernetes://"+namespace+"/policyreports")
	} else if security.Status == "Fail" {
		add("AdmissionAndPolicy", "Fail", security.Details, conditionRemediation("SecurityPolicyReady"), "kubernetes://"+namespace+"/policyreports", "kubernetes://"+namespace+"/applications/"+name)
	} else {
		add("AdmissionAndPolicy", security.Status, fmt.Sprintf("%s; %d matching PolicyReport(s)", security.Details, len(policyReports)), conditionRemediation("SecurityPolicyReady"), "kubernetes://"+namespace+"/policyreports")
	}

	strategy, _, _ := unstructured.NestedString(application.Object, "spec", "deployment", "strategy")
	workloadGVR, workloadKind := deploymentGVR, "deployments"
	if strategy == "canary" {
		workloadGVR, workloadKind = rolloutGVR, "rollouts"
	}
	workload, workloadErr := client.Get(ctx, workloadGVR, namespace, name)
	replicaSets, replicaSetErr := client.List(ctx, replicaSetGVR, namespace, "app.kubernetes.io/instance="+name)
	pods, podErr := client.List(ctx, podGVR, namespace, "app.kubernetes.io/instance="+name)
	if workloadErr != nil {
		add("WorkloadAndRollout", "Fail", ErrorMessage(workloadErr), "Inspect operator reconciliation and generated workload ownership.", "kubernetes://"+namespace+"/"+workloadKind+"/"+name)
	} else if replicaSetErr != nil || podErr != nil {
		add("WorkloadAndRollout", "Unknown", "ReplicaSet or Pod state could not be inspected", "Verify workload read permissions and API availability.", "kubernetes://"+namespace+"/replicasets", "kubernetes://"+namespace+"/pods")
	} else if len(pods) == 0 {
		add("WorkloadAndRollout", "Fail", fmt.Sprintf("workload has %d ReplicaSet(s) but no Pods", len(replicaSets)), "Inspect image admission, scheduling, ReplicaSets, and workload events.", "kubernetes://"+namespace+"/replicasets", "kubernetes://"+namespace+"/pods")
	} else if strategy == "canary" {
		phase, _, _ := unstructured.NestedString(workload.Object, "status", "phase")
		analyses, analysisErr := client.List(ctx, analysisRunGVR, namespace, "app.kubernetes.io/instance="+name)
		if analysisErr != nil {
			add("WorkloadAndRollout", "Unknown", ErrorMessage(analysisErr), "Inspect Rollout and AnalysisRun RBAC.", "kubernetes://"+namespace+"/rollouts/"+name)
		} else if phase == "Degraded" {
			add("WorkloadAndRollout", "Fail", fmt.Sprintf("canary Rollout is Degraded with %d ReplicaSet(s), %d Pod(s), and %d AnalysisRun(s)", len(replicaSets), len(pods), len(analyses)), "Run platformctl app rollout, then submit a recovery Git change.", "kubernetes://"+namespace+"/rollouts/"+name)
		} else {
			add("WorkloadAndRollout", "Pass", fmt.Sprintf("canary Rollout phase=%s with %d ReplicaSet(s), %d Pod(s), and %d AnalysisRun(s)", phase, len(replicaSets), len(pods), len(analyses)), "", "kubernetes://"+namespace+"/rollouts/"+name)
		}
	} else {
		available, _, _ := unstructured.NestedInt64(workload.Object, "status", "availableReplicas")
		if available < 1 {
			add("WorkloadAndRollout", "Fail", "rolling Deployment has no available replicas", "Inspect ReplicaSets, Pods, events, and image admission.", "kubernetes://"+namespace+"/deployments/"+name)
		} else {
			add("WorkloadAndRollout", "Pass", fmt.Sprintf("rolling Deployment has %d available replica(s), %d ReplicaSet(s), and %d Pod(s)", available, len(replicaSets), len(pods)), "", "kubernetes://"+namespace+"/deployments/"+name)
		}
	}

	route, routeErr := client.Get(ctx, httpRouteGVR, namespace, name)
	if routeErr != nil {
		add("GatewayAndEndpoints", "Fail", ErrorMessage(routeErr), "Inspect Gateway, HTTPRoute, Services, and EndpointSlices.", "kubernetes://"+namespace+"/httproutes/"+name)
	} else {
		state, details := routeHealth(route)
		serviceHealth := doctorCondition(application, "ServiceHealth")
		if serviceHealth.Status == "Fail" {
			state, details = "Fail", details+"; "+serviceHealth.Details
		}
		add("GatewayAndEndpoints", state, details, "Inspect Gateway, HTTPRoute, Services, and EndpointSlices.", "kubernetes://"+namespace+"/httproutes/"+name)
	}

	metrics, _, _ := unstructured.NestedBool(application.Object, "spec", "observability", "metrics")
	if !metrics {
		add("MetricsSLOAndAlerts", "Pass", "metrics are intentionally disabled by desired state", "", "git://"+name+"/spec.observability.metrics")
	} else if _, err := client.ServiceProxy(ctx, "monitoring-kube-prometheus-prometheus", 9090, "/api/v1/query", url.Values{"query": []string{fmt.Sprintf(`up{namespace=%q}`, namespace)}}); err != nil {
		add("MetricsSLOAndAlerts", "Unknown", ErrorMessage(err), "Inspect Prometheus readiness, ServiceMonitor discovery, and SLO rules.", "prometheus://query")
	} else {
		add("MetricsSLOAndAlerts", "Pass", "Prometheus query path is available for application SLO and alert inspection", "", "prometheus://query")
	}

	logsEnabled, _, _ := unstructured.NestedBool(application.Object, "spec", "observability", "logs")
	tracesEnabled, _, _ := unstructured.NestedBool(application.Object, "spec", "observability", "traces")
	telemetryStatus, telemetryDetails := "Pass", "logs and traces are intentionally disabled by desired state"
	telemetryEvidence := []string{"git://" + name + "/spec.observability"}
	if logsEnabled || tracesEnabled {
		telemetryDetails = "enabled telemetry backends are queryable"
		if logsEnabled {
			if _, err := client.ServiceProxy(ctx, "loki", 3100, "/loki/api/v1/query_range", url.Values{"query": []string{fmt.Sprintf(`{namespace=%q,application=%q}`, namespace, name)}, "limit": []string{"1"}}); err != nil {
				telemetryStatus, telemetryDetails = "Unknown", "Loki query failed: "+ErrorMessage(err)
			}
			telemetryEvidence = append(telemetryEvidence, "loki://query_range")
		}
		if tracesEnabled {
			if _, err := client.ServiceProxy(ctx, "tempo", 3200, "/api/search", url.Values{"tags": []string{fmt.Sprintf("service.name=%s service.namespace=%s", name, namespace)}, "limit": []string{"1"}}); err != nil {
				telemetryStatus, telemetryDetails = "Unknown", strings.TrimSpace(telemetryDetails+"; Tempo query failed: "+ErrorMessage(err))
			}
			telemetryEvidence = append(telemetryEvidence, "tempo://search")
		}
	}
	add("LogsAndTraces", telemetryStatus, telemetryDetails, "Inspect Alloy, OTel Collector, Loki, and Tempo health and labels.", telemetryEvidence...)

	databaseName, _, _ := unstructured.NestedString(application.Object, "spec", "databaseRef", "name")
	if databaseName == "" {
		add("DatabaseAndBackups", "Pass", "no Database is attached", "", "git://"+name+"/spec.databaseRef")
	} else if database, err := client.Get(ctx, databaseGVR, namespace, databaseName); err != nil {
		add("DatabaseAndBackups", "Fail", ErrorMessage(err), "Run platformctl database status and verify the recovery or backup store.", "kubernetes://"+namespace+"/databases/"+databaseName)
	} else {
		databaseReady := doctorCondition(database, "Ready")
		backups, backupErr := client.List(ctx, backupGVR, namespace, "steadystate.dev/database="+databaseName)
		if backupErr != nil {
			add("DatabaseAndBackups", "Unknown", ErrorMessage(backupErr), "Inspect CloudNativePG and Barman backup permissions.", "kubernetes://"+namespace+"/backups")
		} else {
			add("DatabaseAndBackups", databaseReady.Status, fmt.Sprintf("%s; %d Backup object(s)", databaseReady.Details, len(backups)), conditionRemediation("DatabaseReady"), "kubernetes://"+namespace+"/databases/"+databaseName, "kubernetes://"+namespace+"/backups")
		}
	}
	return checks
}

func doctorCondition(object *unstructured.Unstructured, conditionType string) DoctorCheck {
	status, reason, message := conditionStatus(object, conditionType)
	state := "Fail"
	switch status {
	case "True":
		state = "Pass"
	case "Unknown":
		state = "Warning"
	}
	return DoctorCheck{Name: conditionType, Status: state, Details: strings.TrimSpace(reason + ": " + message)}
}

func nestedInt64(object *unstructured.Unstructured, fields ...string) int64 {
	value, _, _ := unstructured.NestedInt64(object.Object, fields...)
	return value
}
