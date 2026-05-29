package navigation

import "github.com/edwmurph/codux/internal/state"

func SelectRelative(tabs []state.Tab, activeID string, delta int) string {
	if len(tabs) == 0 {
		return ""
	}
	activeIndex := 0
	for index, tab := range tabs {
		if tab.ID == activeID {
			activeIndex = index
			break
		}
	}
	next := (activeIndex + delta) % len(tabs)
	if next < 0 {
		next += len(tabs)
	}
	return tabs[next].ID
}

func SelectGridTab(tabs []state.Tab, activeID string, columns []string, deltaColumn int, deltaRow int) string {
	if len(tabs) == 0 {
		return ""
	}
	byColumn := map[string][]state.Tab{}
	for _, tab := range tabs {
		byColumn[tab.Column] = append(byColumn[tab.Column], tab)
	}
	active := tabs[0]
	for _, tab := range tabs {
		if tab.ID == activeID {
			active = tab
			break
		}
	}
	columnIndex := 0
	for index, column := range columns {
		if column == active.Column {
			columnIndex = index
			break
		}
	}
	rowIndex := 0
	for index, tab := range byColumn[active.Column] {
		if tab.ID == active.ID {
			rowIndex = index
			break
		}
	}
	if deltaRow != 0 {
		columnTabs := byColumn[active.Column]
		if len(columnTabs) == 0 {
			return active.ID
		}
		nextRow := rowIndex + deltaRow
		if nextRow < 0 {
			nextRow = 0
		}
		if nextRow >= len(columnTabs) {
			nextRow = len(columnTabs) - 1
		}
		return columnTabs[nextRow].ID
	}
	nextColumn := columnIndex + deltaColumn
	if nextColumn < 0 {
		nextColumn = 0
	}
	if nextColumn >= len(columns) {
		nextColumn = len(columns) - 1
	}
	targetColumn := columns[nextColumn]
	columnTabs := byColumn[targetColumn]
	if len(columnTabs) == 0 {
		return active.ID
	}
	if rowIndex >= len(columnTabs) {
		rowIndex = len(columnTabs) - 1
	}
	return columnTabs[rowIndex].ID
}
