package logger

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

const (
	loggerINFO  = "INFO"
	loggerWarn  = "WARN"
	loggerError = "ERR"
	loggerDebug = "DEBUG"
)

const (
	logFilePrefix       = "oneapi-"
	claudeLogFilePrefix = "oneapi-claude-"
	logFileSuffix       = ".log"
	logDateFormat       = "20060102"
)

type dailyLogFileManager struct {
	mu          sync.Mutex
	logDir      string
	filePrefix  string
	maxFileKeep int
	currentDate string
	fd          *os.File
}

// claudeRelayManager 专用于 /v1/messages 详细排查日志，独立成文件
// （oneapi-claude-YYYYMMDD.log），不与主日志混写。nil 表示未启用文件日志。
var claudeRelayManager *dailyLogFileManager

type logWriter struct {
	manager *dailyLogFileManager
	console *os.File
}

func (w *logWriter) Write(p []byte) (int, error) {
	n, err := w.console.Write(p)
	if fileErr := w.manager.writeToDailyFile(p); fileErr != nil && err == nil {
		err = fileErr
	}
	return n, err
}

func SetupLogger() {
	if *common.LogDir == "" {
		return
	}
	manager := &dailyLogFileManager{
		logDir:      *common.LogDir,
		filePrefix:  logFilePrefix,
		maxFileKeep: common.LogMaxFileCount,
	}
	if err := manager.ensureLogFileLocked(time.Now()); err != nil {
		log.Fatal("failed to open log file: ", err)
	}
	gin.DefaultWriter = &logWriter{
		manager: manager,
		console: os.Stdout,
	}
	gin.DefaultErrorWriter = &logWriter{
		manager: manager,
		console: os.Stderr,
	}

	// 独立的 /v1/messages 排查日志文件（oneapi-claude-YYYYMMDD.log），
	// 与主日志分离，便于单独抓取 Claude 中转的请求/应答原始数据。
	claudeRelayManager = &dailyLogFileManager{
		logDir:      *common.LogDir,
		filePrefix:  claudeLogFilePrefix,
		maxFileKeep: common.LogMaxFileCount,
	}
}

func (m *dailyLogFileManager) writeToDailyFile(p []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ensureLogFileLocked(time.Now()); err != nil {
		return err
	}
	_, err := m.fd.Write(p)
	return err
}

func (m *dailyLogFileManager) ensureLogFileLocked(now time.Time) error {
	currentDate := now.Format(logDateFormat)
	if m.fd != nil && m.currentDate == currentDate {
		return nil
	}
	if m.fd != nil {
		_ = m.fd.Close()
		m.fd = nil
	}

	prefix := m.filePrefix
	if prefix == "" {
		prefix = logFilePrefix
	}
	logPath := filepath.Join(m.logDir, fmt.Sprintf("%s%s%s", prefix, currentDate, logFileSuffix))
	fd, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	m.fd = fd
	m.currentDate = currentDate

	if m.maxFileKeep > 0 {
		cleanupOldLogFiles(m.logDir, prefix, m.maxFileKeep)
	}
	return nil
}

type logFileInfo struct {
	name      string
	modTime   time.Time
	timestamp time.Time
	hasTime   bool
}

func cleanupOldLogFiles(logDir string, prefix string, maxFileKeep int) {
	if prefix == "" {
		prefix = logFilePrefix
	}
	// 当 prefix 为主日志前缀时，需排除 oneapi-claude- 这类更具体的前缀，
	// 避免把独立的 Claude 排查日志计入主日志的保留配额而误删。
	isMainPrefix := prefix == logFilePrefix
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return
	}
	files := make([]logFileInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, logFileSuffix) {
			continue
		}
		if isMainPrefix && strings.HasPrefix(name, claudeLogFilePrefix) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		parsedTime, hasTime := parseLogFileTime(name, prefix)
		files = append(files, logFileInfo{
			name:      name,
			modTime:   info.ModTime(),
			timestamp: parsedTime,
			hasTime:   hasTime,
		})
	}
	if len(files) <= maxFileKeep {
		return
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].hasTime && files[j].hasTime {
			if files[i].timestamp.Equal(files[j].timestamp) {
				return files[i].name < files[j].name
			}
			return files[i].timestamp.Before(files[j].timestamp)
		}
		if files[i].hasTime != files[j].hasTime {
			return files[i].hasTime
		}
		if files[i].modTime.Equal(files[j].modTime) {
			return files[i].name < files[j].name
		}
		return files[i].modTime.Before(files[j].modTime)
	})
	toDelete := len(files) - maxFileKeep
	for i := 0; i < toDelete; i++ {
		_ = os.Remove(filepath.Join(logDir, files[i].name))
	}
}

func parseLogFileTime(name string, prefix string) (time.Time, bool) {
	if prefix == "" {
		prefix = logFilePrefix
	}
	datePart := strings.TrimSuffix(strings.TrimPrefix(name, prefix), logFileSuffix)
	if t, err := time.Parse("20060102", datePart); err == nil {
		return t, true
	}
	if t, err := time.Parse("20060102150405", datePart); err == nil {
		return t, true
	}
	return time.Time{}, false
}

