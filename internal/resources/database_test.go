package resources

import (
	"encoding/json"
	"strings"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
)

func TestDatabaseResourcesPreserveArchiveBoundary(t *testing.T) {
	database := testDatabase("orders")
	store := DatabaseObjectStore(database, "")
	if _, found, _ := unstructuredField(store.Object, "spec", "serverName"); found {
		t.Fatal("ObjectStore.spec.serverName must remain absent")
	}
	destination, _, _ := unstructuredString(store.Object, "spec", "configuration", "destinationPath")
	if destination != "s3://steadystate-backups/" {
		t.Fatalf("destinationPath = %q", destination)
	}
	cluster := DatabaseCluster(database)
	image, _, _ := unstructuredString(cluster.Object, "spec", "imageName")
	if image != "ghcr.io/cloudnative-pg/postgresql:18.4-system-trixie@sha256:1e6adb18ff3d5a538ff8fcc422c47652cc3b2cc133d5c87b6fd306660f36ffe9" {
		t.Fatalf("PostgreSQL image is not the frozen tag-and-digest reference: %q", image)
	}
	serverName, _, _ := unstructuredString(cluster.Object, "spec", "plugins", "0", "parameters", "serverName")
	if serverName != DatabaseBackupServerName(database) {
		t.Fatalf("write serverName = %q, want %q", serverName, DatabaseBackupServerName(database))
	}
}

func TestDatabaseUnstructuredResourcesAreDeepCopySafe(t *testing.T) {
	database := testDatabase("orders")
	for name, object := range map[string]*unstructured.Unstructured{
		"object store":     DatabaseObjectStore(database, ""),
		"cluster":          DatabaseCluster(database),
		"scheduled backup": DatabaseScheduledBackup(database),
		"final backup":     DatabaseFinalBackup(database),
		"service monitor":  DatabaseServiceMonitor(database),
		"prometheus rule":  DatabasePrometheusRule(database),
	} {
		t.Run(name, func(t *testing.T) {
			if copy := object.DeepCopy(); copy.GetName() != object.GetName() {
				t.Fatal("deep copy did not preserve resource identity")
			}
		})
	}
}

func TestRecoveredDatabaseReadsOldAndWritesNewArchive(t *testing.T) {
	database := testDatabase("orders")
	database.Spec.Recovery = &platformv1alpha1.DatabaseRecovery{SourceServerName: "orders-old-archive"}
	cluster := DatabaseCluster(database)
	recoveryServer, _, _ := unstructuredString(cluster.Object, "spec", "externalClusters", "0", "plugin", "parameters", "serverName")
	if recoveryServer != "orders-old-archive" {
		t.Fatalf("recovery serverName = %q", recoveryServer)
	}
	writeServer, _, _ := unstructuredString(cluster.Object, "spec", "plugins", "0", "parameters", "serverName")
	if writeServer == recoveryServer {
		t.Fatal("recovery reused the source archive for new writes")
	}
}

func TestBackupsDisabledRemovesArchiveConfiguration(t *testing.T) {
	database := testDatabase("orders")
	database.Spec.Backups.Enabled = false
	cluster := DatabaseCluster(database)
	if _, found, _ := unstructuredField(cluster.Object, "spec", "plugins"); found {
		t.Fatal("backups-disabled Cluster still contains a Barman WAL plugin")
	}
	policies := DatabaseNetworkPolicies(database, "172.30.240.10/32")
	if len(policies) != 2 {
		t.Fatalf("backups-disabled Database generated %d NetworkPolicies, want app and monitoring only", len(policies))
	}
	for _, policy := range policies {
		if strings.Contains(policy.Name, "backup") {
			t.Fatalf("backups-disabled Database retained backup egress: %s", policy.Name)
		}
	}
}

