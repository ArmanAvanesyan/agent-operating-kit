//go:build !darwin

package toolisolation

import (
	"context"
	"errors"
)

func Run(context.Context, Policy, Request) (Result, error) {
	return Result{}, errors.New("request denied")
}

// The launcher fails closed where the required Darwin Seatbelt boundary is
// unavailable. The worker's filesystem checks alone are not an OS sandbox.
