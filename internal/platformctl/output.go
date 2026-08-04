package platformctl

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"sigs.k8s.io/yaml"
)

type Printer struct {
	Format string
	Quiet  bool
	Writer io.Writer
}

func (p Printer) Print(value any) error {
	if p.Quiet {
		return nil
	}
	switch p.Format {
	case "json":
		encoder := json.NewEncoder(p.Writer)
		encoder.SetIndent("", "  ")
		return encoder.Encode(value)
	case "yaml":
		data, err := yaml.Marshal(value)
		if err != nil {
			return err
		}
		_, err = p.Writer.Write(data)
		return err
	default:
		return fmt.Errorf("table output requires a table-shaped command")
	}
}

func (p Printer) Table(headers []string, rows [][]string, machineValue any) error {
	if p.Quiet {
		return nil
	}
	if p.Format != "table" {
		return p.Print(machineValue)
	}
	writer := tabwriter.NewWriter(p.Writer, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, strings.Join(headers, "\t")); err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := fmt.Fprintln(writer, strings.Join(row, "\t")); err != nil {
			return err
		}
	}
	return writer.Flush()
}

// Redact removes values associated with credential-bearing keys from output.
func Redact(value string) string {
	lines := strings.Split(value, "\n")
	for index, line := range lines {
		lower := strings.ToLower(line)
		for _, marker := range []string{"token", "password", "private_key", "private-key", "sops_age_key", "database_url", "pgpassword"} {
			if strings.Contains(lower, marker) {
				separator := strings.IndexAny(line, ":=")
				if separator >= 0 {
					lines[index] = line[:separator+1] + " <redacted>"
				}
				break
			}
		}
	}
	return strings.Join(lines, "\n")
}
