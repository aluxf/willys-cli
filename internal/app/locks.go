package app

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

type lockNoticeKey struct{}

func acquire(ctx context.Context, file string, shared bool) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	l := flock.New(file)
	var ok bool
	var err error
	if shared {
		ok, err = l.TryRLock()
	} else {
		ok, err = l.TryLock()
	}
	if err == nil && !ok {
		if w, yes := ctx.Value(lockNoticeKey{}).(io.Writer); yes && !strings.HasSuffix(file, "cookies.json.lock") {
			fmt.Fprintln(w, "Waiting for another command. Press Ctrl+C to cancel.")
		}
		if shared {
			ok, err = l.TryRLockContext(ctx, 25*time.Millisecond)
		} else {
			ok, err = l.TryLockContext(ctx, 25*time.Millisecond)
		}
	}
	if err != nil || !ok {
		l.Close()
		if err == nil {
			err = ctx.Err()
		}
		return nil, err
	}
	if err = os.Chmod(file, 0600); err != nil {
		l.Close()
		return nil, err
	}
	return func() { _ = l.Close() }, nil
}

func commandLock(ctx context.Context, p *Profile, o Options) (func(), error) {
	shared := true
	switch o.Command {
	case "checkout", "setup", "slots", "slot":
		shared = false
	case "auth":
		shared = !(len(o.Positionals) == 1 && o.Positionals[0] == "login")
	case "cart":
		shared = !(len(o.Positionals) == 1 && o.Positionals[0] == "reset")
	case "payment":
		shared = !(len(o.Positionals) == 1 && o.Positionals[0] == "recover")
	case "session":
		shared = o.Values["import-cookies"] == ""
	}
	release, err := acquire(ctx, filepath.Join(p.Path, "profile.lock"), shared)
	if err != nil {
		return nil, err
	}
	if (o.Command == "set" || o.Command == "remove") && len(o.Positionals) > 0 {
		key := fmt.Sprintf("product-%x.lock", sha256.Sum256([]byte(o.Positionals[0])))
		productRelease, e := acquire(ctx, filepath.Join(p.Path, key), false)
		if e != nil {
			release()
			return nil, e
		}
		return func() { productRelease(); release() }, nil
	}
	return release, nil
}
