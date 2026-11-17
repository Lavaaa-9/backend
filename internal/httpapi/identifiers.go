package httpapi

import (
	"github.com/gofiber/fiber/v3"
	"spartial.local/backend/internal/modules/identifiers"
)

func (api API) registerIdentifiers(staff fiber.Router) {
	s := identifiers.Service{DB: api.DB, Keys: api.Config.Keys, IndexKey: api.Config.IdentifierIndexKey, PolicyEnabled: api.Config.CarePolicyEnabled}
	staff.Post("/patients/:id/identifiers", api.require("patients.write"), func(c fiber.Ctx) error {
		if e := validID(c.Params("id")); e != nil {
			return e
		}
		var in identifiers.Input
		if e := bind(c, &in); e != nil {
			return e
		}
		out, e := s.Add(c.Context(), actor(c), c.Params("id"), in, c.Get("Idempotency-Key"), rid(c))
		if e != nil {
			return e
		}
		return c.Status(201).JSON(out)
	})
	staff.Get("/patients/:id/identifiers", api.require("patients.read"), func(c fiber.Ctx) error {
		if e := validID(c.Params("id")); e != nil {
			return e
		}
		out, e := s.List(c.Context(), actor(c), c.Params("id"), rid(c))
		if e != nil {
			return e
		}
		return c.JSON(fiber.Map{"items": out})
	})
	staff.Post("/patient-identifiers/search", api.require("patients.read"), func(c fiber.Ctx) error {
		var in identifiers.Input
		if e := bind(c, &in); e != nil {
			return e
		}
		out, e := s.Search(c.Context(), actor(c), in, rid(c))
		if e != nil {
			return e
		}
		return c.JSON(fiber.Map{"patient_ids": out})
	})
}
