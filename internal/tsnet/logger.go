package tsnet

import (
	"fmt"
	"log/slog"

	"tailscale.com/types/logger"
)

// tsnetLogAdapter converts tsnet's printf-style logging to structured slog logging.
// All TSNet logs are treated as debug level to reduce log chattiness.
func tsnetLogAdapter(serviceName string) logger.Logf {
	return func(format string, args ...any) {
		slog.Debug(fmt.Sprintf(format, args...), slog.String("service", serviceName))
	}
}
