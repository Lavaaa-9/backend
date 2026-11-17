package httpapi

import (
	"github.com/gofiber/fiber/v3"
	"spartial.local/backend/internal/modules/privacy"
	"spartial.local/backend/internal/platform"
	"strconv"
)

func (api API) registerPrivacy(staff fiber.Router) {
	policy := privacy.Policy{DB: api.DB, Enabled: api.Config.CarePolicyEnabled}
	staff.Get("/privacy/alerts", api.require("privacy.report"), func(c fiber.Ctx) error {
		after, n, e := page(c)
		if e != nil {
			return e
		}
		out, e := (privacy.Service{DB: api.DB}).Alerts(c.Context(), actor(c), after, rid(c), n)
		if e != nil {
			return e
		}
		next := ""
		if len(out) == n {
			next = out[len(out)-1].ID
		}
		return c.JSON(fiber.Map{"items": out, "next_cursor": next})
	})
	staff.Get("/privacy/patients/:id/access", api.require("privacy.report"), func(c fiber.Ctx) error {
		if e := validID(c.Params("id")); e != nil {
			return e
		}
		before := int64(9223372036854775807)
		if raw := c.Query("before"); raw != "" {
			v, e := strconv.ParseInt(raw, 10, 64)
			if e != nil || v < 1 {
				return platform.Fail(422, "validation", "Invalid before cursor")
			}
			before = v
		}
		out, e := (privacy.Service{DB: api.DB}).PatientAccess(c.Context(), actor(c), c.Params("id"), rid(c), before)
		if e != nil {
			return e
		}
		return c.JSON(fiber.Map{"items": out})
	})

	staff.Post("/patients/:id/care-assignments", api.require("care.manage"), func(c fiber.Ctx) error {
		if e := validID(c.Params("id")); e != nil {
			return e
		}
		var in privacy.AssignmentInput
		if e := bind(c, &in); e != nil {
			return e
		}
		out, e := policy.Assign(c.Context(), actor(c), c.Params("id"), in, c.Get("Idempotency-Key"), rid(c))
		if e != nil {
			return e
		}
		return c.Status(201).JSON(out)
	})
	staff.Post("/care-assignments/:id/revoke", api.require("care.manage"), func(c fiber.Ctx) error {
		if e := validID(c.Params("id")); e != nil {
			return e
		}
		out, e := policy.RevokeAssignment(c.Context(), actor(c), c.Params("id"), rid(c))
		if e != nil {
			return e
		}
		return c.JSON(out)
	})
	staff.Post("/patients/:id/break-glass", api.require("break_glass.request"), func(c fiber.Ctx) error {
		if e := validID(c.Params("id")); e != nil {
			return e
		}
		var in privacy.BreakGlassInput
		if e := bind(c, &in); e != nil {
			return e
		}
		out, e := policy.BreakGlass(c.Context(), actor(c), c.Params("id"), in, c.Get("Idempotency-Key"), rid(c))
		if e != nil {
			return e
		}
		return c.Status(201).JSON(out)
	})
	staff.Get("/privacy/break-glass", api.require("break_glass.review"), func(c fiber.Ctx) error {
		after, n, e := page(c)
		if e != nil {
			return e
		}
		out, e := policy.Reviews(c.Context(), actor(c), after, rid(c), n)
		if e != nil {
			return e
		}
		next := ""
		if len(out) == n {
			next = out[len(out)-1].ID
		}
		return c.JSON(fiber.Map{"items": out, "next_cursor": next})
	})
	staff.Post("/privacy/break-glass/:id/review", api.require("break_glass.review"), func(c fiber.Ctx) error {
		if e := validID(c.Params("id")); e != nil {
			return e
		}
		var in privacy.ReviewInput
		if e := bind(c, &in); e != nil {
			return e
		}
		out, e := policy.Review(c.Context(), actor(c), c.Params("id"), in, rid(c))
		if e != nil {
			return e
		}
		return c.JSON(out)
	})
}
