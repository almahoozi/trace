package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/almahoozi/trace/internal/traceql"
)

type queryBuilderResult struct {
	Query       string
	Environment string
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
	startRaw      string
	endRaw        string

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
	globalTimeframe string
	globalStartRaw  string
	globalEndRaw    string
	globalFiltering bool
}

const customFieldOptionValue = "__custom_field__"

func newQueryBuilder(query string, fields, environments []string, activeEnv string, activeSince time.Duration, startAt, endAt time.Time, hasStartAt, hasEndAt bool, knownQueryError string) *queryBuilder {
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
		startRaw:     startRaw,
		endRaw:       endRaw,
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
		if keyMsg.String() == "ctrl+enter" || keyMsg.String() == "ctrl+j" {
			b.applyGlobalFormValues()
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
	b.applyGlobalFormValues()
	b.mode = "table"
	b.globalForm = nil
	b.globalFiltering = false
	return result
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

	var lines []string
	lines = append(lines, mutedStyle.Render("up/down move | enter edit/open | d delete row | g settings | ctrl+enter or ctrl+r run | esc cancel"))
	lines = append(lines, "")
	globalPrefix := " "
	if b.row == -1 {
		globalPrefix = ">"
	}
	lines = append(lines, fmt.Sprintf(
		"%s [global settings]: %s=%s %s=%s %s=%s %s=%s",
		globalPrefix,
		mutedStyle.Render("env"), titleStyle.Render(b.environment),
		mutedStyle.Render("timeframe"), titleStyle.Render(b.selectedTimeframeLabel()),
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
	b.globalTimeframe = b.selectedTimeframeLabel()
	b.globalStartRaw = b.startRaw
	b.globalEndRaw = b.endRaw

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

	b.globalForm = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().Title("Environment").Description("press enter to choose environment").Filtering(true).Options(envOptions...).Value(&b.globalEnv),
			huh.NewSelect[string]().Title("Timeframe").Filtering(false).Options(timeOptions...).Value(&b.globalTimeframe),
			huh.NewInput().Title("Start time (RFC3339, optional)").Suggestions(commonDateSuggestions()).Placeholder("2026-07-01T00:00:00Z").Value(&b.globalStartRaw),
			huh.NewInput().Title("End time (RFC3339, optional)").Suggestions(commonDateSuggestions()).Placeholder("2026-07-02T00:00:00Z").Value(&b.globalEndRaw),
		),
	).WithShowHelp(false).WithWidth(max(40, b.width-4)).WithHeight(max(10, b.height-6))
	b.globalFiltering = false
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
	b.environment = defaultText(strings.TrimSpace(b.globalEnv), b.environment)
	b.setTimeframeByLabel(b.globalTimeframe)
	b.startRaw = strings.TrimSpace(b.globalStartRaw)
	b.endRaw = strings.TrimSpace(b.globalEndRaw)
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
		Since:       b.selectedSince(),
	}
	if start, ok := parseRFC3339Optional(b.startRaw); ok {
		res.StartAt = start
		res.HasStartAt = true
	}
	if end, ok := parseRFC3339Optional(b.endRaw); ok {
		res.EndAt = end
		res.HasEndAt = true
	}
	return res
}

func parseRFC3339Optional(raw string) (time.Time, bool) {
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

func commonDateSuggestions() []string {
	now := time.Now().UTC()
	return []string{
		now.Format(time.RFC3339),
		now.Add(-time.Hour).Format(time.RFC3339),
		now.Add(-24 * time.Hour).Format(time.RFC3339),
		time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
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
