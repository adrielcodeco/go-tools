package httpclient

import (
	"context"

	"github.com/bytedance/sonic"
)

// Request is the generic verb-agnostic helper used by GET / POST / …
// It executes the call and unmarshals the response body into *O.
//
// If the call returns a *StatusError, both the body and the error are
// returned — so callers can still inspect partial responses.
func Request[O any](ctx context.Context, method string, opts RequestOptions) (*O, error) {
	body, err := Do(ctx, method, opts)
	if len(body) == 0 {
		return nil, err
	}
	var out O
	if uErr := sonic.Unmarshal(body, &out); uErr != nil {
		if err != nil {
			return nil, err
		}
		return nil, uErr
	}
	return &out, err
}

// GET issues a GET and decodes the body as O.
func GET[O any](ctx context.Context, opts RequestOptions) (*O, error) {
	return Request[O](ctx, "GET", opts)
}

// POST issues a POST and decodes the body as O.
func POST[O any](ctx context.Context, opts RequestOptions) (*O, error) {
	return Request[O](ctx, "POST", opts)
}

// PUT issues a PUT and decodes the body as O.
func PUT[O any](ctx context.Context, opts RequestOptions) (*O, error) {
	return Request[O](ctx, "PUT", opts)
}

// PATCH issues a PATCH and decodes the body as O.
func PATCH[O any](ctx context.Context, opts RequestOptions) (*O, error) {
	return Request[O](ctx, "PATCH", opts)
}

// DELETE issues a DELETE and decodes the body as O.
func DELETE[O any](ctx context.Context, opts RequestOptions) (*O, error) {
	return Request[O](ctx, "DELETE", opts)
}
