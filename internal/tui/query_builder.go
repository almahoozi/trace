package tui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/almahoozi/trace/internal/traceql"
)

type queryBuilderResult struct {
	Query       string
	Environment string
	Limit       int
	SPSS        int
	Since       time.Duration
	StartAt     time.Time
	EndAt       time.Time
	HasStartAt  bool
	HasEndAt    bool
	Apply       bool
	Cancel      bool
	Quit        bool
	Cmd         tea.Cmd
}

type queryBuilder struct {
	rows          []queryRow
	row           int
	tableRow      int
	queryText     string
	queryCursor   int
	queryEditing  bool
	tableDisabled bool
	tableReason   string
	width         int
	height        int
	fields        []string
	operators     []string
	environments  []string
	environment   string
	timeframe     []timeframeOption
	timeIdx       int
	limit         int
	spss          int
	startRaw      string
	endRaw        string
	timeMode      string

	mode string

	rowEditIdx       int
	rowForm          *huh.Form
	rowField         string
	rowOp            string
	rowValue         string
	rowCustom        string
	rowFiltering     bool
	rowFormHasCustom bool

	globalForm      *huh.Form
	globalEnv       string
	globalTimeValue string
	globalLimitRaw  string
	globalSPSSRaw   string
	globalStartRaw   string
	globalEndRaw     string
	globalFiltering bool

	rangeFrom  dateTimeSegments
	rangeTo    dateTimeSegments
	rangeTZ    timezoneSegments
	rangeDurationRaw string
	rangeDurationTouched bool
	rangeFocus int
	rangeError string
}

const customFieldOptionValue = "__custom_field__"

const (
	timeModeSince = "since"
	timeModeRange = "time range"
)

type dateTimeSegments struct {
	Day      string
	Month    string
	Year     string
	Hour     string
	Minute   string
	Second   string
}

type timezoneSegments struct {
	Kind   string
	Hour   string
	Minute string
}

func newQueryBuilder(query string, fields, environments []string, activeEnv string, activeLimit, activeSPSS int, activeSince time.Duration, startAt, endAt time.Time, hasStartAt, hasEndAt bool, knownQueryError string) *queryBuilder {
	clauses := traceql.SplitClauses(query)
	rows := make([]queryRow, 0, len(clauses))
	for _, clause := range clauses {
		field, op, value, ok := parseClause(clause)
		if !ok {
			rows = append(rows, queryRow{Field: "name", Operator: "=", Value: strings.TrimSpace(clause)})
			continue
		}
		rows = append(rows, queryRow{Field: field, Operator: op, Value: strings.TrimSpace(value)})
	}

	availableFields := append([]string{}, defaultQueryFields...)
	seen := map[string]bool{}
	for _, f := range availableFields {
		seen[strings.ToLower(strings.TrimSpace(f))] = true
	}
	for _, f := range fields {
		trimmed := strings.TrimSpace(f)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if seen[key] {
			continue
		}
		availableFields = append(availableFields, trimmed)
		seen[key] = true
	}

	timeframes := []timeframeOption{
		{Label: "15m", Since: 15 * time.Minute, Description: "last 15 minutes"},
		{Label: "1h", Since: time.Hour, Description: "last hour"},
		{Label: "6h", Since: 6 * time.Hour, Description: "last 6 hours"},
		{Label: "24h", Since: 24 * time.Hour, Description: "last day"},
		{Label: "7d", Since: 7 * 24 * time.Hour, Description: "last week"},
		{Label: "30d", Since: 30 * 24 * time.Hour, Description: "last month"},
		{Label: "all", Since: 0, Description: "no time filtering"},
	}
	timeIdx := 1
	for i, option := range timeframes {
		if option.Since == activeSince {
			timeIdx = i
			break
		}
	}

	envs := make([]string, 0, len(environments))
	for _, env := range environments {
		trimmed := strings.TrimSpace(env)
		if trimmed != "" {
			envs = append(envs, trimmed)
		}
	}
	if len(envs) == 0 {
		envs = []string{strings.TrimSpace(activeEnv)}
	}
	active := strings.TrimSpace(activeEnv)
	if active == "" {
		active = envs[0]
	}

	startRaw := ""
	if hasStartAt {
		startRaw = startAt.Format(time.RFC3339)
	}
	endRaw := ""
	if hasEndAt {
		endRaw = endAt.Format(time.RFC3339)
	}

	b := &queryBuilder{
		rows:         rows,
		tableRow:     0,
		fields:       availableFields,
		operators:    []string{"=", "!=", "=~", "!~", ">", ">=", "<", "<="},
		environments: envs,
		environment:  active,
		timeframe:    timeframes,
		timeIdx:      timeIdx,
		limit:        max(1, activeLimit),
		spss:         max(1, activeSPSS),
		startRaw:     startRaw,
		endRaw:       endRaw,
		timeMode:     resolveTimeMode(hasStartAt, hasEndAt),
		queryText:    strings.TrimSpace(query),
		mode:         "table",
	}
	b.refreshTableSupport(strings.TrimSpace(knownQueryError))
	return b
}

var defaultQueryFields = []string{
	"name", "trace:id", "trace:duration", "status", "service.name", "resource.service.name", "resource.deployment.environment",
	"resource.k8s.namespace.name", "resource.k8s.pod.name", "resource.k8s.container.name", "status.code", "status.message",
	"duration", "kind", "span.http.method", "span.http.route", "span.http.target", "span.http.status_code", "span.rpc.system",
	"span.rpc.service", "span.rpc.method", "span.db.system", "span.db.statement", "span.messaging.system", "span.messaging.operation",
	"span.net.peer.name", "span.net.peer.ip", "span.net.host.name", "span.error",
}

func (b *queryBuilder) SetSize(width, height int) {
	b.width = width
	b.height = height
	if b.rowForm != nil {
		b.rowForm.WithWidth(max(40, width-4)).WithHeight(max(10, height-6))
	}
	if b.globalForm != nil {
		b.globalForm.WithWidth(max(40, width-4)).WithHeight(max(10, height-6))
	}
}

func (b *queryBuilder) Update(msg tea.Msg) queryBuilderResult {
	if b == nil {
		return queryBuilderResult{}
	}
	if sizeMsg, ok := msg.(tea.WindowSizeMsg); ok {
		b.SetSize(sizeMsg.Width, sizeMsg.Height)
		return queryBuilderResult{}
	}

	switch b.mode {
	case "row_form":
		return b.updateRowForm(msg)
	case "global_form":
		return b.updateGlobalForm(msg)
	case "global_range_form":
		return b.updateGlobalRangeForm(msg)
	default:
		return b.updateTable(msg)
	}
}

