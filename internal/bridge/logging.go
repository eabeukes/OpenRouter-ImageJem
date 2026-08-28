package bridge

import (
	"encoding/json"
	"io"
	"log"
	"time"
)

type eventLogger struct {
	logger *log.Logger
}

func newEventLogger(w io.Writer) *eventLogger {
	return &eventLogger{logger: log.New(w, "", 0)}
}

func (l *eventLogger) event(fields map[string]any) {
	fields["timestamp"] = time.Now().UTC().Format(time.RFC3339Nano)
	encoded, err := json.Marshal(fields)
	if err != nil {
		l.logger.Printf(`{"level":"error","event":"log_encoding_failed"}`)
		return
	}
	l.logger.Print(string(encoded))
}
