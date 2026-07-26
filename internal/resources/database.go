package resources

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
)

const (
	DatabaseLabelKey            = "steadystate.dev/database"
	DatabaseUIDAnnotationKey    = "steadystate.dev/database-uid"
	DefaultBackupStoreEndpoint  = "http://172.30.240.10:8333"
	BackupStoreSourceSecretName = "steadystate-backup-store"
	BackupStoreSourceNamespace  = "cnpg-system"
	BackupStoreBucket           = "steadystate-backups"
	BarmanPluginName            = "barman-cloud.cloudnative-pg.io"
	PostgreSQLImage             = "ghcr.io/cloudnative-pg/postgresql@sha256:1e6adb18ff3d5a538ff8fcc422c47652cc3b2cc133d5c87b6fd306660f36ffe9"
)

var (
	CNPGClusterGVK       = schema.GroupVersionKind{Group: "postgresql.cnpg.io", Version: "v1", Kind: "Cluster"}
	CNPGBackupGVK        = schema.GroupVersionKind{Group: "postgresql.cnpg.io", Version: "v1", Kind: "Backup"}
	CNPGScheduledGVK     = schema.GroupVersionKind{Group: "postgresql.cnpg.io", Version: "v1", Kind: "ScheduledBackup"}
	BarmanObjectStoreGVK = schema.GroupVersionKind{Group: "barmancloud.cnpg.io", Version: "v1", Kind: "ObjectStore"}
)

// DatabaseLabels returns stable labels for every Database-owned child.
func DatabaseLabels(database *platformv1alpha1.Database) map[string]string {
	return map[string]string{
		"app.kubernetes.io/instance":   database.Name,
		"app.kubernetes.io/managed-by": ManagedBy,
		"app.kubernetes.io/name":       "steadystate-database",
		"app.kubernetes.io/part-of":    "steadystate",
		DatabaseLabelKey:               database.Name,
	}
}

func DatabaseAnnotations(database *platformv1alpha1.Database) map[string]string {
	return map[string]string{DatabaseUIDAnnotationKey: string(database.UID)}
}

func DatabaseClusterName(database *platformv1alpha1.Database) string {
	return DatabaseClusterNameFor(database.Name)
}

func DatabaseClusterNameFor(name string) string {
	// CNPG creates a connection Secret named <cluster>-app. Reserve those four
	// characters here so both names remain valid and exactly predictable.
	return suffixedNameWithLimit(name, "-postgres", 59)
}

func DatabaseConnectionSecretName(database *platformv1alpha1.Database) string {
	return DatabaseConnectionSecretNameFor(database.Name)
}

func DatabaseConnectionSecretNameFor(name string) string {
	return DatabaseClusterNameFor(name) + "-app"
}

func DatabaseBackupCredentialName(database *platformv1alpha1.Database) string {
	return suffixedName(database.Name, "-backup-credentials")
}

func DatabaseObjectStoreName(database *platformv1alpha1.Database) string {
	return suffixedName(database.Name, "-object-store")
}

func DatabaseScheduledBackupName(database *platformv1alpha1.Database) string {
	return suffixedName(database.Name, "-scheduled")
}

func DatabaseFinalBackupName(database *platformv1alpha1.Database) string {
	return suffixedName(database.Name, "-final")
}

func DatabaseBackupServerName(database *platformv1alpha1.Database) string {
	uid := string(database.UID)
	if len(uid) > 12 {
		uid = uid[:12]
	}
	return suffixedName(database.Name, "-"+uid)
}

func databaseUnstructured(database *platformv1alpha1.Database, gvk schema.GroupVersionKind, name string, spec map[string]any) *unstructured.Unstructured {
	object := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": gvk.GroupVersion().String(),
		"kind":       gvk.Kind,
		"metadata": map[string]any{
			"name":        name,
			"namespace":   database.Namespace,
			"labels":      stringMapAny(DatabaseLabels(database)),
			"annotations": stringMapAny(DatabaseAnnotations(database)),
		},
		"spec": spec,
	}}
	object.SetGroupVersionKind(gvk)
	return object
}

