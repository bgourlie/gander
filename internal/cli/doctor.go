package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/bgourlie/gander/internal/doctor"
)

func cmdDoctor(ctx context.Context, env *Env, args []string) error {
	const synopsis = "doctor"
	ops, err := flags(env, "doctor", synopsis, args, nil)
	if err != nil {
		return err
	}
	if err := operandCount(env, synopsis, ops, 0, 0); err != nil {
		return err
	}
	dir, dirErr := env.WorkingDir()
	problems := 0
	doctor.Run(ctx, doctor.Env{Home: env.Home, Getenv: env.Getenv, Dir: dir, DirErr: dirErr}, func(c doctor.Check) {
		mark := "ok"
		if !c.OK {
			mark = "problem"
			problems++
		}
		fmt.Fprintf(env.Stdout, "%-8s %s\n", mark, c.Summary)
		for _, d := range c.Detail {
			if d == "" {
				fmt.Fprintln(env.Stdout)
			} else {
				fmt.Fprintf(env.Stdout, "%-8s %s\n", "", d)
			}
		}
	})
	switch problems {
	case 0:
		return nil
	case 1:
		return errors.New("doctor found a problem")
	default:
		return fmt.Errorf("doctor found %d problems", problems)
	}
}
