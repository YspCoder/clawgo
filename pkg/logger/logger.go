package logger

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
	FATAL
)

var (
	logLevelNames = map[LogLevel]string{
		DEBUG: "DEBUG",
		INFO:  "INFO",
		WARN:  "WARN",
		ERROR: "ERROR",
		FATAL: "FATAL",
	}

	currentLevel = INFO
	logger       *Logger
	once         sync.Once
	mu           sync.RWMutex
)

type Logger struct {
	file         *os.File
	filePath     string
	maxSizeBytes int64
	maxAgeDays   int
	fileMu       sync.Mutex
}

type LogEntry struct {
	Level     string                 `json:"level"`
	Timestamp string                 `json:"timestamp"`
	Component string                 `json:"component,omitempty"`
	Code      int                    `json:"code,omitempty"`
	Message   string                 `json:"message,omitempty"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
	Caller    string                 `json:"caller,omitempty"`
}

func init() {
	once.Do(func() {
		logger = &Logger{}
	})
}

func SetLevel(level LogLevel) {
	mu.Lock()
	defer mu.Unlock()
	currentLevel = level
}

func GetLevel() LogLevel {
	mu.RLock()
	defer mu.RUnlock()
	return currentLevel
}

func EnableFileLogging(filePath string) error {
	return EnableFileLoggingWithRotation(filePath, 20, 3)
}

func EnableFileLoggingWithRotation(filePath string, maxSizeMB, maxAgeDays int) error {
	mu.Lock()
	defer mu.Unlock()

	if maxSizeMB <= 0 {
		maxSizeMB = 20
	}
	if maxAgeDays <= 0 {
		maxAgeDays = 3
	}

	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	if logger.file != nil {
		logger.file.Close()
	}

	logger.file = file
	logger.filePath = filePath
	logger.maxSizeBytes = int64(maxSizeMB) * 1024 * 1024
	logger.maxAgeDays = maxAgeDays
	if err := logger.cleanupOldLogFiles(); err != nil {
		log.Println(C0145, err)
	}
	log.Println(C0146, filePath)
	return nil
}

func DisableFileLogging() {
	mu.Lock()
	defer mu.Unlock()

	if logger.file != nil {
		logger.file.Close()
		logger.file = nil
		logger.filePath = ""
		logger.maxSizeBytes = 0
		logger.maxAgeDays = 0
		log.Println(C0147)
	}
}

func logMessage(level LogLevel, component string, code CodeID, fields map[string]interface{}) {
	if level < currentLevel {
		return
	}

	if fields == nil {
		fields = map[string]interface{}{}
	}

	entryCode := int(code)
	entryMessage := extractSystemError(fields)

	entry := LogEntry{
		Level:     logLevelNames[level],
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Component: component,
		Code:      entryCode,
		Message:   entryMessage,
		Fields:    fields,
	}

	if pc, file, line, ok := runtime.Caller(2); ok {
		fn := runtime.FuncForPC(pc)
		if fn != nil {
			entry.Caller = fmt.Sprintf("%s:%d (%s)", file, line, fn.Name())
		}
	}

	if logger.file != nil {
		jsonData, err := json.Marshal(entry)
		if err == nil {
			if err := logger.writeLine(append(jsonData, '\n')); err != nil {
				log.Println(C0148, err)
			}
		}
	}

	var fieldStr string
	if len(fields) > 0 {
		fieldStr = " " + formatFields(fields)
	}

	logLine := fmt.Sprintf("[%s] [%s]%s %s%s",
		entry.Timestamp,
		logLevelNames[level],
		formatComponent(component),
		formatCode(entry.Code),
		fieldStr,
	)
	if entry.Message != "" {
		logLine += " msg=" + entry.Message
	}

	log.Println(logLine)

	if level == FATAL {
		os.Exit(1)
	}
}

func extractSystemError(fields map[string]interface{}) string {
	if fields == nil {
		return ""
	}
	v, ok := fields[FieldError]
	if !ok || v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", v))
}

func formatCode(code int) string {
	if code <= 0 {
		return "code=0"
	}
	return "code=" + strconv.Itoa(code)
}

func (l *Logger) writeLine(line []byte) error {
	l.fileMu.Lock()
	defer l.fileMu.Unlock()

	if l.file == nil {
		return nil
	}

	if l.maxSizeBytes > 0 {
		if err := l.rotateIfNeeded(int64(len(line))); err != nil {
			return err
		}
	}

	_, err := l.file.Write(line)
	return err
}

func (l *Logger) rotateIfNeeded(nextWrite int64) error {
	info, err := l.file.Stat()
	if err != nil {
		return err
	}

	if info.Size()+nextWrite <= l.maxSizeBytes {
		return nil
	}

	if err := l.file.Close(); err != nil {
		return err
	}

	backupPath := fmt.Sprintf("%s.%s", l.filePath, time.Now().UTC().Format("20060102-150405"))
	if err := os.Rename(l.filePath, backupPath); err != nil {
		return err
	}

	file, err := os.OpenFile(l.filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	l.file = file

	return l.cleanupOldLogFiles()
}

func (l *Logger) cleanupOldLogFiles() error {
	if l.maxAgeDays <= 0 || l.filePath == "" {
		return nil
	}

	dir := filepath.Dir(l.filePath)
	base := filepath.Base(l.filePath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	cutoff := time.Now().AddDate(0, 0, -l.maxAgeDays)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		// Only delete rotated files like clawgo.log.20260213-120000
		if !strings.HasPrefix(name, base+".") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}

	return nil
}

func formatComponent(component string) string {
	if component == "" {
		return ""
	}
	return fmt.Sprintf(" %s:", component)
}

func formatFields(fields map[string]interface{}) string {
	var parts []string
	for k, v := range fields {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	return fmt.Sprintf("{%s}", strings.Join(parts, ", "))
}

func Debug(code CodeID) {
	logMessage(DEBUG, "", code, nil)
}

func DebugC(component string, code CodeID) {
	logMessage(DEBUG, component, code, nil)
}

func DebugF(code CodeID, fields map[string]interface{}) {
	logMessage(DEBUG, "", code, fields)
}

func DebugCF(component string, code CodeID, fields map[string]interface{}) {
	logMessage(DEBUG, component, code, fields)
}

func Info(code CodeID) {
	logMessage(INFO, "", code, nil)
}

func InfoC(component string, code CodeID) {
	logMessage(INFO, component, code, nil)
}

func InfoF(code CodeID, fields map[string]interface{}) {
	logMessage(INFO, "", code, fields)
}

func InfoCF(component string, code CodeID, fields map[string]interface{}) {
	logMessage(INFO, component, code, fields)
}

func Warn(code CodeID) {
	logMessage(WARN, "", code, nil)
}

func WarnC(component string, code CodeID) {
	logMessage(WARN, component, code, nil)
}

func WarnF(code CodeID, fields map[string]interface{}) {
	logMessage(WARN, "", code, fields)
}

func WarnCF(component string, code CodeID, fields map[string]interface{}) {
	logMessage(WARN, component, code, fields)
}

func Error(code CodeID) {
	logMessage(ERROR, "", code, nil)
}

func ErrorC(component string, code CodeID) {
	logMessage(ERROR, component, code, nil)
}

func ErrorF(code CodeID, fields map[string]interface{}) {
	logMessage(ERROR, "", code, fields)
}

func ErrorCF(component string, code CodeID, fields map[string]interface{}) {
	logMessage(ERROR, component, code, fields)
}

func Fatal(code CodeID) {
	logMessage(FATAL, "", code, nil)
}

func FatalC(component string, code CodeID) {
	logMessage(FATAL, component, code, nil)
}

func FatalF(code CodeID, fields map[string]interface{}) {
	logMessage(FATAL, "", code, fields)
}

func FatalCF(component string, code CodeID, fields map[string]interface{}) {
	logMessage(FATAL, component, code, fields)
}
