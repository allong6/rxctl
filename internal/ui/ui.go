// Package ui 负责文本表格与 JSON 输出。
package ui

import (
	"encoding/json"
	"fmt"
	"io"
)

// Table 打印简单对齐表格。
func Table(w io.Writer, head []string, rows [][]string) {
	widths := make([]int, len(head))
	for i, h := range head {
		widths[i] = len(h)
	}
	for _, r := range rows {
		for i := range head {
			if i < len(r) && len(r[i]) > widths[i] {
				widths[i] = len(r[i])
			}
		}
	}
	line := func(cells []string) {
		for i := range head {
			c := ""
			if i < len(cells) {
				c = cells[i]
			}
			fmt.Fprintf(w, "%-*s  ", widths[i], c)
		}
		fmt.Fprintln(w)
	}
	line(head)
	for _, r := range rows {
		line(r)
	}
}

// JSON 输出缩进 JSON。
func JSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
