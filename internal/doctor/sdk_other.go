//go:build !darwin

package doctor

import "context"

func sdkHint(context.Context, func(string) string, func(...string) ([]byte, error)) []string {
	return nil
}
