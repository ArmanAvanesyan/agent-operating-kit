//go:build !linux && !darwin

package toolisolation

import "errors"

func executePlatform(Policy, Request) (Result, error) {
	return Result{}, errors.New("unsupported platform")
}

func executePlatformFD(Policy, Request, int) (Result, error) {
	return Result{}, errors.New("unsupported platform")
}
