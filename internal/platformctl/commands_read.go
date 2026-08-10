package platformctl

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func newTeamCommand(options *Options) *cobra.Command {
	command := &cobra.Command{Use: "team", Short: "Read SteadyState Teams"}
	command.AddCommand(&cobra.Command{
		Use: "list", Short: "List Teams from the Git catalog", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			_, selected, err := options.loadContext()
			if err != nil {
				return err
			}
			catalog, err := LoadCatalog(selected.CheckoutPath)
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(catalog.Tenants))
			for _, tenant := range catalog.SortedTenants() {
				rows = append(rows, []string{tenant.Name, "team-" + tenant.Name, fmt.Sprint(len(tenant.Applications)), fmt.Sprint(len(tenant.Databases))})
			}
			return options.printer().Table([]string{"NAME", "NAMESPACE", "APPLICATIONS", "DATABASES"}, rows, catalog.SortedTenants())
		},
	})
	command.AddCommand(resourceStatusCommand(options, "status NAME", "Show Team status", teamGVR, "Team", false))
	addWriteCommands(command, nil, nil, options)
	return command
}

func newApplicationCommand(options *Options) *cobra.Command {
	command := &cobra.Command{Use: "app", Short: "Read SteadyState Applications"}
	var listNamespace string
	list := &cobra.Command{
		Use: "list", Short: "List Applications", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return listResources(cmd, options, applicationGVR, "Application", listNamespace)
		},
	}
	list.Flags().StringVarP(&listNamespace, "namespace", "n", "", "Team namespace (all namespaces when omitted)")
	command.AddCommand(list)
	command.AddCommand(namespacedStatusCommand(options, "status NAME", "Show Application status", applicationGVR, "Application"))
	command.AddCommand(newApplicationLogsCommand(options))
	command.AddCommand(newApplicationTracesCommand(options))
	command.AddCommand(newApplicationProvenanceCommand(options))
	command.AddCommand(newApplicationSLOCommand(options))
	command.AddCommand(newApplicationPolicyCommand(options))
	command.AddCommand(newApplicationRolloutCommand(options))
	command.AddCommand(newApplicationDoctorCommand(options))
	command.AddCommand(newBreakGlassCommand(options, "promote"))
	command.AddCommand(newBreakGlassCommand(options, "abort"))
	addWriteCommands(nil, command, nil, options)
	return command
}

func newDatabaseCommand(options *Options) *cobra.Command {
	command := &cobra.Command{Use: "database", Short: "Read SteadyState Databases"}
	command.AddCommand(namespacedStatusCommand(options, "status NAME", "Show Database status", databaseGVR, "Database"))
	var namespace string
	backups := &cobra.Command{
		Use: "backups NAME", Short: "List CloudNativePG backups", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			selected, resolvedNamespace, client, ctx, cancel, err := commandClusterContext(cmd, options, namespace, "", args[0])
			_ = selected
			if err != nil {
				return err
			}
			defer cancel()
			items, err := client.List(ctx, backupGVR, resolvedNamespace, "steadystate.dev/database="+args[0])
			if err != nil {
				return err
			}
			return printObjectList(options, "Backup", items)
		},
	}
	backups.Flags().StringVarP(&namespace, "namespace", "n", "", "Team namespace")
	command.AddCommand(backups)
	addWriteCommands(nil, nil, command, options)
	return command
}

func resourceStatusCommand(options *Options, use, short string, gvr schema.GroupVersionResource, kind string, namespaced bool) *cobra.Command {
	return &cobra.Command{
		Use: use, Short: short, Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, selected, err := options.loadContext()
			if err != nil {
				return err
			}
			ctx, cancel := options.commandContext(cmd.Context())
			defer cancel()
			client, err := NewClusterClient(selected)
			if err != nil {
				return err
			}
			namespace := ""
			if namespaced {
				namespace = "team-" + args[0]
			}
			object, err := client.Get(ctx, gvr, namespace, args[0])
			if err != nil {
				return err
			}
			return printSummary(options, Summarize(kind, object))
		},
	}
}