func (b *queryBuilder) updateTable(msg tea.Msg) queryBuilderResult {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return queryBuilderResult{}
	}
	if strings.TrimSpace(b.queryText) == "" {
		b.syncQueryTextFromRows()
	}
	if b.queryCursor < 0 {
		b.queryCursor = 0
	}
	if b.queryCursor > len([]rune(b.queryText)) {
		b.queryCursor = len([]rune(b.queryText))
	}

	maxRow := len(b.rows) + 2
	if b.queryEditing {
		switch keyMsg.String() {
		case "esc":
			b.queryEditing = false
			return queryBuilderResult{}
		case "enter":
			b.queryEditing = false
			b.syncRowsFromQueryText()
			b.refreshTableSupport("")
			return queryBuilderResult{}
		case "left":
			if b.queryCursor > 0 {
				b.queryCursor--
			}
			return queryBuilderResult{}
		case "right":
			if b.queryCursor < len([]rune(b.queryText)) {
				b.queryCursor++
			}
			return queryBuilderResult{}
		case "home":
			b.queryCursor = 0
			return queryBuilderResult{}
		case "end":
			b.queryCursor = len([]rune(b.queryText))
			return queryBuilderResult{}
		case "backspace":
			r := []rune(b.queryText)
			if b.queryCursor > 0 {
				r = append(r[:b.queryCursor-1], r[b.queryCursor:]...)
				b.queryCursor--
				b.queryText = string(r)
				b.syncRowsFromQueryText()
				b.refreshTableSupport("")
			}
			return queryBuilderResult{}
		case "delete":
			r := []rune(b.queryText)
			if b.queryCursor < len(r) {
				r = append(r[:b.queryCursor], r[b.queryCursor+1:]...)
				b.queryText = string(r)
				b.syncRowsFromQueryText()
				b.refreshTableSupport("")
			}
			return queryBuilderResult{}
		case "ctrl+r", "ctrl+enter", "ctrl+j":
			res := b.snapshotResult()
			res.Apply = true
			return res
		}
		if keyMsg.Type == tea.KeyRunes {
			r := []rune(b.queryText)
			insert := keyMsg.Runes
			r = append(r[:b.queryCursor], append(insert, r[b.queryCursor:]...)...)
			b.queryCursor += len(insert)
			b.queryText = string(r)
			b.syncRowsFromQueryText()
			b.refreshTableSupport("")
		} else if keyMsg.Type == tea.KeySpace {
			r := []rune(b.queryText)
			r = append(r[:b.queryCursor], append([]rune{' '}, r[b.queryCursor:]...)...)
			b.queryCursor++
			b.queryText = string(r)
			b.syncRowsFromQueryText()
			b.refreshTableSupport("")
		}
		return queryBuilderResult{}
	}
	switch keyMsg.String() {
	case "q":
		return queryBuilderResult{Quit: true}
	case "esc":
		return queryBuilderResult{Cancel: true}
	case "tab":
		b.jumpComponent(1)
		return queryBuilderResult{}
	case "shift+tab":
		b.jumpComponent(-1)
		return queryBuilderResult{}
	case "up", "k":
		b.moveRow(-1)
	case "down", "j":
		b.moveRow(1)
	case "e", "enter":
		if b.tableDisabled && b.row >= 0 && b.row <= len(b.rows) {
			return queryBuilderResult{}
		}
		if b.row == -1 {
			b.startGlobalForm()
			return queryBuilderResult{Cmd: b.globalForm.Init()}
		}
		if b.row == maxRow {
			b.queryEditing = true
			b.queryCursor = len([]rune(b.queryText))
			return queryBuilderResult{}
		}
		if b.row == maxRow-1 {
			res := b.snapshotResult()
			res.Apply = true
			return res
		}
		b.startRowForm()
		return queryBuilderResult{Cmd: b.rowForm.Init()}
	case "d":
		if b.tableDisabled {
			return queryBuilderResult{}
		}
		if b.row >= 0 && b.row < len(b.rows) {
			b.rows = append(b.rows[:b.row], b.rows[b.row+1:]...)
			b.syncQueryTextFromRows()
			if b.row > len(b.rows) {
				b.row = len(b.rows)
			}
			b.tableRow = min(b.tableRow, len(b.rows))
		}
	case "g":
		b.startGlobalForm()
		return queryBuilderResult{Cmd: b.globalForm.Init()}
	case "o":
		if b.tableDisabled {
			return queryBuilderResult{}
		}
		b.row = len(b.rows)
		b.startRowForm()
		return queryBuilderResult{Cmd: b.rowForm.Init()}
	case "ctrl+r", "ctrl+enter", "ctrl+j":
		res := b.snapshotResult()
		res.Apply = true
		return res
	}
	return queryBuilderResult{}
}

func (b *queryBuilder) updateRowForm(msg tea.Msg) queryBuilderResult {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.String() == "/" && isFocusedFilterableSelect(b.rowForm) {
			b.rowFiltering = true
		}
		if keyMsg.String() == "esc" {
			if !b.rowFiltering {
				b.mode = "table"
				b.rowForm = nil
				return queryBuilderResult{}
			}
			b.rowFiltering = false
		}
		if keyMsg.String() == "ctrl+enter" || keyMsg.String() == "ctrl+j" {
			if b.validateRowFormValues() {
				b.applyRowFormValues()
				b.mode = "table"
				b.rowForm = nil
				b.rowFiltering = false
			}
			return queryBuilderResult{}
		}
	}
	updated, cmd := b.rowForm.Update(msg)
	if next, ok := updated.(*huh.Form); ok {
		b.rowForm = next
	}
	result := queryBuilderResult{Cmd: cmd}
	if wantCustom := b.rowField == customFieldOptionValue; wantCustom != b.rowFormHasCustom {
		b.rebuildRowForm()
		if wantCustom {
			return queryBuilderResult{Cmd: tea.Batch(b.rowForm.Init(), focusNextFieldCmd())}
		}
		return queryBuilderResult{Cmd: b.rowForm.Init()}
	}
	if b.rowForm.State == huh.StateAborted {
		b.mode = "table"
		b.rowForm = nil
		b.rowFiltering = false
		return result
	}
	if b.rowForm.State != huh.StateCompleted {
		return result
	}
	b.applyRowFormValues()
	b.mode = "table"
	b.rowForm = nil
	b.rowFiltering = false
	return result
}

func (b *queryBuilder) updateGlobalForm(msg tea.Msg) queryBuilderResult {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.String() == "/" && isFocusedFilterableSelect(b.globalForm) {
			b.globalFiltering = true
		}
		if keyMsg.String() == "esc" {
			if !b.globalFiltering {
				b.mode = "table"
				b.globalForm = nil
				return queryBuilderResult{}
			}
			b.globalFiltering = false
		}
		if keyMsg.String() == "enter" && strings.EqualFold(strings.TrimSpace(b.globalTimeValue), "edit time range") {
			title := focusedFieldTitle(b.globalForm)
			if title == "Time value" {
				b.applyGlobalBasics()
				b.startGlobalRangeForm()
				return queryBuilderResult{}
			}
		}
		if keyMsg.String() == "ctrl+enter" || keyMsg.String() == "ctrl+j" {
			b.applyGlobalBasics()
			if b.timeMode == timeModeRange {
				b.startGlobalRangeForm()
				return queryBuilderResult{}
			}
			b.startRaw = ""
			b.endRaw = ""
			b.mode = "table"
			b.globalForm = nil
			b.globalFiltering = false
			return queryBuilderResult{}
		}
	}
	updated, cmd := b.globalForm.Update(msg)
	if next, ok := updated.(*huh.Form); ok {
		b.globalForm = next
	}
	result := queryBuilderResult{Cmd: cmd}
	if b.globalForm.State == huh.StateAborted {
		b.mode = "table"
		b.globalForm = nil
		b.globalFiltering = false
		return result
	}
	if b.globalForm.State != huh.StateCompleted {
		return result
	}
	b.applyGlobalBasics()
	if b.timeMode == timeModeRange {
		b.startGlobalRangeForm()
		return result
	}
	b.startRaw = ""
	b.endRaw = ""
	b.mode = "table"
	b.globalForm = nil
	b.globalFiltering = false
	return result
}