func LogInfo(ctx context.Context, msg string) {
	logHelper(ctx, loggerINFO, msg)
}

func LogWarn(ctx context.Context, msg string) {
	logHelper(ctx, loggerWarn, msg)
}

func LogError(ctx context.Context, msg string) {
	logHelper(ctx, loggerError, msg)
}

func LogDebug(ctx context.Context, msg string, args ...any) {
	if common.DebugEnabled {
		if len(args) > 0 {
			msg = fmt.Sprintf(msg, args...)
		}
		logHelper(ctx, loggerDebug, msg)
	}
}

func logHelper(ctx context.Context, level string, msg string) {
	writer := gin.DefaultErrorWriter
	if level == loggerINFO {
		writer = gin.DefaultWriter
	}
	id := ctx.Value(common.RequestIdKey)
	if id == nil {
		id = "SYSTEM"
	}
	now := time.Now()
	_, _ = fmt.Fprintf(writer, "[%s] %v | %s | %s \n", level, now.Format("2006/01/02 - 15:04:05"), id, msg)
}

// LogClaudeRelay 将 /v1/messages 中转的详细排查信息写入独立的
// oneapi-claude-YYYYMMDD.log 文件（不与主日志混写，也不输出到控制台）。
// 若未配置日志目录（claudeRelayManager 为 nil），则回退到主日志的 INFO 通道，
// 保证开启开关后即使没有 LogDir 也能在控制台看到数据。
func LogClaudeRelay(ctx context.Context, msg string) {
	id := ctx.Value(common.RequestIdKey)
	if id == nil {
		id = "SYSTEM"
	}
	now := time.Now()
	line := fmt.Sprintf("[CLAUDE] %v | %s | %s \n", now.Format("2006/01/02 - 15:04:05"), id, msg)
	if claudeRelayManager == nil {
		// 未启用文件日志，回退到主日志 INFO 通道。
		_, _ = fmt.Fprint(gin.DefaultWriter, line)
		return
	}
	if err := claudeRelayManager.writeToDailyFile([]byte(line)); err != nil {
		// 文件写入失败时退回主日志，避免排查数据彻底丢失。
		_, _ = fmt.Fprintf(gin.DefaultErrorWriter, "[CLAUDE] write claude relay log failed: %s | %s", err.Error(), line)
	}
}

func LogQuota(quota int) string {
	// 新逻辑：根据额度展示类型输出
	q := float64(quota)
	switch operation_setting.GetQuotaDisplayType() {
	case operation_setting.QuotaDisplayTypeCNY:
		usd := q / common.QuotaPerUnit
		cny := usd * operation_setting.USDExchangeRate
		return fmt.Sprintf("¥%.6f 额度", cny)
	case operation_setting.QuotaDisplayTypeCustom:
		usd := q / common.QuotaPerUnit
		rate := operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate
		symbol := operation_setting.GetGeneralSetting().CustomCurrencySymbol
		if symbol == "" {
			symbol = "¤"
		}
		if rate <= 0 {
			rate = 1
		}
		v := usd * rate
		return fmt.Sprintf("%s%.6f 额度", symbol, v)
	case operation_setting.QuotaDisplayTypeTokens:
		return fmt.Sprintf("%d 点额度", quota)
	default: // USD
		return fmt.Sprintf("＄%.6f 额度", q/common.QuotaPerUnit)
	}
}

func FormatQuota(quota int) string {
	q := float64(quota)
	switch operation_setting.GetQuotaDisplayType() {
	case operation_setting.QuotaDisplayTypeCNY:
		usd := q / common.QuotaPerUnit
		cny := usd * operation_setting.USDExchangeRate
		return fmt.Sprintf("¥%.6f", cny)
	case operation_setting.QuotaDisplayTypeCustom:
		usd := q / common.QuotaPerUnit
		rate := operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate
		symbol := operation_setting.GetGeneralSetting().CustomCurrencySymbol
		if symbol == "" {
			symbol = "¤"
		}
		if rate <= 0 {
			rate = 1
		}
		v := usd * rate
		return fmt.Sprintf("%s%.6f", symbol, v)
	case operation_setting.QuotaDisplayTypeTokens:
		return fmt.Sprintf("%d", quota)
	default:
		return fmt.Sprintf("＄%.6f", q/common.QuotaPerUnit)
	}
}

// LogJson 仅供测试使用 only for test
func LogJson(ctx context.Context, msg string, obj any) {
	jsonStr, err := common.Marshal(obj)
	if err != nil {
		LogError(ctx, fmt.Sprintf("json marshal failed: %s", err.Error()))
		return
	}
	LogDebug(ctx, fmt.Sprintf("%s | %s", msg, string(jsonStr)))
}