func namespacedStatusCommand(options *Options, use, short string, gvr schema.GroupVersionResource, kind string) *cobra.Command {
	var namespace string
	command := &cobra.Command{
		Use: use, Short: short, Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, resolvedNamespace, client, ctx, cancel, err := commandClusterContext(cmd, options, namespace, kind, args[0])
			if err != nil {
				return err
			}
			defer cancel()
			object, err := client.Get(ctx, gvr, resolvedNamespace, args[0])
			if err != nil {
				return err
			}
			return printSummary(options, Summarize(kind, object))
		},
	}
	command.Flags().StringVarP(&namespace, "namespace", "n", "", "Team namespace (derived from the catalog when omitted)")
	return command
}

func commandClusterContext(cmd *cobra.Command, options *Options, namespace, kind, name string) (Context, string, *ClusterClient, context.Context, context.CancelFunc, error) {
	_, selected, err := options.loadContext()
	if err != nil {
		return Context{}, "", nil, nil, nil, err
	}
	if namespace == "" && kind != "" {
		catalog, catalogErr := LoadCatalog(selected.CheckoutPath)
		if catalogErr != nil {
			return Context{}, "", nil, nil, nil, catalogErr
		}
		namespace, err = catalogNamespace(catalog, kind, name)
		if err != nil {
			return Context{}, "", nil, nil, nil, err
		}
	}
	ctx, cancel := options.commandContext(cmd.Context())
	client, err := NewClusterClient(selected)
	if err != nil {
		cancel()
		return Context{}, "", nil, nil, nil, err
	}
	return selected, namespace, client, ctx, cancel, nil
}

func catalogNamespace(catalog TenantCatalog, kind, name string) (string, error) {
	for _, tenant := range catalog.Tenants {
		switch kind {
		case "Application":
			for _, item := range tenant.Applications {
				if item.Name == name {
					return "team-" + tenant.Name, nil
				}
			}
		case "Database":
			for _, item := range tenant.Databases {
				if item.Name == name {
					return "team-" + tenant.Name, nil
				}
			}
		}
	}
	return "", exitError(ExitNotFound, "%s %q is not present in the Git catalog", kind, name)
}

func listResources(cmd *cobra.Command, options *Options, gvr schema.GroupVersionResource, kind, namespace string) error {
	_, _, client, ctx, cancel, err := commandClusterContext(cmd, options, namespace, "", "")
	if err != nil {
		return err
	}
	defer cancel()
	items, err := client.List(ctx, gvr, namespace, "")
	if err != nil {
		return err
	}
	summaries := make([]ResourceSummary, 0, len(items))
	for index := range items {
		summaries = append(summaries, Summarize(kind, &items[index]))
	}
	rows := make([][]string, 0, len(summaries))
	for _, item := range summaries {
		rows = append(rows, []string{item.Namespace, item.Name, item.Phase, item.Ready, item.Reason})
	}
	return options.printer().Table([]string{"NAMESPACE", "NAME", "PHASE", "READY", "REASON"}, rows, summaries)
}

func printSummary(options *Options, summary ResourceSummary) error {
	return options.printer().Table([]string{"KIND", "NAMESPACE", "NAME", "PHASE", "READY", "REASON", "GENERATION"}, [][]string{{summary.Kind, summary.Namespace, summary.Name, summary.Phase, summary.Ready, summary.Reason, fmt.Sprintf("%d/%d", summary.ObservedGeneration, summary.Generation)}}, summary)
}

func printObjectList(options *Options, kind string, items []unstructured.Unstructured) error {
	rows := make([][]string, 0, len(items))
	values := make([]map[string]any, 0, len(items))
	for index := range items {
		item := &items[index]
		phase, _, _ := unstructured.NestedString(item.Object, "status", "phase")
		rows = append(rows, []string{item.GetNamespace(), item.GetName(), phase, item.GetCreationTimestamp().String()})
		values = append(values, item.Object)
	}
	return options.printer().Table([]string{"NAMESPACE", "NAME", "PHASE", "CREATED"}, rows, values)
}

