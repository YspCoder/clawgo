package logger

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
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
	Message   string                 `json:"message"`
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
		log.Println("Failed to clean up old log files:", err)
	}
	log.Println("File logging enabled:", filePath)
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
		log.Println("File logging disabled")
	}
}

func logMessage(level LogLevel, component string, message string, fields map[string]interface{}) {
	if level < currentLevel {
		return
	}

	entry := LogEntry{
		Level:     logLevelNames[level],
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Component: component,
		Message:   message,
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
				log.Println("Failed to write file log:", err)
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
		message,
		fieldStr,
	)

	log.Println(logLine)

	if level == FATAL {
		os.Exit(1)
	}
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

func Debug(message string) {
	logMessage(DEBUG, "", message, nil)
}

func DebugC(component string, message string) {
	logMessage(DEBUG, component, message, nil)
}

func DebugF(message string, fields map[string]interface{}) {
	logMessage(DEBUG, "", message, fields)
}

func DebugCF(component string, message string, fields map[string]interface{}) {
	logMessage(DEBUG, component, message, fields)
}

func Info(message string) {
	logMessage(INFO, "", message, nil)
}

func InfoC(component string, message string) {
	logMessage(INFO, component, message, nil)
}

func InfoF(message string, fields map[string]interface{}) {
	logMessage(INFO, "", message, fields)
}

func InfoCF(component string, message string, fields map[string]interface{}) {
	logMessage(INFO, component, message, fields)
}

func Warn(message string) {
	logMessage(WARN, "", message, nil)
}

func WarnC(component string, message string) {
	logMessage(WARN, component, message, nil)
}

func WarnF(message string, fields map[string]interface{}) {
	logMessage(WARN, "", message, fields)
}

func WarnCF(component string, message string, fields map[string]interface{}) {
	logMessage(WARN, component, message, fields)
}

func Error(message string) {
	logMessage(ERROR, "", message, nil)
}

func ErrorC(component string, message string) {
	logMessage(ERROR, component, message, nil)
}

func ErrorF(message string, fields map[string]interface{}) {
	logMessage(ERROR, "", message, fields)
}

func ErrorCF(component string, message string, fields map[string]interface{}) {
	logMessage(ERROR, component, message, fields)
}

func Fatal(message string) {
	logMessage(FATAL, "", message, nil)
}

func FatalC(component string, message string) {
	logMessage(FATAL, component, message, nil)
}

func FatalF(message string, fields map[string]interface{}) {
	logMessage(FATAL, "", message, fields)
}

func FatalCF(component string, message string, fields map[string]interface{}) {
	logMessage(FATAL, component, message, fields)
}
