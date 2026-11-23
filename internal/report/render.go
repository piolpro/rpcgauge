package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/piolpro/rpcgauge/internal/probe"
)

// Document is the JSON output.
type Document struct {
	Tool        string    `json:"tool"`
	Version     string    `json:"version"`
	GeneratedAt time.Time `json:"generated_at"`
	Status      Status    `json:"status"`
	Endpoints   []*Entry  `json:"endpoints"`
}

// JSON writes the full machine-readable report.
func JSON(w io.Writer, version string, entries []*Entry) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(Document{
		Tool:        "rpcgauge",
		Version:     version,
		GeneratedAt: time.Now().UTC(),
		Status:      Worst(entries),
		Endpoints:   entries,
	})
}

var columns = []string{"#", "ENDPOINT", "CHAIN", "HEAD", "LAG", "P50", "P95", "ARCHIVE", "LOGS", "DEBUG", "TRACE", "BATCH", "BURST", "STATUS"}

func row(e *Entry) []string {
	if !e.Reachable {
		return []string{"-", e.Name, "-", "-", "-", "-", "-", "-", "-", "-", "-", "-", "-", string(e.Status)}
	}
	rank := fmt.Sprint(e.Rank)
	lag := "-"
	if e.Lag != nil {
		lag = fmt.Sprint(*e.Lag)
	}
	burst := "-"
	if e.Burst != nil {
		burst = fmt.Sprintf("%d/%d", e.Burst.OK, e.Burst.Requests)
	}
	return []string{
		rank, e.Name, ChainName(e.ChainID), fmt.Sprint(e.Head), lag,
		msStr(e.Latency.P50Ms), msStr(e.Latency.P95Ms),
		string(e.Archive), logsStr(e.LogsMaxRange, e.LogsRanges),
		string(e.Debug()), string(e.Trace()), string(e.Batch),
		burst, string(e.Status),
	}
}

func msStr(v float64) string {
	if v == 0 {
		return "-"
	}
	return fmt.Sprintf("%.0fms", v)
}

func logsStr(max uint64, rs []probe.RangeResult) string {
	if len(rs) == 0 {
		return "-"
	}
	if max == 0 {
		if rs[len(rs)-1].RateLimited {
			return "unknown"
		}
		return "no"
	}
	s := humanBlocks(max)
	for _, r := range rs {
		if !r.OK {
			return s
		}
	}
	return ">=" + s
}

func humanBlocks(n uint64) string {
	switch {
	case n >= 1_000_000 && n%1_000_000 == 0:
		return fmt.Sprintf("%dm", n/1_000_000)
	case n >= 1000 && n%1000 == 0:
		return fmt.Sprintf("%dk", n/1000)
	}
	return fmt.Sprint(n)
}

// Table writes an aligned text table followed by any problems and warnings.
func Table(w io.Writer, entries []*Entry) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(columns, "\t"))
	for _, e := range entries {
		fmt.Fprintln(tw, strings.Join(row(e), "\t"))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	notes(w, entries, "")
	return nil
}

// Markdown writes a GitHub-flavoured table, suitable for $GITHUB_STEP_SUMMARY.
func Markdown(w io.Writer, entries []*Entry) error {
	fmt.Fprintln(w, "| "+strings.Join(columns, " | ")+" |")
	fmt.Fprintln(w, "|"+strings.Repeat(" --- |", len(columns)))
	for _, e := range entries {
		cells := row(e)
		for i, c := range cells {
			cells[i] = strings.ReplaceAll(c, "|", `\|`)
		}
		fmt.Fprintln(w, "| "+strings.Join(cells, " | ")+" |")
	}
	notes(w, entries, "- ")
	return nil
}

func notes(w io.Writer, entries []*Entry, bullet string) {
	first := true
	for _, e := range entries {
		for _, p := range e.Problems {
			if first {
				fmt.Fprintln(w)
				first = false
			}
			fmt.Fprintf(w, "%s%s: FAIL %s\n", bullet, e.Name, p)
		}
		for _, p := range e.Warnings {
			if first {
				fmt.Fprintln(w)
				first = false
			}
			fmt.Fprintf(w, "%s%s: WARN %s\n", bullet, e.Name, p)
		}
	}
}
