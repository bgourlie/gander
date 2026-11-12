package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bgourlie/gander/internal/install"
	"github.com/bgourlie/gander/internal/release"
	"github.com/bgourlie/gander/internal/resolve"
	"github.com/bgourlie/gander/internal/version"
	"github.com/bgourlie/gander/internal/versionfile"
)

func cmdInstall(ctx context.Context, env *Env, args []string) error {
	const synopsis = "install [version]"
	ops, err := flags(env, "install", synopsis, args, nil)
	if err != nil {
		return err
	}
	if err := operandCount(env, synopsis, ops, 0, 1); err != nil {
		return err
	}
	var v version.Version
	why := ""
	if len(ops) == 1 {
		if v, err = versionArg(ctx, env, ops[0]); err != nil {
			return err
		}
	} else {
		dir, err := env.WorkingDir()
		if err != nil {
			return err
		}
		path, pinned, ok, err := resolve.FindPin(dir)
		if err != nil {
			return err
		}
		if ok {
			v, why = pinned, "pinned by "+path
		} else {
			if v, err = env.NewClient().LatestVersion(ctx); err != nil {
				return err
			}
			why = "the latest release"
		}
	}
	return ensure(ctx, env, v, why, true)
}

func cmdPin(ctx context.Context, env *Env, args []string) error {
	const synopsis = "pin [version]"
	ops, err := flags(env, "pin", synopsis, args, nil)
	if err != nil {
		return err
	}
	if err := operandCount(env, synopsis, ops, 0, 1); err != nil {
		return err
	}
	dir, err := env.WorkingDir()
	if err != nil {
		return err
	}
	var v version.Version
	why := ""
	if len(ops) == 1 {
		if v, err = versionArg(ctx, env, ops[0]); err != nil {
			return err
		}
	} else {
		r, err := env.inUse()
		if err != nil {
			return err
		}
		v, why = r.Version, r.Reason()
	}
	if err := ensure(ctx, env, v, why, true); err != nil {
		return err
	}
	path := filepath.Join(dir, versionfile.Name)
	if err := versionfile.Write(path, v); err != nil {
		return fmt.Errorf("can't write %s: %w", path, err)
	}
	fmt.Fprintf(env.Stderr, "Pinned Goose %s in %s\n", v, path)
	if s := env.Getenv("GANDER_GOOSE"); s != "" {
		fmt.Fprintf(env.Stderr, "Note: GANDER_GOOSE is set to %s, which goose uses instead of this pin while it's set\n", s)
	}
	return nil
}

func cmdDefault(ctx context.Context, env *Env, args []string) error {
	const synopsis = "default <version>"
	ops, err := flags(env, "default", synopsis, args, nil)
	if err != nil {
		return err
	}
	if err := operandCount(env, synopsis, ops, 1, 1); err != nil {
		return err
	}
	v, err := versionArg(ctx, env, ops[0])
	if err != nil {
		return err
	}
	if err := ensure(ctx, env, v, "", false); err != nil {
		return err
	}
	if err := env.Home.SetDefault(v); err != nil {
		return fmt.Errorf("can't write %s: %w", env.Home.DefaultFile(), err)
	}
	fmt.Fprintf(env.Stderr, "Goose %s is now the default\n", v)
	if r, err := env.inUse(); err == nil && r.Source != resolve.Default && r.Version != v {
		fmt.Fprintf(env.Stderr, "Note: here, goose uses %s instead (%s)\n", r.Version, r.Reason())
	}
	return nil
}

func cmdList(ctx context.Context, env *Env, args []string) error {
	const synopsis = "list [--available]"
	var available bool
	ops, err := flags(env, "list", synopsis, args, func(fs *flag.FlagSet) {
		fs.BoolVar(&available, "available", false, "")
	})
	if err != nil {
		return err
	}
	if err := operandCount(env, synopsis, ops, 0, 0); err != nil {
		return err
	}
	installed, err := env.Home.Installed()
	if err != nil {
		return err
	}
	def, hasDef, err := env.Home.Default()
	if err != nil {
		fmt.Fprintf(env.Stderr, "gander: warning: %v\n", err)
	}
	cur, hasCur := current(env)

	notes := func(v version.Version, isInstalled bool) []string {
		var n []string
		if hasDef && def == v {
			n = append(n, "default")
		}
		if hasCur && cur.Version == v {
			n = append(n, "in use here")
		}
		if !isInstalled {
			n = append(n, "not installed")
		}
		return n
	}

	if available {
		return listAvailable(ctx, env, installed, notes)
	}
	if len(installed) == 0 {
		fmt.Fprintln(env.Stderr, `No Goose versions are installed; run "gander install" to install the latest`)
	}
	for _, v := range installed {
		printVersion(env, v, notes(v, true))
	}
	if hasDef && !slices.Contains(installed, def) {
		printVersion(env, def, notes(def, false))
	}
	if hasCur && !cur.Installed && cur.Version != def {
		printVersion(env, cur.Version, notes(cur.Version, false))
	}
	return nil
}

func listAvailable(ctx context.Context, env *Env, installed []version.Version, notes func(version.Version, bool) []string) error {
	rs, err := env.NewClient().List(ctx)
	if err != nil {
		return err
	}
	latest, _ := release.Latest(rs)
	type rel struct {
		v          version.Version
		prerelease bool
	}
	var vs []rel
	for _, r := range rs {
		v, err := version.Parse(r.Tag)
		if r.Draft || err != nil || v.String() != r.Tag {
			continue
		}
		vs = append(vs, rel{v, r.Prerelease || v.Suffix() != ""})
	}
	slices.SortFunc(vs, func(a, b rel) int { return version.Compare(b.v, a.v) })
	for _, r := range vs {
		var n []string
		if r.v == latest {
			n = append(n, "latest")
		}
		if r.prerelease {
			n = append(n, "prerelease")
		}
		isInstalled := slices.Contains(installed, r.v)
		if isInstalled {
			n = append(n, "installed")
		}
		n = append(n, notes(r.v, true)...)
		printVersion(env, r.v, n)
	}
	return nil
}