func newApplicationLogsCommand(options *Options) *cobra.Command {
	var namespace string
	var follow, historical bool
	var tail int64
	var since time.Duration
	command := &cobra.Command{
		Use: "logs NAME", Short: "Read current or historical application logs", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, resolvedNamespace, client, ctx, cancel, err := commandClusterContext(cmd, options, namespace, "Application", args[0])
			if err != nil {
				return err
			}
			defer cancel()
			if historical {
				query := url.Values{"query": []string{fmt.Sprintf(`{namespace=%q,application=%q}`, resolvedNamespace, args[0])}, "limit": []string{fmt.Sprint(tail)}}
				if since > 0 {
					query.Set("start", fmt.Sprint(time.Now().Add(-since).UnixNano()))
				}
				return printBackendJSON(options, client, ctx, "loki", 3100, "/loki/api/v1/query_range", query)
			}
			stream, pod, err := client.PodLogs(ctx, resolvedNamespace, args[0], follow, tail, since)
			if err != nil {
				return err
			}
			defer func() { _ = stream.Close() }()
			if options.Format == "table" {
				_, err = io.Copy(options.Stdout, stream)
				return err
			}
			data, err := io.ReadAll(io.LimitReader(stream, 8<<20))
			if err != nil {
				return err
			}
			return options.printer().Print(map[string]string{"namespace": resolvedNamespace, "pod": pod, "logs": Redact(string(data))})
		},
	}
	command.Flags().StringVarP(&namespace, "namespace", "n", "", "Team namespace")
	command.Flags().BoolVarP(&follow, "follow", "f", false, "follow the current Pod log stream")
	command.Flags().BoolVar(&historical, "historical", false, "query retained logs from Loki")
	command.Flags().Int64Var(&tail, "tail", 200, "maximum log lines")
	command.Flags().DurationVar(&since, "since", time.Hour, "lookback duration")
	return command
}

func newApplicationTracesCommand(options *Options) *cobra.Command {
	var namespace, traceID string
	var limit int
	const traceIDDescription = "`--trace-id` accepts the canonical 32-character lowercase hexadecimal ID used\n" +
		"in application logs. Tempo's raw OTLP/protobuf JSON response represents its\n" +
		"`traceId` bytes field as Base64; these are two encodings of the same 16 bytes."
	command := &cobra.Command{
		Use:   "traces NAME",
		Short: "Query application traces from Tempo",
		Long:  traceIDDescription,
		Annotations: map[string]string{
			docsDetailsAnnotation: traceIDDescription,
		},
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, resolvedNamespace, client, ctx, cancel, err := commandClusterContext(cmd, options, namespace, "Application", args[0])
			if err != nil {
				return err
			}
			defer cancel()
			path, query := "/api/search", url.Values{"tags": []string{fmt.Sprintf("service.name=%s service.namespace=%s", args[0], resolvedNamespace)}, "limit": []string{fmt.Sprint(limit)}}
			if traceID != "" {
				path, query = "/api/traces/"+url.PathEscape(traceID), url.Values{}
			}
			return printBackendJSON(options, client, ctx, "tempo", 3200, path, query)
		},
	}
	command.Flags().StringVarP(&namespace, "namespace", "n", "", "Team namespace")
	command.Flags().StringVar(&traceID, "trace-id", "", "fetch one exact trace ID")
	command.Flags().IntVar(&limit, "limit", 20, "maximum trace results")
	return command
}