func (b *queryBuilder) updateGlobalRangeForm(msg tea.Msg) queryBuilderResult {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return queryBuilderResult{}
	}
	b.rangeError = ""
	switch keyMsg.String() {
	case "esc":
		b.mode = "global_form"
		b.globalForm = b.buildGlobalForm()
		return queryBuilderResult{Cmd: b.globalForm.Init()}
	case "left", "shift+tab":
		b.rangeFocus = (b.rangeFocus - 1 + rangeEditorFieldCount) % rangeEditorFieldCount
		return queryBuilderResult{}
	case "right", "tab":
		b.rangeFocus = (b.rangeFocus + 1) % rangeEditorFieldCount
		return queryBuilderResult{}
	case "up":
		switch {
		case b.rangeFocus >= 0 && b.rangeFocus <= 5:
			b.rangeFocus = rangeFocusDuration
		case b.rangeFocus >= 6 && b.rangeFocus <= 11:
			b.rangeFocus -= 6
		case b.rangeFocus >= rangeFocusTZKind && b.rangeFocus <= rangeFocusTZMinute:
			b.rangeFocus = 6
		case b.rangeFocus == rangeFocusDuration:
			b.rangeFocus = rangeFocusTZKind
		}
		return queryBuilderResult{}
	case "down":
		switch {
		case b.rangeFocus >= 0 && b.rangeFocus <= 5:
			b.rangeFocus += 6
		case b.rangeFocus >= 6 && b.rangeFocus <= 11:
			b.rangeFocus = rangeFocusTZKind
		case b.rangeFocus >= rangeFocusTZKind && b.rangeFocus <= rangeFocusTZMinute:
			b.rangeFocus = rangeFocusDuration
		case b.rangeFocus == rangeFocusDuration:
			b.rangeFocus = 0
		}
		return queryBuilderResult{}
	case "backspace":
		if b.rangeFocus == rangeFocusDuration {
			if len(b.rangeDurationRaw) > 0 {
				b.rangeDurationRaw = b.rangeDurationRaw[:len(b.rangeDurationRaw)-1]
				b.rangeDurationTouched = true
				b.refreshRangeToFromDuration()
			}
			return queryBuilderResult{}
		}
		if !b.popRangeSegmentValue(b.rangeFocus) {
			prev := (b.rangeFocus - 1 + rangeEditorFieldCount) % rangeEditorFieldCount
			if b.popRangeSegmentValue(prev) {
				b.rangeFocus = prev
			}
		}
		b.refreshRangeDerivedAfterEdit(b.rangeFocus)
		return queryBuilderResult{}
	case "delete":
		if b.rangeFocus == rangeFocusDuration {
			b.rangeDurationRaw = ""
			b.rangeDurationTouched = true
			b.refreshRangeToFromDuration()
			return queryBuilderResult{}
		}
		if b.popRangeSegmentValue(b.rangeFocus) {
			b.refreshRangeDerivedAfterEdit(b.rangeFocus)
		}
		return queryBuilderResult{}
	case "+", "-", "z", "Z":
		if b.rangeFocus == rangeFocusTZKind {
			b.setRangeSegmentValue(b.rangeFocus, keyMsg.String())
			b.refreshRangeDerivedAfterEdit(b.rangeFocus)
			b.rangeFocus = (b.rangeFocus + 1) % rangeEditorFieldCount
		}
		return queryBuilderResult{}
	case "enter", "ctrl+enter", "ctrl+j":
		startInput, endInput, err := b.rangeInputsFromSegments()
		if err != nil {
			b.rangeError = err.Error()
			return queryBuilderResult{}
		}
		b.globalStartRaw = startInput
		b.globalEndRaw = endInput
		b.applyGlobalFormValues()
		b.mode = "table"
		b.globalForm = nil
		b.globalFiltering = false
		return queryBuilderResult{}
	}

	if keyMsg.Type == tea.KeyRunes && len(keyMsg.Runes) == 1 {
		r := keyMsg.Runes[0]
		if b.rangeFocus == rangeFocusDuration {
			if isDurationRune(r) {
				if !b.rangeDurationTouched {
					b.rangeDurationRaw = ""
					b.rangeDurationTouched = true
				}
				b.rangeDurationRaw += string(r)
				b.refreshRangeToFromDuration()
			}
			return queryBuilderResult{}
		}
		if r >= '0' && r <= '9' {
			editedFocus := b.rangeFocus
			if b.appendRangeSegmentValue(editedFocus, string(r)) {
				if b.segmentLenByFocus(editedFocus) > 0 && len(b.getRangeSegmentValue(editedFocus)) >= b.segmentLenByFocus(editedFocus) {
					b.rangeFocus = (b.rangeFocus + 1) % rangeEditorFieldCount
				}
			}
			b.refreshRangeDerivedAfterEdit(editedFocus)
		}
	}

	return queryBuilderResult{}
}

func (b *queryBuilder) View(width int) string {
	if b == nil {
		return ""
	}
	if b.mode == "row_form" && b.rowForm != nil {
		title := "add query row"
		if b.rowEditIdx >= 0 && b.rowEditIdx < len(b.rows) {
			title = fmt.Sprintf("edit query row %d", b.rowEditIdx+1)
		}
		return title + "\n\n" + b.rowForm.View()
	}
	if b.mode == "global_form" && b.globalForm != nil {
		return "query settings\n\n" + b.globalForm.View()
	}
	if b.mode == "global_range_form" {
		return b.viewGlobalRangeForm()
	}

	var lines []string
	lines = append(lines, mutedStyle.Render("up/down move | enter edit/open | d delete row | g settings | ctrl+enter or ctrl+r run | esc cancel"))
	lines = append(lines, "")
	globalPrefix := " "
	if b.row == -1 {
		globalPrefix = ">"
	}
	lines = append(lines, fmt.Sprintf(
		"%s [global settings]: %s=%s %s=%s %s=%s %s=%s %s=%s %s=%s",
		globalPrefix,
		mutedStyle.Render("env"), titleStyle.Render(b.environment),
		mutedStyle.Render("time"), titleStyle.Render(b.activeTimeSummary()),
		mutedStyle.Render("limit"), titleStyle.Render(strconv.Itoa(b.limit)),
		mutedStyle.Render("spss"), titleStyle.Render(strconv.Itoa(b.spss)),
		mutedStyle.Render("start"), titleStyle.Render(defaultText(b.startRaw, "-")),
		mutedStyle.Render("end"), titleStyle.Render(defaultText(b.endRaw, "-")),
	))
	lines = append(lines, "")
	lines = append(lines, "idx | field                  | op  | value")
	lines = append(lines, "----+------------------------+-----+-------------------------")
	for i, row := range b.rows {
		prefix := " "
		if !b.tableDisabled && b.row == i {
			prefix = ">"
		}
		line := fmt.Sprintf("%s%3d | %-22s | %-3s | %s", prefix, i+1, truncate(defaultText(strings.TrimSpace(row.Field), "name"), 22), truncate(defaultText(strings.TrimSpace(row.Operator), "="), 3), truncate(row.Value, max(20, width-40)))
		if b.tableDisabled {
			line = mutedStyle.Render(line)
		}
		lines = append(lines, line)
	}
	newPrefix := " "
	if !b.tableDisabled && b.row == len(b.rows) {
		newPrefix = ">"
	}
	newRowLine := fmt.Sprintf("%s -- | %-22s | %-3s | %s", newPrefix, "(new row)", "=", "press e or enter")
	if b.tableDisabled {
		newRowLine = mutedStyle.Render(newRowLine)
	}
	lines = append(lines, newRowLine)
	if b.tableDisabled {
		lines = append(lines, "")
		lines = append(lines, summaryWarnStyle.Render("table mode disabled: "+b.tableReason))
		lines = append(lines, summaryWarnStyle.Render("edit query text directly, then press enter"))
	}
	runPrefix := " "
	if b.row == len(b.rows)+1 {
		runPrefix = ">"
	}
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("%s [run query]", runPrefix))
	queryPrefix := " "
	if b.row == len(b.rows)+2 {
		queryPrefix = ">"
	}
	queryLine := b.queryText
	if strings.TrimSpace(queryLine) == "" {
		queryLine = ""
	}
	if b.queryEditing {
		r := []rune(queryLine)
		if b.queryCursor < 0 {
			b.queryCursor = 0
		}
		if b.queryCursor > len(r) {
			b.queryCursor = len(r)
		}
		if b.queryCursor == len(r) {
			queryLine = string(r) + "|"
		} else {
			queryLine = string(r[:b.queryCursor]) + "|" + string(r[b.queryCursor:])
		}
	}
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("%s query: %s", queryPrefix, queryLine))
	return strings.Join(lines, "\n")
}

func (b *queryBuilder) startRowForm() {
	b.mode = "row_form"
	b.rowEditIdx = b.row

	current := queryRow{Field: "name", Operator: "=", Value: ""}
	if b.rowEditIdx >= 0 && b.rowEditIdx < len(b.rows) {
		current = b.rows[b.rowEditIdx]
	}
	b.rowField = defaultText(strings.TrimSpace(current.Field), "name")
	b.rowOp = defaultText(strings.TrimSpace(current.Operator), "=")
	b.rowValue = strings.TrimSpace(current.Value)
	b.rowCustom = ""

	fieldOptions := make([]huh.Option[string], 0, len(b.fields)+1)
	seenField := map[string]bool{}
	for _, field := range b.fields {
		trimmed := strings.TrimSpace(field)
		if trimmed == "" {
			continue
		}
		k := strings.ToLower(trimmed)
		if seenField[k] {
			continue
		}
		seenField[k] = true
		fieldOptions = append(fieldOptions, huh.NewOption(trimmed, trimmed))
	}
	if !seenField[strings.ToLower(b.rowField)] {
		b.rowCustom = b.rowField
		b.rowField = customFieldOptionValue
	}
	fieldOptions = append(fieldOptions, huh.NewOption("Other (custom field)", customFieldOptionValue))

	opOptions := make([]huh.Option[string], 0, len(b.operators))
	for _, op := range b.operators {
		label := op
		if hint := operatorHint(op); hint != "" {
			label = fmt.Sprintf("%s - %s", op, hint)
		}
		opOptions = append(opOptions, huh.NewOption(label, op))
	}

	b.rowForm = b.buildRowForm(fieldOptions, opOptions)
	b.rowFiltering = false
	b.rowFormHasCustom = b.rowField == customFieldOptionValue
}

