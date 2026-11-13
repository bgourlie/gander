package cli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/bgourlie/gander/internal/buildinfo"
	"github.com/bgourlie/gander/internal/platform"
	"github.com/bgourlie/gander/internal/self"
	"github.com/bgourlie/gander/internal/version"
)

const selfSynopsis = "self install [--no-modify-path] | self update [version] | self uninstall [-y]"

func cmdSelf(ctx context.Context, env *Env, args []string) error {
	if len(args) == 0 {
		fmt.Fprintf(env.Stderr, "usage: gander %s\n", selfSynopsis)
		return errUsage
	}
	if isHelp(args[0]) {
		fmt.Fprintf(env.Stdout, "usage: gander %s\n", selfSynopsis)
		return errHelp
	}
	exe, err := env.Executable()
	if err != nil {
		return fmt.Errorf("can't find gander's own binary: %w", err)
	}
	s := self.Env{Home: env.Home, Exe: exe, Getenv: env.Getenv, Stderr: env.Stderr}
	switch args[0] {
	case "install":
		return selfInstall(ctx, env, &s, args[1:])
	case "update":
		return selfUpdate(ctx, env, &s, args[1:])
	case "uninstall":
		return selfUninstall(env, &s, args[1:])
	}
	fmt.Fprintf(env.Stderr, "gander self: there's no command %q\nusage: gander %s\n", args[0], selfSynopsis)
	return errUsage
}

func selfInstall(ctx context.Context, env *Env, s *self.Env, args []string) error {
	const synopsis = "self install [--no-modify-path]"
	var noModifyPath bool
	ops, err := flags(env, "self install", synopsis, args, func(fs *flag.FlagSet) {
		fs.BoolVar(&noModifyPath, "no-modify-path", false, "")
	})
	if err != nil {
		return err
	}
	if err := operandCount(env, synopsis, ops, 0, 0); err != nil {
		return err
	}
	// Refuse a machine Goose can't run on before changing anything.
	t, err := platform.Detect()
	if err != nil {
		return err
	}
	if err := platform.Check(t); err != nil {
		return err
	}
	if err := s.Install(!noModifyPath); err != nil {
		return err
	}
	// rustup installs stable the same way: so "goose" works straight away.
	var gooseErr error
	if _, hasDef, err := env.Home.Default(); err == nil && !hasDef {
		v, err := env.NewClient().LatestVersion(ctx)
		if err == nil {
			err = ensure(ctx, env, v, "the latest release", true)
		}
		if err != nil {
			gooseErr = fmt.Errorf("gander is installed, but Goose isn't: %w; once that's fixed, run \"gander install\"", err)
		}
	}
	fmt.Fprint(env.Stderr, s.NextSteps())
	return gooseErr
}

func selfUpdate(ctx context.Context, env *Env, s *self.Env, args []string) error {
	const synopsis = "self update [version]"
	ops, err := flags(env, "self update", synopsis, args, nil)
	if err != nil {
		return err
	}
	if err := operandCount(env, synopsis, ops, 0, 1); err != nil {
		return err
	}
	var want version.Version
	if len(ops) == 1 {
		if want, err = version.Parse(ops[0]); err != nil {
			return err
		}
	}
	t, err := platform.Detect()
	if err != nil {
		return err
	}
	u := &self.Updater{Env: *s, Client: env.NewSelfClient(), Current: buildinfo.Version(), Target: t}
	return u.Update(ctx, want)
}

func selfUninstall(env *Env, s *self.Env, args []string) error {
	const synopsis = "self uninstall [-y]"
	var yes bool
	ops, err := flags(env, "self uninstall", synopsis, args, func(fs *flag.FlagSet) {
		fs.BoolVar(&yes, "y", false, "")
	})
	if err != nil {
		return err
	}
	if err := operandCount(env, synopsis, ops, 0, 0); err != nil {
		return err
	}
	if !yes {
		if !env.StdinIsTerminal {
			fmt.Fprintln(env.Stderr, "gander self uninstall: pass -y to confirm when not running in a terminal")
			return errUsage
		}
		installed, _ := env.Home.Installed()
		fmt.Fprintf(env.Stderr, "This removes gander, its PATH setup, and %d installed Goose versions from %s.\nContinue? [y/N] ", len(installed), env.Home.Dir)
		answer, _ := bufio.NewReader(env.Stdin).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
			fmt.Fprintln(env.Stderr, "Nothing was removed")
			return nil
		}
	}
	return s.Uninstall()
}
