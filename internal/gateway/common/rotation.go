package common

import (
	"context"
	"errors"
	"net/http"

	"anti2api-golang/refactor/internal/credential"
	"anti2api-golang/refactor/internal/vertex"
)

func ShouldRetryWithNextToken(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *vertex.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case http.StatusTooManyRequests, http.StatusUnauthorized, http.StatusForbidden:
			return true
		}
	}
	return false
}

func DoWithRoundRobin[T any](ctx context.Context, store *credential.Store, maxAttempts int, op func(acc *credential.Account) (T, error)) (T, *credential.Account, error) {
	var zero T
	if store == nil {
		return zero, nil, errors.New("credential store is nil")
	}
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastErr error
	var lastAcc *credential.Account

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if ctx.Err() != nil {
			return zero, lastAcc, ctx.Err()
		}

		acc, err := store.GetToken()
		if err != nil {
			return zero, lastAcc, err
		}
		lastAcc = acc

		v, err := op(acc)
		if err == nil {
			return v, acc, nil
		}
		lastErr = err
		if !ShouldRetryWithNextToken(err) {
			return zero, acc, err
		}
	}

	return zero, lastAcc, lastErr
}
