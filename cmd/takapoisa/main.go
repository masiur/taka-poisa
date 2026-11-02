// Command takapoisa runs the open-source Taka Poisa server: migrations, API and web UI in one binary.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/masiur/taka-poisa/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx, app.Options{}); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}
