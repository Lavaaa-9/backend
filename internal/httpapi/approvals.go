package httpapi

import (
	"github.com/gofiber/fiber/v3"
	"spartial.local/backend/internal/modules/approvals"
)

func (api API) registerApprovals(staff fiber.Router) {
	s := approvals.Service{DB: api.DB}
	staff.Post("/approvals", api.require("approvals.request"), func(c fiber.Ctx) error {
		var in approvals.Input
		if e := bind(c, &in); e != nil {
			return e
		}
		out, e := s.Create(c.Context(), actor(c), in, c.Get("Idempotency-Key"), rid(c))
		if e != nil {
			return e
		}
		return c.Status(201).JSON(out)
	})
	staff.Get("/approvals", api.require("approvals.review"), func(c fiber.Ctx) error {
		after, n, e := page(c)
		if e != nil {
			return e
		}
		out, e := s.List(c.Context(), actor(c), after, rid(c), n)
		if e != nil {
			return e
		}
		next := ""
		if len(out) == n {
			next = out[len(out)-1].ID
		}
		return c.JSON(fiber.Map{"items": out, "next_cursor": next})
	})
	for _, decision := range []string{"approve", "reject"} {
		staff.Post("/approvals/:id/"+decision, api.require("approvals.review"), func(c fiber.Ctx) error {
			if e := validID(c.Params("id")); e != nil {
				return e
			}
			var in approvals.Decision
			if e := bind(c, &in); e != nil {
				return e
			}
			out, e := s.Decide(c.Context(), actor(c), c.Params("id"), in, decision == "approve", rid(c))
			if e != nil {
				return e
			}
			return c.JSON(out)
		})
	}
}