func (b *queryBuilder) rebuildRowForm() {
	fieldOptions := make([]huh.Option[string], 0, len(b.fields)+1)
	seenField := map[string]bool{}
	for _, field := range b.fields {
		trimmed := strings.TrimSpace(field)
		if trimmed == "" {
			continue
		}
		k := strings.ToLower(trimmed)
		if seenField[k] {
			continue
		}
		seenField[k] = true
		fieldOptions = append(fieldOptions, huh.NewOption(trimmed, trimmed))
	}
	if !seenField[strings.ToLower(b.rowField)] && b.rowField != customFieldOptionValue {
		fieldOptions = append(fieldOptions, huh.NewOption(b.rowField, b.rowField))
	}
	fieldOptions = append(fieldOptions, huh.NewOption("Other (custom field)", customFieldOptionValue))

	opOptions := make([]huh.Option[string], 0, len(b.operators))
	for _, op := range b.operators {
		label := op
		if hint := operatorHint(op); hint != "" {
			label = fmt.Sprintf("%s - %s", op, hint)
		}
		opOptions = append(opOptions, huh.NewOption(label, op))
	}

	b.rowForm = b.buildRowForm(fieldOptions, opOptions)
	b.rowFormHasCustom = b.rowField == customFieldOptionValue
}

func (b *queryBuilder) buildRowForm(fieldOptions []huh.Option[string], opOptions []huh.Option[string]) *huh.Form {
	fields := []huh.Field{
		huh.NewSelect[string]().Title("Field (select)").Description("press enter to open options; type to filter").Filtering(true).Options(fieldOptions...).Value(&b.rowField).Validate(func(v string) error {
			if strings.TrimSpace(v) == "" {
				return fmt.Errorf("field is required")
			}
			return nil
		}),
	}
	if b.rowField == customFieldOptionValue {
		fields = append(fields,
			huh.NewInput().Title("Custom field").Placeholder("resource.my.custom.label").Value(&b.rowCustom).Validate(func(v string) error {
				if strings.TrimSpace(v) == "" {
					return fmt.Errorf("custom field is required")
				}
				return nil
			}),
		)
	}
	fields = append(fields,
		huh.NewSelect[string]().Title("Operator").Description("press enter to open options").Filtering(true).Options(opOptions...).Value(&b.rowOp).Validate(func(v string) error {
			raw := strings.TrimSpace(v)
			for _, allowed := range b.operators {
				if raw == allowed {
					return nil
				}
			}
			return fmt.Errorf("unsupported operator")
		}),
		huh.NewInput().Title("Value").Value(&b.rowValue),
	)

	return huh.NewForm(
		huh.NewGroup(fields...),
	).WithShowHelp(false).WithWidth(max(40, b.width-4)).WithHeight(max(10, b.height-6))
}

func (b *queryBuilder) startGlobalForm() {
	b.mode = "global_form"
	b.globalEnv = b.environment
	b.globalTimeValue = b.selectedTimeframeLabel()
	if b.timeMode == timeModeRange {
		b.globalTimeValue = "edit time range"
	}
	b.globalLimitRaw = strconv.Itoa(max(1, b.limit))
	b.globalSPSSRaw = strconv.Itoa(max(1, b.spss))
	b.globalStartRaw = formatDateTimeInput(b.startRaw)
	b.globalEndRaw = formatDateTimeInput(b.endRaw)
	b.globalForm = b.buildGlobalForm()
	b.globalFiltering = false
}

func (b *queryBuilder) buildGlobalForm() *huh.Form {
	envOptions := make([]huh.Option[string], 0, len(b.environments))
	for _, env := range b.environments {
		envOptions = append(envOptions, huh.NewOption(env, env))
	}
	timeOptions := make([]huh.Option[string], 0, len(b.timeframe))
	for _, tf := range b.timeframe {
		label := tf.Label
		if tf.Description != "" {
			label = fmt.Sprintf("%s - %s", tf.Label, tf.Description)
		}
		timeOptions = append(timeOptions, huh.NewOption(label, tf.Label))
	}
	timeValueOptions := make([]huh.Option[string], 0, len(timeOptions)+1)
	timeValueOptions = append(timeValueOptions, timeOptions...)
	timeValueOptions = append(timeValueOptions, huh.NewOption("edit time range", "edit time range"))

	fields := []huh.Field{
		huh.NewSelect[string]().Title("Environment").Description("press enter to choose environment").Filtering(true).Options(envOptions...).Value(&b.globalEnv),
		huh.NewSelect[string]().Title("Time value").Description(fmt.Sprintf("pick since preset or edit explicit range (%s -> %s)", defaultText(b.globalStartRaw, "not set"), defaultText(b.globalEndRaw, "not set"))).Filtering(false).Options(timeValueOptions...).Value(&b.globalTimeValue),
	}

	fields = append(fields,
		huh.NewInput().Title("Limit").Description("max traces to request").Placeholder("50").Value(&b.globalLimitRaw).Validate(func(v string) error {
			if _, err := parsePositiveInt(v); err != nil {
				return err
			}
			return nil
		}),
		huh.NewInput().Title("SPSS").Description("spans per span set").Placeholder("3").Value(&b.globalSPSSRaw).Validate(func(v string) error {
			if _, err := parsePositiveInt(v); err != nil {
				return err
			}
			return nil
		}),
	)

	return huh.NewForm(
		huh.NewGroup(fields...),
	).WithShowHelp(false).WithWidth(max(40, b.width-4)).WithHeight(max(10, b.height-6))
}

func isFocusedFilterableSelect(form *huh.Form) bool {
	if form == nil {
		return false
	}
	switch focused := form.GetFocusedField().(type) {
	case *huh.Select[string]:
		return focused.GetFiltering()
	default:
		return false
	}
}

func focusedFieldTitle(form *huh.Form) string {
	if form == nil {
		return ""
	}
	focused := form.GetFocusedField()
	withTitle, ok := focused.(interface{ GetTitle() string })
	if !ok {
		return ""
	}
	return strings.TrimSpace(withTitle.GetTitle())
}

func (b *queryBuilder) validateRowFormValues() bool {
	resolvedField := strings.TrimSpace(b.rowField)
	if resolvedField == customFieldOptionValue {
		resolvedField = strings.TrimSpace(b.rowCustom)
	}
	if resolvedField == "" {
		return false
	}
	raw := strings.TrimSpace(b.rowOp)
	for _, allowed := range b.operators {
		if raw == allowed {
			return true
		}
	}
	return false
}

func (b *queryBuilder) applyRowFormValues() {
	resolvedField := strings.TrimSpace(b.rowField)
	if resolvedField == customFieldOptionValue {
		resolvedField = strings.TrimSpace(b.rowCustom)
	}
	row := queryRow{Field: resolvedField, Operator: strings.TrimSpace(b.rowOp), Value: strings.TrimSpace(b.rowValue)}
	if row.Field == "" {
		row.Field = "name"
	}
	if row.Operator == "" {
		row.Operator = "="
	}
	if b.rowEditIdx >= 0 && b.rowEditIdx < len(b.rows) {
		b.rows[b.rowEditIdx] = row
		b.row = b.rowEditIdx
	} else {
		b.rows = append(b.rows, row)
		b.row = len(b.rows) - 1
	}
	b.syncQueryTextFromRows()
	b.refreshTableSupport("")
}

func (b *queryBuilder) applyGlobalFormValues() {
	b.applyGlobalBasics()
	if b.timeMode == timeModeRange {
		b.startRaw = parseDateTimeInputToRFC3339(b.globalStartRaw)
		b.endRaw = parseDateTimeInputToRFC3339(b.globalEndRaw)
		b.setTimeframeByLabel("all")
		return
	}
	b.setTimeframeByLabel(b.globalTimeValue)
	b.startRaw = ""
	b.endRaw = ""
}

