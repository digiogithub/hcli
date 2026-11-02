// Package output renders command results as a table, JSON or YAML.
//
// Commands build a Table for the human view and pass the raw API objects for
// the machine formats, so `-o json` always returns the full API payload.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"gopkg.in/yaml.v3"
)

// Format is the selected output format.
type Format string

const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
	FormatYAML  Format = "yaml"
)

// Formats lists the accepted values (used for flag completion).
var Formats = []string{string(FormatTable), string(FormatJSON), string(FormatYAML)}

// Parse validates a --output value.
func Parse(s string) (Format, error) {
	switch Format(strings.ToLower(s)) {
	case FormatTable, "":
		return FormatTable, nil
	case FormatJSON:
		return FormatJSON, nil
	case FormatYAML, "yml":
		return FormatYAML, nil
	}
	return "", fmt.Errorf("unknown output format %q (use %s)", s, strings.Join(Formats, ", "))
}

// Printer writes results in the selected format.
type Printer struct {
	Out      io.Writer
	Format   Format
	NoHeader bool
}

// Table is a simple column/row view.
type Table struct {
	Headers []string
	Rows    [][]string
}

// AddRow appends a row, formatting every value with fmt.Sprint ("-" for empty).
func (t *Table) AddRow(values ...any) {
	row := make([]string, len(values))
	for i, v := range values {
		s := fmt.Sprint(v)
		if v == nil || s == "" || s == "<nil>" {
			s = "-"
		}
		row[i] = s
	}
	t.Rows = append(t.Rows, row)
}

// Print renders raw as JSON/YAML, or table as a table.
func (p *Printer) Print(raw any, table *Table) error {
	switch p.Format {
	case FormatJSON:
		return p.JSON(raw)
	case FormatYAML:
		return p.YAML(raw)
	default:
		if table == nil {
			return p.JSON(raw)
		}
		return p.Table(table)
	}
}

// JSON writes v as indented JSON.
func (p *Printer) JSON(v any) error {
	enc := json.NewEncoder(p.Out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// YAML writes v as YAML (via JSON, so struct json tags are honoured).
func (p *Printer) YAML(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var generic any
	if err := json.Unmarshal(data, &generic); err != nil {
		return err
	}
	enc := yaml.NewEncoder(p.Out)
	enc.SetIndent(2)
	defer enc.Close()
	return enc.Encode(generic)
}

// Table writes a tab-aligned table.
func (p *Printer) Table(t *Table) error {
	w := tabwriter.NewWriter(p.Out, 0, 0, 3, ' ', 0)
	if !p.NoHeader && len(t.Headers) > 0 {
		fmt.Fprintln(w, strings.ToUpper(strings.Join(t.Headers, "\t")))
	}
	for _, row := range t.Rows {
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}
	return w.Flush()
}

// KeyValues prints a describe-style "Key: value" block for table output.
func (p *Printer) KeyValues(raw any, pairs [][2]string) error {
	if p.Format != FormatTable {
		return p.Print(raw, nil)
	}
	w := tabwriter.NewWriter(p.Out, 0, 0, 2, ' ', 0)
	for _, kv := range pairs {
		v := kv[1]
		if v == "" {
			v = "-"
		}
		fmt.Fprintf(w, "%s:\t%s\n", kv[0], v)
	}
	return w.Flush()
}