func printVersion(env *Env, v version.Version, notes []string) {
	if len(notes) == 0 {
		fmt.Fprintln(env.Stdout, v)
		return
	}
	fmt.Fprintf(env.Stdout, "%s (%s)\n", v, strings.Join(notes, ", "))
}

// current resolves the version in use here for list's annotations. Problems are warnings:
// they shouldn't stop the list.
func current(env *Env) (resolve.Result, bool) {
	r, err := env.inUse()
	var none *resolve.NoVersionError
	if err != nil && !errors.As(err, &none) {
		fmt.Fprintf(env.Stderr, "gander: warning: %v\n", err)
	}
	return r, err == nil
}

func cmdCurrent(ctx context.Context, env *Env, args []string) error {
	const synopsis = "current"
	ops, err := flags(env, "current", synopsis, args, nil)
	if err != nil {
		return err
	}
	if err := operandCount(env, synopsis, ops, 0, 0); err != nil {
		return err
	}
	r, err := env.inUse()
	if err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "%s (%s)\n", r.Version, r.Reason())
	switch {
	case r.Installed:
		fmt.Fprintln(env.Stdout, r.Path)
	case r.Named() && env.Getenv("GANDER_AUTO_INSTALL") != "0":
		fmt.Fprintln(env.Stdout, "not installed; goose installs it when it first runs here")
	default:
		fmt.Fprintf(env.Stdout, "not installed; run \"gander install %s\"\n", r.Version)
	}
	return nil
}

func cmdDir(ctx context.Context, env *Env, args []string) error {
	const synopsis = "dir"
	ops, err := flags(env, "dir", synopsis, args, nil)
	if err != nil {
		return err
	}
	if err := operandCount(env, synopsis, ops, 0, 0); err != nil {
		return err
	}
	r, err := env.inUse()
	if err != nil {
		return err
	}
	if !r.Installed {
		return fmt.Errorf("Goose %s (%s) isn't installed; run \"gander install %s\"", r.Version, r.Reason(), r.Version)
	}
	fmt.Fprintln(env.Stdout, env.Home.VersionDir(r.Version))
	return nil
}

func cmdUninstall(ctx context.Context, env *Env, args []string) error {
	const synopsis = "uninstall <version>"
	ops, err := flags(env, "uninstall", synopsis, args, nil)
	if err != nil {
		return err
	}
	if err := operandCount(env, synopsis, ops, 1, 1); err != nil {
		return err
	}
	v, err := version.Parse(ops[0])
	if err != nil {
		return err
	}
	if !env.Home.IsInstalled(v) {
		installed, _ := env.Home.Installed()
		if len(installed) == 0 {
			return fmt.Errorf("Goose %s isn't installed, and no versions are", v)
		}
		names := make([]string, len(installed))
		for i, iv := range installed {
			names[i] = iv.String()
		}
		return fmt.Errorf("Goose %s isn't installed; the installed versions are %s", v, strings.Join(names, ", "))
	}
	if err := install.Uninstall(ctx, env.Home, v, env.Stderr); err != nil {
		return err
	}
	fmt.Fprintf(env.Stderr, "Removed Goose %s\n", v)

	// Clear the default only after the files are gone.
	if def, ok, err := env.Home.Default(); err == nil && ok && def == v {
		if err := env.Home.ClearDefault(); err != nil {
			return fmt.Errorf("removed %s, but can't clear it as the default: %w; delete %s", v, err, env.Home.DefaultFile())
		}
		fmt.Fprintln(env.Stderr, `It was the default, so no default is set now; run "gander default <version>" to pick one`)
	}
	if r, err := env.inUse(); err == nil && r.Version == v && r.Named() {
		fmt.Fprintf(env.Stderr, "Note: it's still %s, so goose will install it again when it runs here\n", r.Reason())
	}
	return nil
}

// ensure installs v if it isn't installed. why, if set, says why this version, for the
// message. With firstDefault, a version installed when no default is set becomes the
// default.
func ensure(ctx context.Context, env *Env, v version.Version, why string, firstDefault bool) error {
	if env.Home.IsInstalled(v) {
		fmt.Fprintf(env.Stderr, "Goose %s is already installed\n", v)
	} else {
		in, err := env.NewInstaller()
		if err != nil {
			return err
		}
		if why != "" {
			fmt.Fprintf(env.Stderr, "Installing Goose %s (%s)\n", v, why)
		} else {
			fmt.Fprintf(env.Stderr, "Installing Goose %s\n", v)
		}
		if _, err := in.Install(ctx, v); err != nil {
			return err
		}
		fmt.Fprintf(env.Stderr, "Installed Goose %s in %s\n", v, env.Home.VersionDir(v))
	}
	if !firstDefault {
		return nil
	}
	_, hasDef, err := env.Home.Default()
	if err != nil || hasDef {
		return nil
	}
	if err := env.Home.SetDefault(v); err != nil {
		return fmt.Errorf("can't write %s: %w", env.Home.DefaultFile(), err)
	}
	fmt.Fprintf(env.Stderr, "Goose %s is now the default\n", v)
	return nil
}

// versionArg reads a version operand, where "latest" means the latest release.
func versionArg(ctx context.Context, env *Env, s string) (version.Version, error) {
	if s == "latest" {
		return env.NewClient().LatestVersion(ctx)
	}
	return version.Parse(s)
}