func (b *queryBuilder) applyGlobalBasics() {
	b.environment = defaultText(strings.TrimSpace(b.globalEnv), b.environment)
	if strings.EqualFold(strings.TrimSpace(b.globalTimeValue), "edit time range") {
		b.timeMode = timeModeRange
	} else {
		b.timeMode = timeModeSince
	}
	b.limit = parsePositiveIntOrFallback(b.globalLimitRaw, b.limit)
	b.spss = parsePositiveIntOrFallback(b.globalSPSSRaw, b.spss)
}

func (b *queryBuilder) startGlobalRangeForm() {
	fromTime, hasFrom := parseRFC3339WithZoneOptional(b.startRaw)
	if !hasFrom {
		fromTime = time.Now().UTC().Truncate(time.Second)
	}
	toTime, hasTo := parseRFC3339WithZoneOptional(b.endRaw)
	if !hasTo {
		toTime = fromTime.Add(time.Hour)
	}
	if toTime.Before(fromTime) || toTime.Equal(fromTime) {
		toTime = fromTime.Add(time.Hour)
	}
	b.rangeFrom = segmentsFromTime(fromTime)
	b.rangeTo = segmentsFromTime(toTime)
	b.rangeTZ = timezoneFromTime(fromTime)
	b.rangeDurationRaw = formatDurationInput(toTime.Sub(fromTime))
	b.rangeDurationTouched = false
	b.rangeFocus = 0
	b.rangeError = ""
	b.mode = "global_range_form"
	b.globalForm = nil
	b.globalFiltering = false
}

func (b *queryBuilder) toClauses() []string {
	clauses := make([]string, 0, len(b.rows))
	for _, row := range b.rows {
		field := strings.TrimSpace(row.Field)
		op := strings.TrimSpace(row.Operator)
		value := strings.TrimSpace(row.Value)
		if field == "" || op == "" {
			continue
		}
		if value == "" {
			value = `""`
		}
		clauses = append(clauses, field+op+value)
	}
	return clauses
}

func (b *queryBuilder) snapshotResult() queryBuilderResult {
	query := strings.TrimSpace(b.queryText)
	if query == "" {
		query = traceql.CompileClauses(b.toClauses())
	}
	res := queryBuilderResult{
		Query:       query,
		Environment: b.environment,
		Limit:       b.limit,
		SPSS:        b.spss,
		Since:       b.selectedSince(),
	}
	if b.timeMode == timeModeRange {
		if start, ok := parseRFC3339Optional(b.startRaw); ok {
			res.StartAt = start
			res.HasStartAt = true
		}
		if end, ok := parseRFC3339Optional(b.endRaw); ok {
			res.EndAt = end
			res.HasEndAt = true
		}
	}
	return res
}

func parseRFC3339Optional(raw string) (time.Time, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, trimmed); err == nil {
		return t.UTC(), true
	}
	localLayouts := []string{
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05.000",
		"2006-01-02T15:04:05.000000",
		"2006-01-02T15:04:05.000000000",
	}
	for _, layout := range localLayouts {
		if t, err := time.ParseInLocation(layout, trimmed, time.Local); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func resolveTimeMode(hasStartAt, hasEndAt bool) string {
	if hasStartAt || hasEndAt {
		return timeModeRange
	}
	return timeModeSince
}

func normalizeTimeMode(raw string) string {
	if strings.EqualFold(strings.TrimSpace(raw), timeModeRange) {
		return timeModeRange
	}
	return timeModeSince
}

var dateTimeInputPattern = regexp.MustCompile(`^\s*(\d{2})\s*/\s*(\d{2})\s*/\s*(\d{4})\s+(\d{2})\s*:\s*(\d{2})\s*:\s*(\d{2})\s*(?:(Z|z)|([+-])\s*(\d{2}):(\d{2}))\s*$`)

func formatDateTimeInput(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		t2, ok := parseRFC3339Optional(trimmed)
		if !ok {
			return ""
		}
		t = t2
	}
	_, offsetSeconds := t.Zone()
	if offsetSeconds == 0 {
		return fmt.Sprintf("%02d / %02d / %04d  %02d : %02d : %02d  Z", t.Day(), int(t.Month()), t.Year(), t.Hour(), t.Minute(), t.Second())
	}
	sign := "+"
	if offsetSeconds < 0 {
		sign = "-"
		offsetSeconds = -offsetSeconds
	}
	offsetHour := offsetSeconds / 3600
	offsetMin := (offsetSeconds % 3600) / 60
	return fmt.Sprintf("%02d / %02d / %04d  %02d : %02d : %02d  %s %02d:%02d", t.Day(), int(t.Month()), t.Year(), t.Hour(), t.Minute(), t.Second(), sign, offsetHour, offsetMin)
}

func validateDateTimeInput(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	_, err := parseDateTimeInput(raw)
	if err != nil {
		return err
	}
	return nil
}

func parseDateTimeInputToRFC3339(raw string) string {
	t, err := parseDateTimeInput(raw)
	if err != nil {
		return ""
	}
	return t.Format(time.RFC3339)
}

func parseDateTimeInput(raw string) (time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, fmt.Errorf("empty value")
	}
	matches := dateTimeInputPattern.FindStringSubmatch(trimmed)
	if len(matches) != 11 {
		return time.Time{}, fmt.Errorf("use DD / MM / YYYY  HH : mm : ss  + 00:00 (or Z)")
	}

	day, _ := strconv.Atoi(matches[1])
	month, _ := strconv.Atoi(matches[2])
	year, _ := strconv.Atoi(matches[3])
	hour, _ := strconv.Atoi(matches[4])
	minute, _ := strconv.Atoi(matches[5])
	second, _ := strconv.Atoi(matches[6])
	offset := 0
	if strings.TrimSpace(matches[7]) == "" {
		sign := matches[8]
		offsetHour, _ := strconv.Atoi(matches[9])
		offsetMin, _ := strconv.Atoi(matches[10])

		if offsetHour > 14 {
			return time.Time{}, fmt.Errorf("timezone hour must be 00..14")
		}
		if offsetMin > 59 {
			return time.Time{}, fmt.Errorf("timezone minutes must be 00..59")
		}
		if offsetHour == 14 && offsetMin != 0 {
			return time.Time{}, fmt.Errorf("timezone 14 requires minutes 00")
		}

		offset = offsetHour*3600 + offsetMin*60
		if sign == "-" {
			offset = -offset
		}
	}

	if month < 1 || month > 12 {
		return time.Time{}, fmt.Errorf("month must be 01..12")
	}
	if day < 1 || day > 31 {
		return time.Time{}, fmt.Errorf("day must be 01..31")
	}
	if hour > 23 {
		return time.Time{}, fmt.Errorf("hour must be 00..23")
	}
	if minute > 59 {
		return time.Time{}, fmt.Errorf("minute must be 00..59")
	}
	if second > 59 {
		return time.Time{}, fmt.Errorf("second must be 00..59")
	}
	zone := time.FixedZone("input", offset)
	t := time.Date(year, time.Month(month), day, hour, minute, second, 0, zone)
	if t.Year() != year || int(t.Month()) != month || t.Day() != day {
		return time.Time{}, fmt.Errorf("invalid calendar date")
	}
	return t, nil
}

func parsePositiveInt(raw string) (int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, fmt.Errorf("value is required")
	}
	value, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, fmt.Errorf("must be a whole number")
	}
	if value <= 0 {
		return 0, fmt.Errorf("must be greater than zero")
	}
	return value, nil
}

func parsePositiveIntOrFallback(raw string, fallback int) int {
	value, err := parsePositiveInt(raw)
	if err != nil {
		if fallback <= 0 {
			return 1
		}
		return fallback
	}
	return value
}

func (b *queryBuilder) syncQueryTextFromRows() {
	b.queryText = traceql.CompileClauses(b.toClauses())
	b.queryCursor = len([]rune(b.queryText))
}

