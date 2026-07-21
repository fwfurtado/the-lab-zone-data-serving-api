package sink

import (
	"context"
	"log/slog"

	"github.com/twmb/franz-go/pkg/kgo"
)

// kgoSlog faz a ponte do logger interno do franz-go para o slog. Sem isso,
// falhas de dial/TLS/SASL/metadata ficam em retry SILENCIOSO e o sink parece
// travado em "sink iniciado" — o PollFetches bloqueia sem nunca reportar.
type kgoSlog struct{ log *slog.Logger }

func (k kgoSlog) Level() kgo.LogLevel {
	if k.log.Enabled(context.Background(), slog.LevelDebug) {
		return kgo.LogLevelDebug
	}
	return kgo.LogLevelInfo
}

func (k kgoSlog) Log(level kgo.LogLevel, msg string, keyvals ...any) {
	var l slog.Level
	switch level {
	case kgo.LogLevelError:
		l = slog.LevelError
	case kgo.LogLevelWarn:
		l = slog.LevelWarn
	case kgo.LogLevelInfo:
		l = slog.LevelInfo
	default:
		l = slog.LevelDebug
	}
	k.log.Log(context.Background(), l, "kgo: "+msg, keyvals...)
}
