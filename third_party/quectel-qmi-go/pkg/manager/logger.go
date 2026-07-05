package manager

import (
	"github.com/sirupsen/logrus"
	"go.uber.org/zap"
)

// ============================================================================
// Logger Interface / 日志接口
// 允许用户注入自己的 logger 实现
// ============================================================================

// Logger defines the logging interface used by the manager
// Logger 定义管理器使用的日志接口
type Logger interface {
	Debug(args ...interface{})
	Debugf(format string, args ...interface{})
	Info(args ...interface{})
	Infof(format string, args ...interface{})
	Warn(args ...interface{})
	Warnf(format string, args ...interface{})
	Error(args ...interface{})
	Errorf(format string, args ...interface{})
	WithField(key string, value interface{}) Logger
	WithError(err error) Logger
}

// ============================================================================
// Logrus Adapter / Logrus 适配器
// ============================================================================

// logrusAdapter wraps logrus.Entry to implement Logger interface
// logrusAdapter 包装 logrus.Entry 实现 Logger 接口
type logrusAdapter struct {
	entry *logrus.Entry
}

// NewLogrusLogger creates a Logger from logrus.Logger / NewLogrusLogger 从 logrus.Logger 创建 Logger
func NewLogrusLogger(l *logrus.Logger) Logger {
	return &logrusAdapter{entry: logrus.NewEntry(l)}
}

// NewLogrusEntryLogger creates a Logger from logrus.Entry / NewLogrusEntryLogger 从 logrus.Entry 创建 Logger
func NewLogrusEntryLogger(e *logrus.Entry) Logger {
	return &logrusAdapter{entry: e}
}

func (a *logrusAdapter) Debug(args ...interface{})                 { a.entry.Debug(args...) }
func (a *logrusAdapter) Debugf(format string, args ...interface{}) { a.entry.Debugf(format, args...) }
func (a *logrusAdapter) Info(args ...interface{})                  { a.entry.Info(args...) }
func (a *logrusAdapter) Infof(format string, args ...interface{})  { a.entry.Infof(format, args...) }
func (a *logrusAdapter) Warn(args ...interface{})                  { a.entry.Warn(args...) }
func (a *logrusAdapter) Warnf(format string, args ...interface{})  { a.entry.Warnf(format, args...) }
func (a *logrusAdapter) Error(args ...interface{})                 { a.entry.Error(args...) }
func (a *logrusAdapter) Errorf(format string, args ...interface{}) { a.entry.Errorf(format, args...) }

func (a *logrusAdapter) WithField(key string, value interface{}) Logger {
	return &logrusAdapter{entry: a.entry.WithField(key, value)}
}

func (a *logrusAdapter) WithError(err error) Logger {
	return &logrusAdapter{entry: a.entry.WithError(err)}
}

// ============================================================================
// Nop Logger / 空日志器
// ============================================================================

// nopLogger discards all log messages / nopLogger 丢弃所有日志消息
type nopLogger struct{}

// NewNopLogger creates a logger that discards all output / NewNopLogger 创建丢弃所有输出的日志器
func NewNopLogger() Logger {
	return &nopLogger{}
}

func (n *nopLogger) Debug(args ...interface{})                      {}
func (n *nopLogger) Debugf(format string, args ...interface{})      {}
func (n *nopLogger) Info(args ...interface{})                       {}
func (n *nopLogger) Infof(format string, args ...interface{})       {}
func (n *nopLogger) Warn(args ...interface{})                       {}
func (n *nopLogger) Warnf(format string, args ...interface{})       {}
func (n *nopLogger) Error(args ...interface{})                      {}
func (n *nopLogger) Errorf(format string, args ...interface{})      {}
func (n *nopLogger) WithField(key string, value interface{}) Logger { return n }
func (n *nopLogger) WithError(err error) Logger                     { return n }

// ============================================================================
// Zap Adapter / Zap 适配器
// ============================================================================

// zapAdapter wraps zap.SugaredLogger to implement Logger interface
// zapAdapter 包装 zap.SugaredLogger 实现 Logger 接口
type zapAdapter struct {
	logger *zap.SugaredLogger
}

// NewZapLogger creates a Logger from zap.Logger
// NewZapLogger 从 zap.Logger 创建 Logger
func NewZapLogger(l *zap.Logger) Logger {
	return &zapAdapter{logger: l.Sugar()}
}

func (a *zapAdapter) Debug(args ...interface{})                 { a.logger.Debug(args...) }
func (a *zapAdapter) Debugf(format string, args ...interface{}) { a.logger.Debugf(format, args...) }
func (a *zapAdapter) Info(args ...interface{})                  { a.logger.Info(args...) }
func (a *zapAdapter) Infof(format string, args ...interface{})  { a.logger.Infof(format, args...) }
func (a *zapAdapter) Warn(args ...interface{})                  { a.logger.Warn(args...) }
func (a *zapAdapter) Warnf(format string, args ...interface{})  { a.logger.Warnf(format, args...) }
func (a *zapAdapter) Error(args ...interface{})                 { a.logger.Error(args...) }
func (a *zapAdapter) Errorf(format string, args ...interface{}) { a.logger.Errorf(format, args...) }

func (a *zapAdapter) WithField(key string, value interface{}) Logger {
	return &zapAdapter{logger: a.logger.With(key, value)}
}

func (a *zapAdapter) WithError(err error) Logger {
	return &zapAdapter{logger: a.logger.With("error", err)}
}