func (b *queryBuilder) syncRowsFromQueryText() {
	clauses := traceql.SplitClauses(b.queryText)
	rows := make([]queryRow, 0, len(clauses))
	for _, clause := range clauses {
		field, op, value, ok := parseClause(clause)
		if !ok {
			rows = append(rows, queryRow{Field: "name", Operator: "=", Value: strings.TrimSpace(clause)})
			continue
		}
		rows = append(rows, queryRow{Field: field, Operator: op, Value: strings.TrimSpace(value)})
	}
	b.rows = rows
	if b.row > len(b.rows)+2 {
		b.row = len(b.rows) + 2
	}
}

func (b *queryBuilder) refreshTableSupport(knownQueryError string) {
	reason := strings.TrimSpace(knownQueryError)
	if reason != "" {
		lower := strings.ToLower(reason)
		if strings.Contains(lower, "syntax") || strings.Contains(lower, "parse") || strings.Contains(lower, "unexpected") || strings.Contains(lower, "invalid") {
			b.tableDisabled = true
			b.tableReason = reason
			if b.row >= 0 && b.row <= len(b.rows) {
				b.row = len(b.rows) + 2
			}
			return
		}
	}
	q := strings.TrimSpace(b.queryText)
	if q == "" {
		b.tableDisabled = false
		b.tableReason = ""
		return
	}
	if strings.Contains(q, "||") || strings.Contains(q, "(") || strings.Contains(q, ")") {
		b.tableDisabled = true
		b.tableReason = "complex query (OR/grouping) is not supported in table mode"
		if b.row >= 0 && b.row <= len(b.rows) {
			b.row = len(b.rows) + 2
		}
		return
	}
	if strings.Count(q, "{") != strings.Count(q, "}") {
		b.tableDisabled = true
		b.tableReason = "unbalanced braces in query"
		if b.row >= 0 && b.row <= len(b.rows) {
			b.row = len(b.rows) + 2
		}
		return
	}
	b.tableDisabled = false
	b.tableReason = ""
}

func (b *queryBuilder) selectedSince() time.Duration {
	if b.timeMode == timeModeRange {
		return 0
	}
	if len(b.timeframe) == 0 {
		return 0
	}
	if b.timeIdx < 0 {
		b.timeIdx = 0
	}
	if b.timeIdx >= len(b.timeframe) {
		b.timeIdx = len(b.timeframe) - 1
	}
	return b.timeframe[b.timeIdx].Since
}

func (b *queryBuilder) activeTimeSummary() string {
	if b.timeMode == timeModeRange {
		return "range"
	}
	return b.selectedTimeframeLabel()
}

func (b *queryBuilder) selectedTimeframeLabel() string {
	if len(b.timeframe) == 0 {
		return "all"
	}
	if b.timeIdx < 0 {
		b.timeIdx = 0
	}
	if b.timeIdx >= len(b.timeframe) {
		b.timeIdx = len(b.timeframe) - 1
	}
	return b.timeframe[b.timeIdx].Label
}

func (b *queryBuilder) setTimeframeByLabel(label string) {
	for i, option := range b.timeframe {
		if strings.EqualFold(option.Label, strings.TrimSpace(label)) {
			b.timeIdx = i
			return
		}
	}
}

func (b *queryBuilder) cycleTimeframe(direction int) {
	if len(b.timeframe) == 0 {
		return
	}
	b.timeIdx = (b.timeIdx + direction + len(b.timeframe)) % len(b.timeframe)
}

func defaultText(value, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

const (
	rangeFocusTZKind   = 12
	rangeFocusTZHour   = 13
	rangeFocusTZMinute = 14
	rangeFocusDuration = 15
	rangeEditorFieldCount = 16
)

func (b *queryBuilder) viewGlobalRangeForm() string {
	var lines []string
	lines = append(lines, "query settings: time range")
	lines = append(lines, "")
	lines = append(lines, mutedStyle.Render("single-line segmented editor | left/right/tab move | up/down switch from/to | enter apply | esc back"))
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("From %s", b.renderSegmentsLine(b.rangeFrom, 0)))
	lines = append(lines, fmt.Sprintf("To   %s", b.renderSegmentsLine(b.rangeTo, 6)))
	lines = append(lines, fmt.Sprintf("TZ   %s", b.renderTimezoneLine()))
	lines = append(lines, fmt.Sprintf("Dur  %s", b.renderDurationField()))
	if strings.TrimSpace(b.rangeError) != "" {
		lines = append(lines, "")
		lines = append(lines, summaryWarnStyle.Render(b.rangeError))
	}
	return strings.Join(lines, "\n")
}

func (b *queryBuilder) renderSegmentsLine(s dateTimeSegments, focusBase int) string {
	parts := []string{
		b.renderSegmentToken(s.Day, "DD", focusBase+0), " / ",
		b.renderSegmentToken(s.Month, "MM", focusBase+1), " / ",
		b.renderSegmentToken(s.Year, "YYYY", focusBase+2), "  ",
		b.renderSegmentToken(s.Hour, "HH", focusBase+3), " : ",
		b.renderSegmentToken(s.Minute, "mm", focusBase+4), " : ",
		b.renderSegmentToken(s.Second, "ss", focusBase+5),
	}
	return strings.Join(parts, "")
}

func (b *queryBuilder) renderTimezoneLine() string {
	kindPlaceholder := "+/-/Z"
	kind := b.rangeTZ.Kind
	if strings.TrimSpace(kind) == "" {
		kind = "+"
	}
	line := b.renderSegmentToken(kind, kindPlaceholder, rangeFocusTZKind)
	line += " " + b.renderSegmentToken(b.rangeTZ.Hour, "00", rangeFocusTZHour)
	line += ":" + b.renderSegmentToken(b.rangeTZ.Minute, "00", rangeFocusTZMinute)
	line += mutedStyle.Render("  (set Z for UTC)")
	return line
}

func (b *queryBuilder) renderDurationField() string {
	value := b.rangeDurationRaw
	if strings.TrimSpace(value) == "" {
		value = "1h"
	}
	token := "[" + value + "]"
	if b.rangeFocus == rangeFocusDuration {
		return titleStyle.Render(token) + mutedStyle.Render("  (from + duration => to)")
	}
	return token + mutedStyle.Render("  (from + duration => to)")
}

func (b *queryBuilder) renderSegmentToken(value, placeholder string, idx int) string {
	display := value
	if strings.TrimSpace(display) == "" {
		display = placeholder
	}
	token := "[" + display + "]"
	if b.rangeFocus == idx {
		return titleStyle.Render(token)
	}
	if strings.TrimSpace(value) == "" {
		return mutedStyle.Render(token)
	}
	return token
}

func (b *queryBuilder) rangeSegmentIndex(focus int) int {
	if focus >= 0 && focus <= 5 {
		return focus
	}
	if focus >= 6 && focus <= 11 {
		return focus - 6
	}
	if focus == rangeFocusTZKind {
		return 6
	}
	if focus == rangeFocusTZHour {
		return 7
	}
	if focus == rangeFocusTZMinute {
		return 8
	}
	return -1
}

func (b *queryBuilder) segmentLenByFocus(focus int) int {
	switch b.rangeSegmentIndex(focus) {
	case 0, 1, 3, 4, 5, 7, 8:
		return 2
	case 2:
		return 4
	case 6:
		return 1
	default:
		return 0
	}
}

func (b *queryBuilder) getRangeSegmentValue(focus int) string {
	if focus >= 0 && focus <= 5 {
		return getDateSegmentValue(&b.rangeFrom, focus)
	}
	if focus >= 6 && focus <= 11 {
		return getDateSegmentValue(&b.rangeTo, focus-6)
	}
	switch focus {
	case rangeFocusTZKind:
		return defaultText(b.rangeTZ.Kind, "+")
	case rangeFocusTZHour:
		return b.rangeTZ.Hour
	case rangeFocusTZMinute:
		return b.rangeTZ.Minute
	default:
		return ""
	}
}

