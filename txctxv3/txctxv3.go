// Package txctxv3 is the Fiber v3 adapter for the txcore engine. See the
// sibling txctx package for Fiber v2.
package txctxv3

import (
	"context"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"

	"github.com/adrielcodeco/go-tools/txcore"
)

// Config re-exports the shared core configuration.
type Config = txcore.Config

// BoolPtr is a helper for setting Config.LazyTx inline.
func BoolPtr(v bool) *bool { return txcore.BoolPtr(v) }

// ContextExtractor is called once per request to obtain the base context for
// the transaction holder. When provided to Middleware it is used instead of
// c.Context().
//
// When to use it: Elastic APM stores its transaction on the fasthttp
// RequestCtx, while Fiber v3 keeps a stdlib context on c.Context(). Without
// an extractor, GORM callbacks that call apmgorm.WithContext lose the APM
// transaction. Supply an extractor whenever apmfiberv3.Middleware is in the
// chain.
//
// Correct 3-middleware ordering:
//
//	app.Use(apmfiberv3.Middleware())   // 1. captures APM tx on RequestCtx
//	app.Use(logfiberv3.Middleware())   // 2. reads APM tx for trace fields
//	app.Use(txctxv3.Middleware(db, cfg, extractor)) // 3. inherits APM ctx
//
// Example extractor for Fiber v3 (inherits the stdlib context which Fiber v3
// populates from the fasthttp RequestCtx before invoking handlers):
//
//	func(c fiber.Ctx) context.Context { return c.Context() }
type ContextExtractor func(c fiber.Ctx) context.Context

// Middleware returns a Fiber v3 middleware that manages a request-scoped
// GORM transaction with timeout-triggered rollback and commit/rollback
// callbacks. An optional ContextExtractor may be supplied to override the
// base context (default: c.Context()).
func Middleware(db *gorm.DB, cfg Config, extractor ...ContextExtractor) fiber.Handler {
	cfg = cfg.WithDefaults()
	lazy := *cfg.LazyTx
	var extract ContextExtractor
	if len(extractor) > 0 {
		extract = extractor[0]
	}

	return func(c fiber.Ctx) error {
		baseCtx := c.Context()
		if extract != nil {
			baseCtx = extract(c)
		}
		reqCtx, cancel := context.WithTimeout(baseCtx, cfg.Timeout)
		defer cancel()

		holder := txcore.NewHolder(db, cfg.CompensationCtx, lazy, cfg.OnCallbackError)
		ctx := txcore.Inject(reqCtx, holder)
		c.SetContext(ctx)

		if !lazy {
			holder.Begin(ctx)
		}

		defer func() {
			if r := recover(); r != nil {
				holder.Rollback()
				panic(r)
			}
		}()

		handlerErr := c.Next()

		if handlerErr != nil || ctx.Err() != nil {
			holder.Rollback()
			return handlerErr
		}

		commitErr, postErr := holder.Commit()
		if commitErr != nil {
			holder.Rollback()
			return commitErr
		}
		return postErr
	}
}

// DB returns the request-scoped *gorm.DB.
func DB(c fiber.Ctx) *gorm.DB { return DBFromCtx(c.Context()) }

// DBFromCtx is the context.Context variant of DB.
func DBFromCtx(ctx context.Context) *gorm.DB {
	return txcore.MustFromCtx(ctx).DB(ctx)
}

// Outside returns a *gorm.DB whose context is decoupled from the request
// cancellation but preserves request values.
func Outside(c fiber.Ctx) *gorm.DB { return OutsideCtx(c.Context()) }

// OutsideCtx is the context.Context variant of Outside.
func OutsideCtx(ctx context.Context) *gorm.DB {
	return txcore.MustFromCtx(ctx).Outside(ctx)
}

// OnRollback registers fn to run if the transaction rolls back.
func OnRollback(c fiber.Ctx, fn func(*gorm.DB) error) { OnRollbackCtx(c.Context(), fn) }

// OnRollbackCtx is the context.Context variant of OnRollback.
func OnRollbackCtx(ctx context.Context, fn func(*gorm.DB) error) {
	txcore.MustFromCtx(ctx).AppendOnRollback(fn)
}

// OnCommit registers fn to run only if the transaction commits successfully.
func OnCommit(c fiber.Ctx, fn func(*gorm.DB) error) { OnCommitCtx(c.Context(), fn) }

// OnCommitCtx is the context.Context variant of OnCommit.
func OnCommitCtx(ctx context.Context, fn func(*gorm.DB) error) {
	txcore.MustFromCtx(ctx).AppendOnCommit(fn)
}
