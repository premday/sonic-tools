package analyzer

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

var (
	headerStyle = lipgloss.NewStyle().PaddingLeft(1).PaddingRight(1)
	cellStyle   = lipgloss.NewStyle().PaddingLeft(1).PaddingRight(1)
	borderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))

	sectionHeaderTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("255"))

	sectionHeaderRuleStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("214"))

	notFoundStyle = lipgloss.NewStyle().
			Faint(true).
			PaddingLeft(2).
			MarginTop(1)
)

func sectionHeader(title string) string {
	rule := sectionHeaderRuleStyle.Render("──")
	return fmt.Sprintf("%s %s %s\n", rule, sectionHeaderTitleStyle.Render(title), rule)
}

func sectionNotFound(msg string) string {
	return fmt.Sprintln(notFoundStyle.Render(msg))
}

type tableBuilder struct {
	headers []string
	rows    [][]string
}

func newTable(headers ...string) *tableBuilder {
	return &tableBuilder{headers: headers}
}

func (t *tableBuilder) addRow(values ...string) {
	row := make([]string, len(values))
	for i, v := range values {
		if v == "" {
			row[i] = "-"
		} else {
			row[i] = v
		}
	}
	t.rows = append(t.rows, row)
}

func (t *tableBuilder) String() string {
	if len(t.headers) == 0 {
		return ""
	}

	lt := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(borderStyle).
		Headers(t.headers...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			return cellStyle
		})

	for _, row := range t.rows {
		lt.Row(row...)
	}

	return lt.String() + "\n"
}