func TestDatabaseBackupEgressIsLeastPrivilegeAndOperational(t *testing.T) {
	database := testDatabase("orders")
	policy := DatabaseBackupNetworkPolicy(database, "172.30.240.10/32")
	if len(policy.Spec.Egress) != 4 {
		t.Fatalf("backup egress has %d rules, want S3, API service, API endpoint, and cluster peers", len(policy.Spec.Egress))
	}
	assertEgressIPPort(t, policy.Spec.Egress[0], "172.30.240.10/32", 8333)
	assertEgressIPPort(t, policy.Spec.Egress[1], KubernetesAPIServiceCIDR, 443)
	apiRule := policy.Spec.Egress[2]
	if len(apiRule.To) != len(KubernetesAPIEndpointCIDRs) || len(apiRule.Ports) != 1 || apiRule.Ports[0].Port.IntVal != 6443 {
		t.Fatalf("API endpoint egress is not restricted to private control-plane port 6443: %#v", apiRule)
	}
	for index, cidr := range KubernetesAPIEndpointCIDRs {
		if apiRule.To[index].IPBlock == nil || apiRule.To[index].IPBlock.CIDR != cidr {
			t.Fatalf("API endpoint peer %d does not match %s: %#v", index, cidr, apiRule.To[index])
		}
	}
	peer := policy.Spec.Egress[3]
	if peer.To[0].PodSelector.MatchLabels["cnpg.io/cluster"] != DatabaseClusterName(database) ||
		len(peer.Ports) != 2 || peer.Ports[0].Port.IntVal != 5432 || peer.Ports[1].Port.IntVal != 8000 {
		t.Fatalf("CNPG peer egress is not restricted to database and manager ports: %#v", peer)
	}
}

func assertEgressIPPort(t *testing.T, rule networkingv1.NetworkPolicyEgressRule, cidr string, port int32) {
	t.Helper()
	if len(rule.To) != 1 || rule.To[0].IPBlock == nil || rule.To[0].IPBlock.CIDR != cidr ||
		len(rule.Ports) != 1 || rule.Ports[0].Port == nil || rule.Ports[0].Port.IntVal != port {
		t.Fatalf("egress rule does not match %s:%d: %#v", cidr, port, rule)
	}
}

func TestDatabaseNamesAreSuffixSafe(t *testing.T) {
	database := testDatabase(strings.Repeat("a", 63))
	for name, value := range map[string]string{
		"cluster": DatabaseClusterName(database), "connection": DatabaseConnectionSecretName(database),
		"credential": DatabaseBackupCredentialName(database), "objectStore": DatabaseObjectStoreName(database),
		"scheduled": DatabaseScheduledBackupName(database), "final": DatabaseFinalBackupName(database),
	} {
		if len(value) > 63 {
			t.Errorf("%s name has %d characters: %s", name, len(value), value)
		}
	}
	if got, want := DatabaseConnectionSecretName(database), DatabaseClusterName(database)+"-app"; got != want {
		t.Fatalf("connection Secret name = %q, want CNPG contract %q", got, want)
	}
}

func TestDatabaseBackupAlertsUseControllerObservedHealth(t *testing.T) {
	database := testDatabase("orders")
	encoded, err := json.Marshal(DatabasePrometheusRule(database).Object)
	if err != nil {
		t.Fatal(err)
	}
	rendered := string(encoded)
	for _, required := range []string{
		"SteadyStateDatabaseBackupStale",
		"SteadyStateDatabaseBackupFailed",
		"steadystate_database_backup_healthy",
		"steadystate_database_last_successful_backup_timestamp_seconds",
	} {
		if !strings.Contains(rendered, required) {
			t.Errorf("Database backup alerts are missing %q", required)
		}
	}
	if strings.Contains(rendered, "cnpg_collector_last_") {
		t.Fatal("Database backup alerts use deprecated CNPG plugin metrics")
	}
}

func testDatabase(name string) *platformv1alpha1.Database {
	return &platformv1alpha1.Database{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "team-payments", UID: types.UID("01234567-89ab-cdef-0123-456789abcdef")},
		Spec: platformv1alpha1.DatabaseSpec{
			Engine: "postgres", Instances: 1,
			Storage: platformv1alpha1.DatabaseStorage{Size: "1Gi"},
			Backups: platformv1alpha1.DatabaseBackups{Enabled: true, Schedule: "0 0 2 * * *", Retention: "7d"},
		},
	}
}

func unstructuredField(object map[string]any, fields ...string) (any, bool, error) {
	current := any(object)
	for _, field := range fields {
		switch typed := current.(type) {
		case map[string]any:
			value, found := typed[field]
			if !found {
				return nil, false, nil
			}
			current = value
		case []any:
			index := int(field[0] - '0')
			if index < 0 || index >= len(typed) {
				return nil, false, nil
			}
			current = typed[index]
		default:
			return nil, false, nil
		}
	}
	return current, true, nil
}

func unstructuredString(object map[string]any, fields ...string) (string, bool, error) {
	value, found, err := unstructuredField(object, fields...)
	if !found || err != nil {
		return "", found, err
	}
	text, ok := value.(string)
	return text, ok, nil
}
