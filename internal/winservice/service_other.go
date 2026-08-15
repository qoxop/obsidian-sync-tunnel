//go:build !windows

package winservice

import (
	"context"
	"errors"
)

func Run(_ string, _ func(context.Context) error) error {
	return errors.New("Windows service mode is only available on Windows")
}
