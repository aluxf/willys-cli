package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/aluxf/willys-cli/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() { <-ctx.Done(); stop() }()
	info, _ := os.Stdin.Stat()
	interactive := info != nil && info.Mode()&os.ModeCharDevice != 0
	a := app.NewApp(os.Stdin, os.Stdout, os.Stderr, interactive)
	if err := a.Execute(ctx, os.Args[1:]); err != nil {
		os.Exit(app.ReportError(os.Stderr, err, app.JSONRequested(os.Args[1:])))
	}
}