func (b *queryBuilder) setRangeSegmentValue(focus int, value string) {
	if focus >= 0 && focus <= 5 {
		setDateSegmentValue(&b.rangeFrom, focus, value)
		return
	}
	if focus >= 6 && focus <= 11 {
		setDateSegmentValue(&b.rangeTo, focus-6, value)
		return
	}
	switch focus {
	case rangeFocusTZKind:
		switch strings.ToUpper(strings.TrimSpace(value)) {
		case "Z":
			b.rangeTZ.Kind = "Z"
			b.rangeTZ.Hour = "00"
			b.rangeTZ.Minute = "00"
		case "-":
			b.rangeTZ.Kind = "-"
		default:
			b.rangeTZ.Kind = "+"
		}
	case rangeFocusTZHour:
		b.rangeTZ.Hour = value
	case rangeFocusTZMinute:
		b.rangeTZ.Minute = value
	}
}

func getDateSegmentValue(seg *dateTimeSegments, idx int) string {
	switch idx {
	case 0:
		return seg.Day
	case 1:
		return seg.Month
	case 2:
		return seg.Year
	case 3:
		return seg.Hour
	case 4:
		return seg.Minute
	case 5:
		return seg.Second
	default:
		return ""
	}
}

func setDateSegmentValue(seg *dateTimeSegments, idx int, value string) {
	switch idx {
	case 0:
		seg.Day = value
	case 1:
		seg.Month = value
	case 2:
		seg.Year = value
	case 3:
		seg.Hour = value
	case 4:
		seg.Minute = value
	case 5:
		seg.Second = value
	}
}

func (b *queryBuilder) appendRangeSegmentValue(focus int, digit string) bool {
	if focus == rangeFocusTZKind || focus == rangeFocusDuration {
		return false
	}
	current := b.getRangeSegmentValue(focus)
	maxLen := b.segmentLenByFocus(focus)
	if maxLen <= 0 {
		return false
	}
	if len(current) >= maxLen {
		current = ""
	}
	b.setRangeSegmentValue(focus, current+digit)
	return true
}

func (b *queryBuilder) popRangeSegmentValue(focus int) bool {
	if focus == rangeFocusTZKind || focus == rangeFocusDuration {
		return false
	}
	current := b.getRangeSegmentValue(focus)
	if current == "" {
		return false
	}
	b.setRangeSegmentValue(focus, current[:len(current)-1])
	return true
}

func (b *queryBuilder) rangeInputsFromSegments() (string, string, error) {
	fromTime, tzNorm, err := buildTimeFromSegments(b.rangeFrom, b.rangeTZ, time.Now().UTC())
	if err != nil {
		return "", "", fmt.Errorf("from: %w", err)
	}
	toTime, _, err := buildTimeFromSegments(b.rangeTo, tzNorm, fromTime.Add(time.Hour))
	if err != nil {
		return "", "", fmt.Errorf("to: %w", err)
	}
	if !toTime.After(fromTime) {
		return "", "", fmt.Errorf("to must be after from")
	}
	b.rangeTZ = tzNorm
	b.rangeFrom = segmentsFromTime(fromTime)
	b.rangeTo = segmentsFromTime(toTime)
	b.rangeDurationRaw = formatDurationInput(toTime.Sub(fromTime))
	return formatTimeWithTimezone(fromTime, tzNorm), formatTimeWithTimezone(toTime, tzNorm), nil
}

func (b *queryBuilder) refreshRangeDerivedAfterEdit(focus int) {
	if focus >= 6 && focus <= 11 {
		b.refreshDurationFromRange()
		return
	}
	b.refreshRangeToFromDuration()
}

func (b *queryBuilder) refreshRangeToFromDuration() {
	fromTime, tzNorm, err := buildTimeFromSegments(b.rangeFrom, b.rangeTZ, time.Now().UTC())
	if err != nil {
		return
	}
	b.rangeTZ = tzNorm
	trimmed := strings.TrimSpace(b.rangeDurationRaw)
	duration := time.Hour
	if trimmed == "" {
		if !b.rangeDurationTouched {
			b.rangeDurationRaw = "1h"
		} else {
			return
		}
	} else {
		parsed, ok := parseDurationFlexible(trimmed)
		if !ok {
			return
		}
		duration = parsed
	}
	toTime := fromTime.Add(duration)
	b.rangeTo = segmentsFromTime(toTime)
}

func (b *queryBuilder) refreshDurationFromRange() {
	fromTime, tzNorm, err := buildTimeFromSegments(b.rangeFrom, b.rangeTZ, time.Now().UTC())
	if err != nil {
		return
	}
	toTime, _, err := buildTimeFromSegments(b.rangeTo, tzNorm, fromTime.Add(time.Hour))
	if err != nil {
		return
	}
	if !toTime.After(fromTime) {
		return
	}
	b.rangeTZ = tzNorm
	b.rangeDurationRaw = formatDurationInput(toTime.Sub(fromTime))
}

func buildTimeFromSegments(seg dateTimeSegments, tz timezoneSegments, fallback time.Time) (time.Time, timezoneSegments, error) {
	normTZ, zone, err := normalizeTimezoneSegments(tz)
	if err != nil {
		return time.Time{}, timezoneSegments{}, err
	}
	fallbackInZone := fallback.In(zone)
	day, dayNorm, err := normalizeSegmentNumber(seg.Day, 2, fallbackInZone.Day())
	if err != nil {
		return time.Time{}, timezoneSegments{}, fmt.Errorf("day must be 01..31")
	}
	month, monthNorm, err := normalizeSegmentNumber(seg.Month, 2, int(fallbackInZone.Month()))
	if err != nil {
		return time.Time{}, timezoneSegments{}, fmt.Errorf("month must be 01..12")
	}
	year, yearNorm, err := normalizeSegmentNumber(seg.Year, 4, fallbackInZone.Year())
	if err != nil {
		return time.Time{}, timezoneSegments{}, fmt.Errorf("invalid year")
	}
	hour, hourNorm, err := normalizeSegmentNumber(seg.Hour, 2, fallbackInZone.Hour())
	if err != nil {
		return time.Time{}, timezoneSegments{}, fmt.Errorf("hour must be 00..23")
	}
	minute, minuteNorm, err := normalizeSegmentNumber(seg.Minute, 2, fallbackInZone.Minute())
	if err != nil {
		return time.Time{}, timezoneSegments{}, fmt.Errorf("minute must be 00..59")
	}
	second, secondNorm, err := normalizeSegmentNumber(seg.Second, 2, fallbackInZone.Second())
	if err != nil {
		return time.Time{}, timezoneSegments{}, fmt.Errorf("second must be 00..59")
	}

	seg.Day = dayNorm
	seg.Month = monthNorm
	seg.Year = yearNorm
	seg.Hour = hourNorm
	seg.Minute = minuteNorm
	seg.Second = secondNorm

	if month < 1 || month > 12 || day < 1 || day > 31 || hour > 23 || minute > 59 || second > 59 || year < 1 {
		return time.Time{}, timezoneSegments{}, fmt.Errorf("invalid date/time values")
	}

	t := time.Date(year, time.Month(month), day, hour, minute, second, 0, zone)
	if t.Year() != year || int(t.Month()) != month || t.Day() != day {
		return time.Time{}, timezoneSegments{}, fmt.Errorf("invalid calendar date")
	}
	return t, normTZ, nil
}

func normalizeTimezoneSegments(tz timezoneSegments) (timezoneSegments, *time.Location, error) {
	kind := strings.ToUpper(strings.TrimSpace(tz.Kind))
	if kind == "" {
		kind = "+"
	}
	if kind == "Z" {
		norm := timezoneSegments{Kind: "Z", Hour: "00", Minute: "00"}
		return norm, time.UTC, nil
	}
	if kind != "+" && kind != "-" {
		kind = "+"
	}
	hour, hourNorm, err := normalizeSegmentNumber(tz.Hour, 2, 0)
	if err != nil {
		return timezoneSegments{}, nil, fmt.Errorf("timezone hour must be 00..14")
	}
	minute, minuteNorm, err := normalizeSegmentNumber(tz.Minute, 2, 0)
	if err != nil {
		return timezoneSegments{}, nil, fmt.Errorf("timezone minutes must be 00..59")
	}
	if hour > 14 {
		return timezoneSegments{}, nil, fmt.Errorf("timezone hour must be 00..14")
	}
	if minute > 59 {
		return timezoneSegments{}, nil, fmt.Errorf("timezone minutes must be 00..59")
	}
	if hour == 14 && minute != 0 {
		return timezoneSegments{}, nil, fmt.Errorf("timezone 14 requires minutes 00")
	}
	offset := hour*3600 + minute*60
	if kind == "-" {
		offset = -offset
	}
	norm := timezoneSegments{Kind: kind, Hour: hourNorm, Minute: minuteNorm}
	return norm, time.FixedZone("input", offset), nil
}