func newApplicationProvenanceCommand(options *Options) *cobra.Command {
	var namespace string
	command := &cobra.Command{
		Use: "provenance NAME", Short: "Show active image and Git provenance", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, ns, client, ctx, cancel, err := commandClusterContext(cmd, options, namespace, "Application", args[0])
			if err != nil {
				return err
			}
			defer cancel()
			object, err := client.Get(ctx, applicationGVR, ns, args[0])
			if err != nil {
				return err
			}
			status, _, _ := unstructured.NestedMap(object.Object, "status")
			value := map[string]any{"namespace": ns, "name": args[0], "activeVersion": status["activeVersion"], "candidateVersion": status["candidateVersion"], "resolvedImageDigest": status["resolvedImageDigest"], "resolvedGitRevision": status["resolvedGitRevision"]}
			return options.printer().Table([]string{"NAMESPACE", "NAME", "ACTIVE", "CANDIDATE", "DIGEST", "REVISION"}, [][]string{{ns, args[0], stringValue(status["activeVersion"]), stringValue(status["candidateVersion"]), stringValue(status["resolvedImageDigest"]), stringValue(status["resolvedGitRevision"])}}, value)
		},
	}
	command.Flags().StringVarP(&namespace, "namespace", "n", "", "Team namespace")
	return command
}

func newApplicationSLOCommand(options *Options) *cobra.Command {
	var namespace string
	command := &cobra.Command{
		Use: "slo NAME", Short: "Query application SLO and alert series", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, ns, client, ctx, cancel, err := commandClusterContext(cmd, options, namespace, "Application", args[0])
			if err != nil {
				return err
			}
			defer cancel()
			query := fmt.Sprintf(`{namespace=%q,application=%q} and on() ({__name__=~"steadystate:.*|ALERTS"})`, ns, args[0])
			return printBackendJSON(options, client, ctx, "monitoring-kube-prometheus-prometheus", 9090, "/api/v1/query", url.Values{"query": []string{query}})
		},
	}
	command.Flags().StringVarP(&namespace, "namespace", "n", "", "Team namespace")
	return command
}

func newApplicationPolicyCommand(options *Options) *cobra.Command {
	var namespace string
	command := &cobra.Command{
		Use: "policy NAME", Short: "Show Kyverno policy results", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, ns, client, ctx, cancel, err := commandClusterContext(cmd, options, namespace, "Application", args[0])
			if err != nil {
				return err
			}
			defer cancel()
			items, err := client.List(ctx, policyReportGVR, ns, "app.kubernetes.io/instance="+args[0])
			if err != nil {
				return err
			}
			return printObjectList(options, "PolicyReport", items)
		},
	}
	command.Flags().StringVarP(&namespace, "namespace", "n", "", "Team namespace")
	return command
}

func newApplicationRolloutCommand(options *Options) *cobra.Command {
	var namespace string
	command := &cobra.Command{
		Use: "rollout NAME", Short: "Show Rollout and AnalysisRun state", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, ns, client, ctx, cancel, err := commandClusterContext(cmd, options, namespace, "Application", args[0])
			if err != nil {
				return err
			}
			defer cancel()
			rollout, err := client.Get(ctx, rolloutGVR, ns, args[0])
			if err != nil {
				return err
			}
			analyses, err := client.List(ctx, analysisRunGVR, ns, "app.kubernetes.io/instance="+args[0])
			if err != nil {
				return err
			}
			value := map[string]any{"rollout": rollout.Object, "analysisRuns": objectMaps(analyses)}
			phase, _, _ := unstructured.NestedString(rollout.Object, "status", "phase")
			return options.printer().Table([]string{"NAMESPACE", "ROLLOUT", "PHASE", "ANALYSIS RUNS"}, [][]string{{ns, args[0], phase, fmt.Sprint(len(analyses))}}, value)
		},
	}
	command.Flags().StringVarP(&namespace, "namespace", "n", "", "Team namespace")
	return command
}