// DatabaseObjectStore builds the Barman destination. serverName is deliberately omitted.
func DatabaseObjectStore(database *platformv1alpha1.Database, endpoint string) *unstructured.Unstructured {
	if endpoint == "" {
		endpoint = DefaultBackupStoreEndpoint
	}
	spec := map[string]any{
		"configuration": map[string]any{
			"destinationPath": fmt.Sprintf("s3://%s/", BackupStoreBucket),
			"endpointURL":     endpoint,
			"s3Credentials": map[string]any{
				"accessKeyId":     map[string]any{"name": DatabaseBackupCredentialName(database), "key": "ACCESS_KEY_ID"},
				"secretAccessKey": map[string]any{"name": DatabaseBackupCredentialName(database), "key": "ACCESS_SECRET_KEY"},
			},
			"wal": map[string]any{"compression": "gzip"},
		},
		"retentionPolicy": database.Spec.Backups.Retention,
		"instanceSidecarConfiguration": map[string]any{
			"env": []any{
				map[string]any{"name": "AWS_REQUEST_CHECKSUM_CALCULATION", "value": "when_required"},
				map[string]any{"name": "AWS_RESPONSE_CHECKSUM_VALIDATION", "value": "when_required"},
			},
			"resources": map[string]any{
				"requests": map[string]any{"cpu": "25m", "memory": "64Mi"},
				"limits":   map[string]any{"cpu": "200m", "memory": "256Mi"},
			},
		},
	}
	return databaseUnstructured(database, BarmanObjectStoreGVK, DatabaseObjectStoreName(database), spec)
}

// DatabaseCluster builds a CNPG Cluster with plugin-based WAL archiving or recovery.
func DatabaseCluster(database *platformv1alpha1.Database) *unstructured.Unstructured {
	spec := map[string]any{
		"instances":  int64(database.Spec.Instances),
		"imageName":  PostgreSQLImage,
		"storage":    map[string]any{"size": database.Spec.Storage.Size},
		"monitoring": map[string]any{"enablePodMonitor": true},
		"resources": map[string]any{
			"requests": map[string]any{"cpu": "100m", "memory": "256Mi"},
			"limits":   map[string]any{"cpu": "500m", "memory": "1Gi"},
		},
	}
	if database.Spec.Storage.StorageClassName != nil {
		spec["storage"].(map[string]any)["storageClass"] = *database.Spec.Storage.StorageClassName
	}
	if database.Spec.Backups.Enabled {
		spec["plugins"] = []any{map[string]any{
			"name":          BarmanPluginName,
			"isWALArchiver": true,
			"parameters": map[string]any{
				"barmanObjectName": DatabaseObjectStoreName(database),
				"serverName":       DatabaseBackupServerName(database),
			},
		}}
	}
	if database.Spec.Recovery == nil {
		spec["bootstrap"] = map[string]any{"initdb": map[string]any{}}
	} else {
		recovery := map[string]any{"source": "steadystate-recovery"}
		if database.Spec.Recovery.TargetTime != nil {
			recovery["recoveryTarget"] = map[string]any{
				"targetTime": database.Spec.Recovery.TargetTime.UTC().Format("2006-01-02 15:04:05.000000+00"),
			}
		}
		spec["bootstrap"] = map[string]any{"recovery": recovery}
		spec["externalClusters"] = []any{map[string]any{
			"name": "steadystate-recovery",
			"plugin": map[string]any{
				"name": BarmanPluginName,
				"parameters": map[string]any{
					"barmanObjectName": DatabaseObjectStoreName(database),
					"serverName":       database.Spec.Recovery.SourceServerName,
				},
			},
		}}
	}
	return databaseUnstructured(database, CNPGClusterGVK, DatabaseClusterName(database), spec)
}

func DatabaseScheduledBackup(database *platformv1alpha1.Database) *unstructured.Unstructured {
	return databaseUnstructured(database, CNPGScheduledGVK, DatabaseScheduledBackupName(database), map[string]any{
		"schedule":             database.Spec.Backups.Schedule,
		"cluster":              map[string]any{"name": DatabaseClusterName(database)},
		"method":               "plugin",
		"pluginConfiguration":  map[string]any{"name": BarmanPluginName},
		"backupOwnerReference": "self",
		"immediate":            true,
	})
}

func DatabaseFinalBackup(database *platformv1alpha1.Database) *unstructured.Unstructured {
	return databaseUnstructured(database, CNPGBackupGVK, DatabaseFinalBackupName(database), map[string]any{
		"cluster":             map[string]any{"name": DatabaseClusterName(database)},
		"method":              "plugin",
		"pluginConfiguration": map[string]any{"name": BarmanPluginName},
	})
}

func DatabaseBackupCredential(database *platformv1alpha1.Database, source *corev1.Secret) *corev1.Secret {
	data := map[string][]byte{}
	for _, key := range []string{"ACCESS_KEY_ID", "ACCESS_SECRET_KEY"} {
		if value, found := source.Data[key]; found {
			data[key] = append([]byte(nil), value...)
		}
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        DatabaseBackupCredentialName(database),
			Namespace:   database.Namespace,
			Labels:      DatabaseLabels(database),
			Annotations: DatabaseAnnotations(database),
		},
		Type: corev1.SecretTypeOpaque,
		Data: data,
	}
}