func normalizeSegmentNumber(raw string, width int, fallback int) (int, string, error) {
	digits := digitsOnly(raw)
	if digits == "" {
		digits = fmt.Sprintf("%0*d", width, fallback)
	}
	if len(digits) > width {
		digits = digits[len(digits)-width:]
	}
	if len(digits) < width {
		digits = strings.Repeat("0", width-len(digits)) + digits
	}
	value, err := strconv.Atoi(digits)
	if err != nil {
		return 0, "", err
	}
	return value, digits, nil
}

func digitsOnly(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func segmentsFromTime(t time.Time) dateTimeSegments {
	return dateTimeSegments{
		Day:    fmt.Sprintf("%02d", t.Day()),
		Month:  fmt.Sprintf("%02d", int(t.Month())),
		Year:   fmt.Sprintf("%04d", t.Year()),
		Hour:   fmt.Sprintf("%02d", t.Hour()),
		Minute: fmt.Sprintf("%02d", t.Minute()),
		Second: fmt.Sprintf("%02d", t.Second()),
	}
}

func timezoneFromTime(t time.Time) timezoneSegments {
	_, offsetSeconds := t.Zone()
	if offsetSeconds == 0 {
		return timezoneSegments{Kind: "Z", Hour: "00", Minute: "00"}
	}
	kind := "+"
	if offsetSeconds < 0 {
		kind = "-"
		offsetSeconds = -offsetSeconds
	}
	return timezoneSegments{Kind: kind, Hour: fmt.Sprintf("%02d", offsetSeconds/3600), Minute: fmt.Sprintf("%02d", (offsetSeconds%3600)/60)}
}

func formatTimeWithTimezone(t time.Time, tz timezoneSegments) string {
	kind := strings.ToUpper(strings.TrimSpace(tz.Kind))
	if kind == "Z" {
		return fmt.Sprintf("%02d / %02d / %04d  %02d : %02d : %02d  Z", t.Day(), int(t.Month()), t.Year(), t.Hour(), t.Minute(), t.Second())
	}
	if kind != "+" && kind != "-" {
		kind = "+"
	}
	h := defaultText(tz.Hour, "00")
	m := defaultText(tz.Minute, "00")
	return fmt.Sprintf("%02d / %02d / %04d  %02d : %02d : %02d  %s %s:%s", t.Day(), int(t.Month()), t.Year(), t.Hour(), t.Minute(), t.Second(), kind, h, m)
}

func parseDurationOrDefault(raw string, fallback time.Duration) time.Duration {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fallback
	}
	d, err := time.ParseDuration(trimmed)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}

func parseDurationFlexible(raw string) (time.Duration, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, false
	}
	if d, err := time.ParseDuration(trimmed); err == nil && d > 0 {
		return d, true
	}
	if digits := digitsOnly(trimmed); digits != "" && digits == trimmed {
		hours, err := strconv.Atoi(digits)
		if err == nil && hours > 0 {
			return time.Duration(hours) * time.Hour, true
		}
	}
	return 0, false
}

func isDurationRune(r rune) bool {
	if r >= '0' && r <= '9' {
		return true
	}
	switch r {
	case 'h', 'm', 's', 'u', 'n', '.', 'H', 'M', 'S', 'U', 'N', 'µ':
		return true
	default:
		return false
	}
}

func formatDurationInput(d time.Duration) string {
	if d <= 0 {
		return "1h"
	}
	if d%(time.Hour) == 0 {
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	if d%(time.Minute) == 0 {
		return fmt.Sprintf("%dm", int(d/time.Minute))
	}
	return d.Truncate(time.Second).String()
}

func parseRFC3339WithZoneOptional(raw string) (time.Time, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func commonDateSuggestions() []string {
	now := time.Now().UTC()
	return []string{
		now.Format(time.RFC3339),
		now.Add(-time.Hour).Format(time.RFC3339),
		now.Add(-24 * time.Hour).Format(time.RFC3339),
		time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
	}
}

func focusNextFieldCmd() tea.Cmd {
	return func() tea.Msg {
		return tea.KeyMsg{Type: tea.KeyTab}
	}
}

func operatorHint(op string) string {
	switch op {
	case "=":
		return "exact match"
	case "!=":
		return "exclude exact value"
	case "=~":
		return "regex match"
	case "!~":
		return "does not match regex"
	case ">":
		return "greater than"
	case ">=":
		return "greater than or equal"
	case "<":
		return "less than"
	case "<=":
		return "less than or equal"
	default:
		return ""
	}
}

func (b *queryBuilder) componentForRow() int {
	switch b.row {
	case -1:
		return 0
	case len(b.rows) + 1:
		return 2
	case len(b.rows) + 2:
		return 3
	default:
		return 1
	}
}

func (b *queryBuilder) jumpComponent(direction int) {
	if b.tableDisabled {
		allowed := []int{-1, len(b.rows) + 1, len(b.rows) + 2}
		idx := 0
		switch b.row {
		case -1:
			idx = 0
		case len(b.rows) + 1:
			idx = 1
		case len(b.rows) + 2:
			idx = 2
		default:
			idx = 2
		}
		next := (idx + direction + len(allowed)) % len(allowed)
		b.row = allowed[next]
		return
	}
	if b.row >= 0 && b.row <= len(b.rows) {
		b.tableRow = b.row
	}
	current := b.componentForRow()
	next := (current + direction + 4) % 4
	switch next {
	case 0:
		b.row = -1
	case 1:
		if b.tableRow < 0 {
			b.tableRow = 0
		}
		if b.tableRow > len(b.rows) {
			b.tableRow = len(b.rows)
		}
		b.row = b.tableRow
	case 2:
		b.row = len(b.rows) + 1
	case 3:
		b.row = len(b.rows) + 2
	}
}

func (b *queryBuilder) moveRow(direction int) {
	if direction == 0 {
		return
	}
	if b.tableDisabled {
		allowed := []int{-1, len(b.rows) + 1, len(b.rows) + 2}
		idx := 0
		switch b.row {
		case -1:
			idx = 0
		case len(b.rows) + 1:
			idx = 1
		case len(b.rows) + 2:
			idx = 2
		default:
			if direction > 0 {
				idx = 1
			} else {
				idx = 0
			}
		}
		next := idx + direction
		if next < 0 {
			next = 0
		}
		if next >= len(allowed) {
			next = len(allowed) - 1
		}
		b.row = allowed[next]
		return
	}

	maxRow := len(b.rows) + 2
	b.row += direction
	if b.row < -1 {
		b.row = -1
	}
	if b.row > maxRow {
		b.row = maxRow
	}
	if b.row >= 0 && b.row <= len(b.rows) {
		b.tableRow = b.row
	}
}

type queryRow struct {
	Field    string
	Operator string
	Value    string
}

type timeframeOption struct {
	Label       string
	Since       time.Duration
	Description string
}

func parseClause(clause string) (field, op, value string, ok bool) {
	trimmed := strings.TrimSpace(clause)
	if trimmed == "" {
		return "", "", "", false
	}
	operators := []string{"=~", "!~", ">=", "<=", "!=", "=", ">", "<", ":"}
	for _, candidate := range operators {
		idx := strings.Index(trimmed, candidate)
		if idx <= 0 {
			continue
		}
		left := strings.TrimSpace(trimmed[:idx])
		right := strings.TrimSpace(trimmed[idx+len(candidate):])
		if left == "" || right == "" {
			continue
		}
		if candidate == ":" {
			candidate = "="
		}
		return left, candidate, unquoteString(right), true
	}
	return "", "", "", false
}

func unquoteString(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) >= 2 {
		if (trimmed[0] == '"' && trimmed[len(trimmed)-1] == '"') || (trimmed[0] == '\'' && trimmed[len(trimmed)-1] == '\'') || (trimmed[0] == '`' && trimmed[len(trimmed)-1] == '`') {
			return trimmed[1 : len(trimmed)-1]
		}
	}
	return trimmed
}