func newApplicationDoctorCommand(options *Options) *cobra.Command {
	var namespace string
	command := &cobra.Command{
		Use: "doctor NAME", Short: "Diagnose Application health contracts", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			selected, ns, client, ctx, cancel, err := commandClusterContext(cmd, options, namespace, "Application", args[0])
			if err != nil {
				return err
			}
			defer cancel()
			application, err := client.Get(ctx, applicationGVR, ns, args[0])
			if err != nil {
				return err
			}
			checks := runApplicationDoctor(ctx, selected, client, ns, args[0], application)
			rows, failed := [][]string{}, false
			for _, check := range checks {
				rows = append(rows, []string{check.Status, check.Name, Redact(check.Details), strings.Join(check.Evidence, ", "), check.Remediation})
				failed = failed || check.Status == "Fail"
			}
			if err := options.printer().Table([]string{"STATUS", "CHECK", "DETAILS", "EVIDENCE", "REMEDIATION"}, rows, checks); err != nil {
				return err
			}
			if failed {
				return exitError(ExitUnhealthy, "Application %s/%s has failed health contracts", ns, args[0])
			}
			return nil
		},
	}
	command.Flags().StringVarP(&namespace, "namespace", "n", "", "Team namespace")
	return command
}

func conditionRemediation(condition string) string {
	switch condition {
	case "ServiceHealth":
		return "Run platformctl app rollout and inspect HTTPRoute acceptance."
	case "SecurityPolicyReady":
		return "Run platformctl app policy and verify image signatures."
	case "DatabaseReady":
		return "Run platformctl database status for the referenced Database."
	default:
		return "Run platformctl app status and inspect the reported reason."
	}
}

func routeHealth(route *unstructured.Unstructured) (string, string) {
	parents, _, _ := unstructured.NestedSlice(route.Object, "status", "parents")
	accepted, resolved := false, false
	for _, rawParent := range parents {
		parent, ok := rawParent.(map[string]any)
		if !ok {
			continue
		}
		conditions, _, _ := unstructured.NestedSlice(parent, "conditions")
		for _, rawCondition := range conditions {
			condition, ok := rawCondition.(map[string]any)
			if !ok || condition["status"] != "True" {
				continue
			}
			switch condition["type"] {
			case "Accepted":
				accepted = true
			case "ResolvedRefs":
				resolved = true
			}
		}
	}
	if accepted && resolved {
		return "Pass", "route is accepted and all references are resolved; " + routeBackendWeights(route)
	}
	return "Fail", fmt.Sprintf("route contracts are incomplete (accepted=%t resolvedRefs=%t); %s", accepted, resolved, routeBackendWeights(route))
}

func routeBackendWeights(route *unstructured.Unstructured) string {
	rules, _, _ := unstructured.NestedSlice(route.Object, "spec", "rules")
	backends := []string{}
	for _, rawRule := range rules {
		rule, ok := rawRule.(map[string]any)
		if !ok {
			continue
		}
		for _, rawBackend := range sliceValue(rule["backendRefs"]) {
			backend, ok := rawBackend.(map[string]any)
			if !ok {
				continue
			}
			name, _ := backend["name"].(string)
			weight := int64(1)
			switch value := backend["weight"].(type) {
			case int64:
				weight = value
			case float64:
				weight = int64(value)
			}
			if name != "" {
				backends = append(backends, fmt.Sprintf("%s=%d", name, weight))
			}
		}
	}
	if len(backends) == 0 {
		return "no backend weights reported"
	}
	return "backend weights " + strings.Join(backends, ",")
}

func sliceValue(value any) []any {
	items, _ := value.([]any)
	return items
}

func printBackendJSON(options *Options, client *ClusterClient, ctx context.Context, service string, port int, path string, query url.Values) error {
	raw, err := client.ServiceProxy(ctx, service, port, path, query)
	if err != nil {
		return err
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return exitError(ExitRemote, "decode backend response: %v", err)
	}
	if options.Format == "table" {
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(options.Stdout, string(data))
		return err
	}
	return options.printer().Print(value)
}

func objectMaps(items []unstructured.Unstructured) []map[string]any {
	values := make([]map[string]any, 0, len(items))
	for index := range items {
		values = append(values, items[index].Object)
	}
	return values
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}
