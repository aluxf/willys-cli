package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/aluxf/willys-cli/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	info, _ := os.Stdin.Stat()
	interactive := info != nil && info.Mode()&os.ModeCharDevice != 0
	a := app.NewApp(os.Stdin, os.Stdout, os.Stderr, interactive)
	if err := a.Execute(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
