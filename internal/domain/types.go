package domain

import "time"

type Trace struct {
	TraceID            string
	RootSpanIDs        []string
	Spans              []*Span
	SpansByID          map[string]*Span
	OperationName      string
	Duration           time.Duration
	StartTime          time.Time
	ServiceCount       int
	ErrorSpanCount     int
	SpanCount          int
	Environment        string
	GrafanaExternalURL string
}

type Span struct {
	ID         string
	ParentID   string
	Service    string
	Name       string
	Kind       string
	Start      time.Time
	End        time.Time
	Duration   time.Duration
	StatusCode string
	StatusMsg  string
	Attributes map[string]any
	Events     []SpanEvent
	Links      []SpanLink
	Children   []*Span
}

func (s Span) HasError() bool {
	return s.StatusCode == "STATUS_CODE_ERROR" || s.StatusCode == "ERROR"
}

type SpanEvent struct {
	Name       string
	Time       time.Time
	Attributes map[string]any
}

type SpanLink struct {
	TraceID    string
	SpanID     string
	Attributes map[string]any
}

type LogEntry struct {
	Timestamp time.Time
	Service   string
	Level     string
	Message   string
	RawLine   string
	JSON      map[string]any
	Labels    map[string]string
}

type Session struct {
	Trace          *Trace
	Logs           []LogEntry
	Environment    string
	GrafanaURL     string
	BetterstackURL string
}

type TraceListItem struct {
	TraceID        string
	OperationName  string
	Service        string
	ErrorSpanCount int
	SpanCount      int
	Duration       time.Duration
	StartTime      time.Time
}