func DatabaseNetworkPolicies(database *platformv1alpha1.Database, endpointCIDR string) []*networkingv1.NetworkPolicy {
	labels := DatabaseLabels(database)
	clusterSelector := metav1.LabelSelector{MatchLabels: map[string]string{"cnpg.io/cluster": DatabaseClusterName(database)}}
	appSelector := metav1.LabelSelector{MatchLabels: map[string]string{DatabaseLabelKey: database.Name}}
	monitoringNamespace := metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "monitoring"}}
	prometheusSelector := metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/name": "prometheus"}}
	tcp := corev1.ProtocolTCP
	policies := []*networkingv1.NetworkPolicy{
		{
			ObjectMeta: metav1.ObjectMeta{Name: suffixedName(database.Name, "-allow-app"), Namespace: database.Namespace, Labels: labels, Annotations: DatabaseAnnotations(database)},
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: clusterSelector,
				PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
				Ingress: []networkingv1.NetworkPolicyIngressRule{{
					From:  []networkingv1.NetworkPolicyPeer{{PodSelector: &appSelector}},
					Ports: []networkingv1.NetworkPolicyPort{{Protocol: &tcp, Port: ptrIntOrString(intstr.FromInt32(5432))}},
				}},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: suffixedName(database.Name, "-allow-monitoring"), Namespace: database.Namespace, Labels: labels, Annotations: DatabaseAnnotations(database)},
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: clusterSelector,
				PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
				Ingress: []networkingv1.NetworkPolicyIngressRule{{
					From:  []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &monitoringNamespace, PodSelector: &prometheusSelector}},
					Ports: []networkingv1.NetworkPolicyPort{{Protocol: &tcp, Port: ptrIntOrString(intstr.FromInt32(9187))}},
				}},
			},
		},
	}
	if database.Spec.Backups.Enabled {
		policies = append(policies, DatabaseBackupNetworkPolicy(database, endpointCIDR))
	}
	return policies
}

func DatabaseBackupNetworkPolicy(database *platformv1alpha1.Database, endpointCIDR string) *networkingv1.NetworkPolicy {
	tcp := corev1.ProtocolTCP
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: suffixedName(database.Name, "-allow-backup"), Namespace: database.Namespace, Labels: DatabaseLabels(database), Annotations: DatabaseAnnotations(database)},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"cnpg.io/cluster": DatabaseClusterName(database)}},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
			Egress: []networkingv1.NetworkPolicyEgressRule{{
				To:    []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: endpointCIDR}}},
				Ports: []networkingv1.NetworkPolicyPort{{Protocol: &tcp, Port: ptrIntOrString(intstr.FromInt32(8333))}},
			}},
		},
	}
}

func ptrIntOrString(value intstr.IntOrString) *intstr.IntOrString { return &value }

func DatabaseServiceMonitor(database *platformv1alpha1.Database) *unstructured.Unstructured {
	return databaseUnstructured(database, schema.GroupVersionKind{Group: "monitoring.coreos.com", Version: "v1", Kind: "ServiceMonitor"}, suffixedName(database.Name, "-monitor"), map[string]any{
		"selector":  map[string]any{"matchLabels": map[string]any{"cnpg.io/cluster": DatabaseClusterName(database)}},
		"endpoints": []any{map[string]any{"port": "metrics", "interval": "15s"}},
	})
}

func DatabasePrometheusRule(database *platformv1alpha1.Database) *unstructured.Unstructured {
	return databaseUnstructured(database, schema.GroupVersionKind{Group: "monitoring.coreos.com", Version: "v1", Kind: "PrometheusRule"}, suffixedName(database.Name, "-alerts"), map[string]any{
		"groups": []any{map[string]any{
			"name": fmt.Sprintf("steadystate.database.%s", database.Name),
			"rules": []any{
				map[string]any{
					"alert": "SteadyStateDatabaseBackupStale",
					"expr": fmt.Sprintf(
						"steadystate_database_backup_healthy{namespace=%q,database=%q} == 0 or time() - steadystate_database_last_successful_backup_timestamp_seconds{namespace=%q,database=%q} > 90000",
						database.Namespace, database.Name, database.Namespace, database.Name,
					),
					"for":         "1m",
					"labels":      map[string]any{"severity": "warning"},
					"annotations": map[string]any{"summary": "Database backup is older than 25 hours"},
				},
				map[string]any{
					"alert":       "SteadyStateDatabaseBackupFailed",
					"expr":        fmt.Sprintf("steadystate_database_backup_healthy{namespace=%q,database=%q} == 0", database.Namespace, database.Name),
					"for":         "1m",
					"labels":      map[string]any{"severity": "critical"},
					"annotations": map[string]any{"summary": "Database backup is failing"},
				},
			},
		}},
	})
}

func DatabaseOwnerReference(database *platformv1alpha1.Database) metav1.OwnerReference {
	controller, block := true, true
	return metav1.OwnerReference{
		APIVersion: platformv1alpha1.GroupVersion.String(),
		Kind:       "Database", Name: database.Name, UID: types.UID(database.UID),
		Controller: &controller, BlockOwnerDeletion: &block,
	}
}
