package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"io"
	"log/slog"
	"mime"
	"spartial.local/backend/internal/telemetry"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"spartial.local/backend/api/openapi"
	"spartial.local/backend/internal/config"
	"spartial.local/backend/internal/modules/admin"
	"spartial.local/backend/internal/modules/approvals"
	"spartial.local/backend/internal/modules/audit"
	"spartial.local/backend/internal/modules/billing"
	"spartial.local/backend/internal/modules/clinical"
	"spartial.local/backend/internal/modules/comms"
	"spartial.local/backend/internal/modules/diagnostics"
	"spartial.local/backend/internal/modules/documents"
	"spartial.local/backend/internal/modules/facilities"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/modules/patients"
	"spartial.local/backend/internal/modules/pharmacy"
	"spartial.local/backend/internal/modules/privacy"
	"spartial.local/backend/internal/modules/reports"
	"spartial.local/backend/internal/modules/telehealth"
	"spartial.local/backend/internal/modules/visits"
	"spartial.local/backend/internal/platform"
	"spartial.local/backend/internal/security"
)

type structValidator struct{ v *validator.Validate }

func (v structValidator) Validate(out any) error { return v.v.Struct(out) }

type API struct {
	DB         *pgxpool.Pool
	Identity   identity.Service
	Patients   patients.Service
	Facilities facilities.Service
	Visits     visits.Service
	Reports    reports.Service
	Clinical   clinical.Service
	Diag       diagnostics.Service
	Pharmacy   pharmacy.Service
	Billing    billing.Service
	Telehealth telehealth.Service
	Documents  documents.Service
	Privacy    privacy.Service
	Approvals  approvals.Service
	Comms      comms.Service
	Admin      admin.Service
	Config     config.Config
	Log        *slog.Logger
}

func New(cfg config.Config, db *pgxpool.Pool, log *slog.Logger) (*fiber.App, error) {
	dummy, e := security.HashPassword("invalid-password-never-used")
	if e != nil {
		return nil, e
	}
	timezone := cfg.Timezone
	if timezone == "" {
		timezone = "Africa/Dar_es_Salaam"
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, err
	}
	api := API{
		DB:         db,
		Identity:   identity.Service{DB: db, DummyHash: dummy, Keys: cfg.Keys, Production: cfg.Env == "production"},
		Patients:   patients.Service{DB: db, Location: location, Keys: cfg.Keys, IndexKey: cfg.IdentifierIndexKey},
		Facilities: facilities.Service{DB: db},
		Visits:     visits.Service{DB: db},
		Reports:    reports.Service{DB: db},
		Clinical:   clinical.Service{DB: db},
		Diag:       diagnostics.Service{DB: db},
		Pharmacy:   pharmacy.Service{DB: db},
		Billing:    billing.Service{DB: db, Keys: cfg.Keys, IndexKey: cfg.IdentifierIndexKey},
		Telehealth: telehealth.Service{DB: db},
		Documents:  documents.Service{DB: db, Keys: cfg.Keys},
		Privacy:    privacy.Service{DB: db},
		Approvals:  approvals.Service{DB: db},
		Comms:      comms.Service{DB: db},
		Admin:      admin.Service{DB: db},
		Config:     cfg,
		Log:        log,
	}
	metrics := &telemetry.Metrics{}
	app := fiber.New(fiber.Config{AppName: "Spartial API", BodyLimit: 64 * 1024, TrustProxy: len(cfg.TrustedProxies) > 0, TrustProxyConfig: fiber.TrustProxyConfig{Proxies: cfg.TrustedProxies}, ProxyHeader: "X-Forwarded-For", EnableIPValidation: true, Immutable: true, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, StructValidator: structValidator{validator.New()}, JSONDecoder: strictJSON, ErrorHandler: api.handleError})
	app.Use(func(c fiber.Ctx) error {
		id := uuid.NewString()
		c.Locals("request_id", id)
		c.Set("X-Request-ID", id)
		c.Set("Cache-Control", "no-store")
		c.Set("X-Content-Type-Options", "nosniff")
		if cfg.Env == "production" {
			c.Set("Strict-Transport-Security", "max-age=31536000")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		spanCtx, span := otel.Tracer("spartial.http").Start(ctx, "http.request")
		defer span.End()
		c.SetContext(platform.WithClientMeta(spanCtx, c.IP(), c.Get("User-Agent")))
		start := time.Now()
		var err error
		if cfg.Env == "production" && c.Scheme() != "https" && c.Path() != "/health/live" && c.Path() != "/health/ready" {
			err = platform.Fail(426, "https_required", "HTTPS is required")
		} else {
			err = c.Next()
		}
		status := c.Response().StatusCode()
		if err != nil {
			status = 500
			var p *platform.Error
			if errors.As(platform.Normalize(err), &p) {
				status = p.Status
			}
			var f *fiber.Error
			if errors.As(err, &f) {
				status = f.Code
			}
		}
		route := c.Route().Path
		span.SetName(telemetry.SafeMethod(c.Method()) + " " + route)
		span.SetAttributes(attribute.String("http.request.method", telemetry.SafeMethod(c.Method())), attribute.String("http.route", route), attribute.Int("http.response.status_code", status))
		if status >= 500 {
			span.SetStatus(codes.Error, "request_failed")
		}
		metrics.Observe(c.Method(), route, status, time.Since(start))
		log.Info("http_request", "request_id", id, "method", telemetry.SafeMethod(c.Method()), "route", c.Route().Path, "status", status, "duration_ms", time.Since(start).Milliseconds())
		return err
	})
	app.Use(recover.New())
	app.Use(cors.New(cors.Config{AllowOrigins: cfg.Origins, AllowCredentials: true, AllowMethods: []string{"GET", "POST", "PUT", "OPTIONS"}, AllowHeaders: []string{"Content-Type", "Authorization", "Idempotency-Key"}, ExposeHeaders: []string{"X-Request-ID"}}))
	app.Get("/health/live", func(c fiber.Ctx) error { return c.JSON(fiber.Map{"status": "ok"}) })
	app.Get("/health/ready", func(c fiber.Ctx) error {
		if e := db.Ping(c.Context()); e != nil {
			return platform.Fail(503, "not_ready", "Database unavailable")
		}
		if e := platform.CheckSchema(c.Context(), db); e != nil {
			return e
		}
		return c.JSON(fiber.Map{"status": "ready"})
	})
	app.Get("/api/openapi.json", func(c fiber.Ctx) error { c.Set("Content-Type", "application/json"); return c.Send(openapi.Spec) })
	auth := app.Group("/api/v1/auth")
	auth.Post("/login", api.login)
	auth.Post("/refresh", api.refresh)
	auth.Get("/me", api.authenticate, func(c fiber.Ctx) error { return c.JSON(actor(c)) })
	auth.Post("/logout", api.authenticate, func(c fiber.Ctx) error {
		a := actor(c)
		if a.ClientType == "web" {
			if e := api.origin(c); e != nil {
				return e
			}
		}
		if e := api.Identity.Revoke(c.Context(), a, a.SessionID, rid(c)); e != nil {
			return e
		}
		api.clearCookie(c)
		return c.SendStatus(204)
	})
	auth.Get("/sessions", api.authenticate, func(c fiber.Ctx) error {
		v, e := api.Identity.Sessions(c.Context(), actor(c).ID)
		if e != nil {
			return e
		}
		return c.JSON(fiber.Map{"items": v})
	})
	auth.Post("/sessions/:id/revoke", api.authenticate, func(c fiber.Ctx) error {
		if e := validID(c.Params("id")); e != nil {
			return e
		}
		if e := api.Identity.Revoke(c.Context(), actor(c), c.Params("id"), rid(c)); e != nil {
			return e
		}
		return c.SendStatus(204)
	})
	auth.Post("/password", api.authenticate, func(c fiber.Ctx) error {
		var in struct {
			CurrentPassword string `json:"current_password" validate:"required,max=256"`
			NewPassword     string `json:"new_password" validate:"required,min=12,max=256"`
		}
		if e := bind(c, &in); e != nil {
			return e
		}
		if e := api.Identity.ChangePassword(c.Context(), actor(c), in.CurrentPassword, in.NewPassword, rid(c)); e != nil {
			return e
		}
		api.clearCookie(c)
		return c.SendStatus(204)
	})
	auth.Post("/mfa/verify", func(c fiber.Ctx) error {
		var in identity.MFAVerify
		if e := bind(c, &in); e != nil {
			return e
		}
		if e := api.client(c, in.ClientType); e != nil {
			return e
		}
		t, e := api.Identity.VerifyMFA(c.Context(), in, c.IP(), c.Get("User-Agent"), rid(c))
		if e != nil {
			return e
		}
		return api.tokens(c, t, in.ClientType)
	})
	auth.Post("/mfa/enroll", api.authenticate, func(c fiber.Ctx) error {
		var in struct {
			CurrentPassword string `json:"current_password" validate:"required,max=256"`
		}
		if e := bind(c, &in); e != nil {
			return e
		}
		v, e := api.Identity.StartEnrollment(c.Context(), actor(c), in.CurrentPassword, rid(c))
		if e != nil {
			return e
		}
		return c.JSON(v)
	})
	auth.Post("/mfa/confirm", api.authenticate, func(c fiber.Ctx) error {
		var in struct {
			Code string `json:"code" validate:"required,len=6,numeric"`
		}
		if e := bind(c, &in); e != nil {
			return e
		}
		codes, e := api.Identity.ConfirmEnrollment(c.Context(), actor(c), in.Code, rid(c))
		if e != nil {
			return e
		}
		return c.JSON(fiber.Map{"recovery_codes": codes})
	})
	auth.Post("/mfa/disable", api.authenticate, func(c fiber.Ctx) error {
		var in struct {
			CurrentPassword string `json:"current_password" validate:"required,max=256"`
			Code            string `json:"code" validate:"omitempty,len=6,numeric"`
			RecoveryCode    string `json:"recovery_code" validate:"omitempty,max=32"`
		}
		if e := bind(c, &in); e != nil {
			return e
		}
		if e := api.Identity.DisableMFA(c.Context(), actor(c), in.CurrentPassword, in.Code, in.RecoveryCode, rid(c)); e != nil {
			return e
		}
		api.clearCookie(c)
		return c.SendStatus(204)
	})
	auth.Post("/mfa/recovery-codes/regenerate", api.authenticate, func(c fiber.Ctx) error {
		var in struct {
			CurrentPassword string `json:"current_password" validate:"required,max=256"`
			Code            string `json:"code" validate:"omitempty,len=6,numeric"`
			RecoveryCode    string `json:"recovery_code" validate:"omitempty,max=32"`
		}
		if e := bind(c, &in); e != nil {
			return e
		}
		codes, e := api.Identity.RegenerateRecoveryCodes(c.Context(), actor(c), in.CurrentPassword, in.Code, in.RecoveryCode, rid(c))
		if e != nil {
			return e
		}
		return c.JSON(fiber.Map{"recovery_codes": codes})
	})
	auth.Post("/password-reset/request", func(c fiber.Ctx) error {
		var in struct {
			Email string `json:"email" validate:"required,email,max=254"`
		}
		if e := bind(c, &in); e != nil {
			return e
		}
		tok, e := api.Identity.RequestPasswordReset(c.Context(), in.Email, c.IP(), c.Get("User-Agent"))
		if e != nil {
			return e
		}
		resp := fiber.Map{"status": "accepted"}
		if api.Config.Env != "production" && tok != "" {
			resp["dev_reset_token"] = tok
		}
		return c.Status(202).JSON(resp)
	})
	auth.Post("/password-reset/confirm", func(c fiber.Ctx) error {
		var in struct {
			Token       string `json:"token" validate:"required,min=32,max=128"`
			NewPassword string `json:"new_password" validate:"required,min=12,max=256"`
		}
		if e := bind(c, &in); e != nil {
			return e
		}
		if e := api.Identity.ConfirmPasswordReset(c.Context(), in.Token, in.NewPassword, c.IP(), c.Get("User-Agent")); e != nil {
			return e
		}
		api.clearCookie(c)
		return c.SendStatus(204)
	})
	auth.Get("/devices", api.authenticate, func(c fiber.Ctx) error {
		v, e := api.Identity.ListDevices(c.Context(), actor(c), rid(c))
		if e != nil {
			return e
		}
		return c.JSON(fiber.Map{"items": v})
	})
	auth.Post("/devices", api.authenticate, func(c fiber.Ctx) error {
		var in identity.DeviceInput
		if e := bind(c, &in); e != nil {
			return e
		}
		v, e := api.Identity.RegisterDevice(c.Context(), actor(c), in, api.Config.IdentifierIndexKey, rid(c))
		if e != nil {
			return e
		}
		return c.Status(201).JSON(v)
	})
	auth.Post("/devices/:id/revoke", api.authenticate, func(c fiber.Ctx) error {
		if e := validID(c.Params("id")); e != nil {
			return e
		}
		v, e := api.Identity.RevokeDevice(c.Context(), actor(c), c.Params("id"), rid(c))
		if e != nil {
			return e
		}
		return c.JSON(v)
	})
	staff := app.Group("/api/v1/staff", api.authenticate)
	staff.Get("/facilities", api.require(""), func(c fiber.Ctx) error {
		if e := oneOf(c, "status", "active", "inactive"); e != nil {
			return e
		}
		v, e := api.Facilities.List(c.Context(), actor(c), c.Query("q"), c.Query("status"), rid(c))
		if e != nil {
			return e
		}
		return c.JSON(fiber.Map{"items": v})
	})
	staff.Post("/facilities", api.require("admin.facility.manage"), func(c fiber.Ctx) error {
		var in facilities.Input
		if e := bind(c, &in); e != nil {
			return e
		}
		f, e := api.Facilities.Create(c.Context(), actor(c), in, rid(c))
		if e != nil {
			return e
		}
		return c.Status(201).JSON(f)
	})
	staff.Get("/facilities/:id", api.require(""), func(c fiber.Ctx) error {
		if e := validID(c.Params("id")); e != nil {
			return e
		}
		f, e := api.Facilities.Get(c.Context(), actor(c), c.Params("id"), rid(c))
		if e != nil {
			return e
		}
		return c.JSON(f)
	})
	staff.Put("/facilities/:id", api.require("admin.facility.manage"), func(c fiber.Ctx) error {
		if e := validID(c.Params("id")); e != nil {
			return e
		}
		var in facilities.UpdateInput
		if e := bind(c, &in); e != nil {
			return e
		}
		f, e := api.Facilities.Update(c.Context(), actor(c), c.Params("id"), in, rid(c))
		if e != nil {
			return e
		}
		return c.JSON(f)
	})
	staff.Post("/facilities/:id/status", api.require("admin.facility.manage"), func(c fiber.Ctx) error {
		if e := validID(c.Params("id")); e != nil {
			return e
		}
		var in facilities.StatusInput
		if e := bind(c, &in); e != nil {
			return e
		}
		f, e := api.Facilities.SetStatus(c.Context(), actor(c), c.Params("id"), in, rid(c))
		if e != nil {
			return e
		}
		return c.JSON(f)
	})
	staff.Get("/facilities/:id/departments", api.require(""), func(c fiber.Ctx) error {
		if e := validID(c.Params("id")); e != nil {
			return e
		}
		v, e := api.Facilities.Departments(c.Context(), actor(c), c.Params("id"), rid(c))
		if e != nil {
			return e
		}
		return c.JSON(fiber.Map{"items": v})
	})
	staff.Get("/users", api.require("admin.user.manage"), func(c fiber.Ctx) error {
		after, n, e := page(c)
		if e != nil {
			return e
		}
		if e = optUUID(c, "facility_id"); e != nil {
			return e
		}
		if e = oneOf(c, "status", "pending_activation", "active", "suspended", "deactivated"); e != nil {
			return e
		}
		v, e := api.Identity.List(c.Context(), actor(c), identity.StaffFilter{
			Q:          c.Query("q"),
			Role:       c.Query("role"),
			Status:     c.Query("status"),
			FacilityID: c.Query("facility_id"),
			After:      after,
			Limit:      n,
		}, rid(c))
		if e != nil {
			return e
		}
		next := ""
		if len(v) == n {
			next = v[len(v)-1].ID
		}
		return c.JSON(fiber.Map{"items": v, "next_cursor": next})
	})
	staff.Get("/users/:id", api.require("admin.user.manage"), func(c fiber.Ctx) error {
		if e := validID(c.Params("id")); e != nil {
			return e
		}
		u, e := api.Identity.GetStaff(c.Context(), actor(c), c.Params("id"), rid(c))
		if e != nil {
			return e
		}
		return c.JSON(u)
	})
	staff.Post("/users", api.require("admin.user.manage"), func(c fiber.Ctx) error {
		var in identity.StaffInput
		if e := bind(c, &in); e != nil {
			return e
		}
		u, e := api.Identity.CreateStaff(c.Context(), actor(c), in, rid(c))
		if e != nil {
			return e
		}
		return c.Status(201).JSON(u)
	})
	staff.Put("/users/:id", api.require("admin.user.manage"), func(c fiber.Ctx) error {
		if e := validID(c.Params("id")); e != nil {
			return e
		}
		var in identity.UpdateStaff
		if e := bind(c, &in); e != nil {
			return e
		}
		u, e := api.Identity.UpdateStaff(c.Context(), actor(c), c.Params("id"), in, rid(c))
		if e != nil {
			return e
		}
		return c.JSON(u)
	})
	staff.Post("/users/:id/status", api.require("admin.user.manage"), func(c fiber.Ctx) error {
		if e := validID(c.Params("id")); e != nil {
			return e
		}
		var in identity.StatusChangeInput
		if e := bind(c, &in); e != nil {
			return e
		}
		u, e := api.Identity.ChangeStatus(c.Context(), actor(c), c.Params("id"), in, rid(c))
		if e != nil {
			return e
		}
		return c.JSON(u)
	})
	staff.Post("/patients", api.require("patient.create"), func(c fiber.Ctx) error {
		var in patients.Input
		if e := bind(c, &in); e != nil {
			return e
		}
		p, e := api.Patients.Create(c.Context(), actor(c), in, c.Get("Idempotency-Key"), rid(c))
		if e != nil {
			return e
		}
		return c.Status(201).JSON(p)
	})
	staff.Get("/patients", api.require("patient.read"), func(c fiber.Ctx) error {
		after, n, e := page(c)
		if e != nil {
			return e
		}
		if e = optUUID(c, "facility_id"); e != nil {
			return e
		}
		if e = optDate(c, "birth_date"); e != nil {
			return e
		}
		if e = oneOf(c, "status", "active", "inactive", "deceased", "merged"); e != nil {
			return e
		}
		if e = oneOf(c, "sort", "id", "name", "newest"); e != nil {
			return e
		}
		v, e := api.Patients.List(c.Context(), actor(c), patients.ListFilter{
			Q:          c.Query("q"),
			Phone:      c.Query("phone"),
			BirthDate:  c.Query("birth_date"),
			FacilityID: c.Query("facility_id"),
			Status:     c.Query("status"),
			Sort:       c.Query("sort"),
			After:      after,
			Limit:      n,
		}, rid(c))
		if e != nil {
			return e
		}
		next := ""
		if len(v) == n {
			next = v[len(v)-1].ID
		}
		return c.JSON(fiber.Map{"items": v, "next_cursor": next})
	})
	api.mountPatientStatic(staff)
	staff.Get("/patients/:id", api.require("patient.read"), func(c fiber.Ctx) error {
		if e := validID(c.Params("id")); e != nil {
			return e
		}
		p, e := api.Patients.Get(c.Context(), actor(c), c.Params("id"), rid(c))
		if e != nil {
			return e
		}
		return c.JSON(p)
	})
	staff.Put("/patients/:id", api.require("patient.update"), func(c fiber.Ctx) error {
		if e := validID(c.Params("id")); e != nil {
			return e
		}
		var in patients.Update
		if e := bind(c, &in); e != nil {
			return e
		}
		p, e := api.Patients.Update(c.Context(), actor(c), c.Params("id"), in, rid(c))
		if e != nil {
			return e
		}
		return c.JSON(p)
	})
	staff.Post("/patients/:id/status", api.require("patient.update"), func(c fiber.Ctx) error {
		if e := validID(c.Params("id")); e != nil {
			return e
		}
		var in patients.StatusInput
		if e := bind(c, &in); e != nil {
			return e
		}
		p, e := api.Patients.ChangeStatus(c.Context(), actor(c), c.Params("id"), in, rid(c))
		if e != nil {
			return e
		}
		return c.JSON(p)
	})
	staff.Post("/patients/:id/verifications", api.require(""), func(c fiber.Ctx) error {
		if e := validID(c.Params("id")); e != nil {
			return e
		}
		var in patients.VerificationInput
		if e := bind(c, &in); e != nil {
			return e
		}
		v, e := api.Patients.Verify(c.Context(), actor(c), c.Params("id"), in, c.Get("Idempotency-Key"), rid(c))
		if e != nil {
			return e
		}
		return c.Status(201).JSON(v)
	})
	staff.Get("/patients/:id/verifications", api.require("patient.read"), func(c fiber.Ctx) error {
		if e := validID(c.Params("id")); e != nil {
			return e
		}
		v, e := api.Patients.Verifications(c.Context(), actor(c), c.Params("id"), rid(c))
		if e != nil {
			return e
		}
		return c.JSON(fiber.Map{"items": v})
	})
	staff.Get("/services", api.require(""), func(c fiber.Ctx) error {
		v, e := api.Visits.Catalog(c.Context(), actor(c), c.Query("category"), rid(c))
		if e != nil {
			return e
		}
		return c.JSON(fiber.Map{"items": v})
	})
	staff.Post("/encounters", api.require("encounter.manage"), func(c fiber.Ctx) error {
		var in visits.EncounterInput
		if e := bind(c, &in); e != nil {
			return e
		}
		v, e := api.Visits.CreateEncounter(c.Context(), actor(c), in, c.Get("Idempotency-Key"), rid(c))
		if e != nil {
			return e
		}
		return c.Status(201).JSON(v)
	})
	staff.Get("/encounters", api.require(""), func(c fiber.Ctx) error {
		after, n, e := page(c)
		if e != nil {
			return e
		}
		if p := c.Query("patient_id"); p != "" {
			if e := validID(p); e != nil {
				return e
			}
		}
		st := c.Query("status")
		if st != "" && st != "registered" && st != "in_progress" && st != "completed" && st != "cancelled" && st != "entered_in_error" {
			return platform.Fail(422, "validation", "Invalid status")
		}
		v, e := api.Visits.ListEncounters(c.Context(), actor(c), c.Query("patient_id"), st, after, rid(c), n)
		if e != nil {
			return e
		}
		next := ""
		if len(v) == n {
			next = v[len(v)-1].ID
		}
		return c.JSON(fiber.Map{"items": v, "next_cursor": next})
	})
	staff.Get("/encounters/:id", api.require(""), func(c fiber.Ctx) error {
		if e := validID(c.Params("id")); e != nil {
			return e
		}
		v, e := api.Visits.GetEncounter(c.Context(), actor(c), c.Params("id"), rid(c))
		if e != nil {
			return e
		}
		return c.JSON(v)
	})
	staff.Post("/encounters/:id/status", api.require("encounter.manage"), func(c fiber.Ctx) error {
		if e := validID(c.Params("id")); e != nil {
			return e
		}
		var in visits.StatusInput
		if e := bind(c, &in); e != nil {
			return e
		}
		v, e := api.Visits.ChangeStatus(c.Context(), actor(c), c.Params("id"), in, rid(c))
		if e != nil {
			return e
		}
		return c.JSON(v)
	})
	staff.Post("/encounters/:id/nursing", api.require("nursing.write"), func(c fiber.Ctx) error {
		if e := validID(c.Params("id")); e != nil {
			return e
		}
		var in visits.NursingInput
		if e := bind(c, &in); e != nil {
			return e
		}
		v, e := api.Visits.RecordNursing(c.Context(), actor(c), c.Params("id"), in, c.Get("Idempotency-Key"), rid(c))
		if e != nil {
			return e
		}
		return c.Status(201).JSON(v)
	})
	staff.Get("/encounters/:id/nursing", api.require(""), func(c fiber.Ctx) error {
		if e := validID(c.Params("id")); e != nil {
			return e
		}
		v, e := api.Visits.ListNursing(c.Context(), actor(c), c.Params("id"), rid(c))
		if e != nil {
			return e
		}
		return c.JSON(fiber.Map{"items": v})
	})
	staff.Post("/nursing/:id/void", api.require("nursing.write"), func(c fiber.Ctx) error {
		if e := validID(c.Params("id")); e != nil {
			return e
		}
		var in visits.ErrorInput
		if e := bind(c, &in); e != nil {
			return e
		}
		v, e := api.Visits.VoidNursing(c.Context(), actor(c), c.Params("id"), in, rid(c))
		if e != nil {
			return e
		}
		return c.JSON(v)
	})
	staff.Post("/encounters/:id/procedures", api.require(""), func(c fiber.Ctx) error {
		if e := validID(c.Params("id")); e != nil {
			return e
		}
		var in visits.ProcedureInput
		if e := bind(c, &in); e != nil {
			return e
		}
		v, e := api.Visits.RecordProcedure(c.Context(), actor(c), c.Params("id"), in, c.Get("Idempotency-Key"), rid(c))
		if e != nil {
			return e
		}
		return c.Status(201).JSON(v)
	})
	staff.Get("/encounters/:id/procedures", api.require(""), func(c fiber.Ctx) error {
		if e := validID(c.Params("id")); e != nil {
			return e
		}
		v, e := api.Visits.ListProcedures(c.Context(), actor(c), c.Params("id"), rid(c))
		if e != nil {
			return e
		}
		return c.JSON(fiber.Map{"items": v})
	})
	staff.Post("/procedures/:id/action", api.require(""), func(c fiber.Ctx) error {
		if e := validID(c.Params("id")); e != nil {
			return e
		}
		var in visits.ProcedureAction
		if e := bind(c, &in); e != nil {
			return e
		}
		v, e := api.Visits.ActOnProcedure(c.Context(), actor(c), c.Params("id"), in, rid(c))
		if e != nil {
			return e
		}
		return c.JSON(v)
	})
	staff.Get("/reports/staff-activity", api.require("report.staff_activity"), func(c fiber.Ctx) error {
		to := time.Now().In(time.UTC)
		from := to.AddDate(0, 0, -6)
		var e error
		if v := c.Query("from"); v != "" {
			if from, e = time.Parse("2006-01-02", v); e != nil {
				return platform.Fail(422, "validation", "from must be YYYY-MM-DD")
			}
		}
		if v := c.Query("to"); v != "" {
			if to, e = time.Parse("2006-01-02", v); e != nil {
				return platform.Fail(422, "validation", "to must be YYYY-MM-DD")
			}
		}
		if to.Before(from) || to.Sub(from) > 366*24*time.Hour {
			return platform.Fail(422, "validation", "Date range must be 0-366 days")
		}
		v, e := api.Reports.StaffActivity(c.Context(), actor(c), from, to, rid(c))
		if e != nil {
			return e
		}
		return c.JSON(fiber.Map{"items": v, "from": from.Format("2006-01-02"), "to": to.Format("2006-01-02")})
	})
	staff.Get("/reports/staff-access", api.require("report.staff_activity"), func(c fiber.Ctx) error {
		v, e := api.Reports.StaffAccess(c.Context(), actor(c), rid(c))
		if e != nil {
			return e
		}
		return c.JSON(fiber.Map{"items": v})
	})
	api.mountExt(staff)
	api.mountComplete(staff)
	staff.Get("/audit/export", api.require("audit.read"), api.auditExport)
	staff.Get("/audit", api.require("audit.read"), api.audit)
	staff.Get("/metrics", api.require("report.view"), func(c fiber.Ctx) error {
		c.Set("Content-Type", "text/plain; version=0.0.4")
		return c.SendString(metrics.Prometheus())
	})
	return app, nil
}
func strictJSON(data []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	if e := d.Decode(new(any)); e != io.EOF {
		return errors.New("exactly one JSON value required")
	}
	return nil
}
func bind(c fiber.Ctx, out any) error {
	media, _, err := mime.ParseMediaType(c.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return platform.Fail(415, "unsupported_media_type", "Use application/json")
	}
	if e := c.Bind().JSON(out); e != nil {
		var ve validator.ValidationErrors
		if errors.As(e, &ve) {
			return e
		}
		return platform.Fail(400, "invalid_json", "Invalid JSON or unknown request field")
	}
	return nil
}
func rid(c fiber.Ctx) string           { return c.Locals("request_id").(string) }
func actor(c fiber.Ctx) identity.Actor { return c.Locals("actor").(identity.Actor) }
func validID(id string) error {
	if _, e := uuid.Parse(id); e != nil {
		return platform.Fail(422, "validation", "Invalid UUID")
	}
	return nil
}
func page(c fiber.Ctx) (string, int, error) {
	after := c.Query("cursor")
	if after != "" {
		if e := validID(after); e != nil {
			return "", 0, e
		}
	}
	n := 25
	if v := c.Query("limit"); v != "" {
		var e error
		n, e = strconv.Atoi(v)
		if e != nil || n < 1 || n > 100 {
			return "", 0, platform.Fail(422, "validation", "limit must be 1–100")
		}
	}
	return after, n, nil
}
func (api API) authenticate(c fiber.Ctx) error {
	h := c.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") || len(h) > 150 {
		return platform.Unauthorized()
	}
	a, e := api.Identity.Actor(c.Context(), strings.TrimPrefix(h, "Bearer "), rid(c))
	if e != nil {
		if errors.Is(e, context.DeadlineExceeded) {
			return e
		}
		e = platform.Normalize(e)
		var p *platform.Error
		if errors.As(e, &p) && p.Status == 404 {
			return platform.Unauthorized()
		}
		return e
	}
	c.Locals("actor", a)
	return c.Next()
}

// require checks a permission derived by the database for this session; an empty name
// only requires authentication. RLS enforces the same permissions again in SQL.
func (api API) require(permission string) fiber.Handler {
	return func(c fiber.Ctx) error {
		a := actor(c)
		if !a.MFAVerified && api.needsMFA(c, a) {
			api.deny(c, a, "mfa_required")
			return platform.Fail(403, "mfa_required", "This role requires a session verified with a second factor; enroll at /api/v1/auth/mfa/enroll or sign in again")
		}
		if permission != "" && !a.Can(permission) {
			api.deny(c, a, permission)
			return platform.Forbidden()
		}
		return c.Next()
	}
}

func (api API) needsMFA(c fiber.Ctx, a identity.Actor) bool {
	if api.Config.MFAEnforcePrivileged {
		switch a.Role {
		case "administrator", "compliance_officer", "billing_supervisor", "lab_supervisor":
			return true
		}
	}
	if api.Config.MFAEnforceRemoteClinical && !api.Config.IsClinicIP(c.IP()) {
		switch a.Role {
		case "doctor", "nurse":
			return true
		}
	}
	return false
}
func (api API) deny(c fiber.Ctx, a identity.Actor, permission string) {
	tx, _, e := platform.BeginRequest(c.Context(), api.DB, a.SessionID, rid(c))
	if e != nil {
		return
	}
	defer tx.Rollback(c.Context())
	if platform.Audit(c.Context(), tx, "access.denied", "authorization", "denied", "", "", "", permission) == nil {
		tx.Commit(c.Context())
	}
}
func (api API) origin(c fiber.Ctx) error {
	for _, o := range api.Config.Origins {
		if c.Get("Origin") == o {
			return nil
		}
	}
	return platform.Fail(403, "origin_denied", "A configured Origin is required for browser authentication")
}
func (api API) client(c fiber.Ctx, client string) error {
	if client == "web" {
		return api.origin(c)
	}
	if c.Get("Origin") != "" {
		return platform.Fail(403, "origin_denied", "Mobile authentication does not accept browser origins")
	}
	return nil
}
func (api API) login(c fiber.Ctx) error {
	var in identity.Credentials
	if e := bind(c, &in); e != nil {
		return e
	}
	if e := api.client(c, in.ClientType); e != nil {
		return e
	}
	t, challenge, e := api.Identity.Login(c.Context(), in, c.IP(), c.Get("User-Agent"), rid(c))
	if e != nil {
		return e
	}
	if challenge != nil {
		return c.JSON(challenge)
	}
	return api.tokens(c, t, in.ClientType)
}
func (api API) refresh(c fiber.Ctx) error {
	var in struct {
		ClientType   string `json:"client_type" validate:"required,oneof=web mobile"`
		RefreshToken string `json:"refresh_token" validate:"max=128"`
	}
	if e := bind(c, &in); e != nil {
		return e
	}
	if e := api.client(c, in.ClientType); e != nil {
		return e
	}
	if in.ClientType == "web" {
		if in.RefreshToken != "" {
			return platform.Fail(422, "validation", "Web refresh uses the HttpOnly cookie")
		}
		in.RefreshToken = c.Cookies("spartial_refresh")
	}
	t, e := api.Identity.Refresh(c.Context(), in.RefreshToken, in.ClientType, rid(c))
	if e != nil {
		if in.ClientType == "web" {
			api.clearCookie(c)
		}
		return e
	}
	return api.tokens(c, t, in.ClientType)
}
func (api API) tokens(c fiber.Ctx, t identity.Tokens, client string) error {
	if client == "web" {
		c.Cookie(&fiber.Cookie{Name: "spartial_refresh", Value: t.RefreshToken, HTTPOnly: true, Secure: api.Config.CookieSecure, SameSite: "Strict", Path: "/api/v1/auth", MaxAge: 7 * 24 * 60 * 60})
		return c.JSON(t)
	}
	return c.JSON(struct {
		identity.Tokens
		RefreshToken string `json:"refresh_token"`
	}{t, t.RefreshToken})
}
func (api API) clearCookie(c fiber.Ctx) {
	c.Cookie(&fiber.Cookie{Name: "spartial_refresh", Value: "", HTTPOnly: true, Secure: api.Config.CookieSecure, SameSite: "Strict", Path: "/api/v1/auth", MaxAge: -1, Expires: time.Unix(1, 0)})
}
func (api API) handleError(c fiber.Ctx, err error) error {
	err = platform.Normalize(err)
	status, code, message := 500, "internal_error", "Internal server error"
	var p *platform.Error
	var f *fiber.Error
	var ve validator.ValidationErrors
	fields := map[string]string{}
	if errors.As(err, &p) {
		status, code, message = p.Status, p.Code, p.Message
	} else if errors.As(err, &ve) {
		status, code, message = 422, "validation", "Request validation failed"
		for _, v := range ve {
			fields[v.Field()] = v.Tag()
		}
	} else if errors.As(err, &f) {
		status, code, message = f.Code, "http_error", "Request could not be processed"
	}
	if status >= 500 {
		if api.Config.Env == "production" {
			api.Log.Error("request_failed", "request_id", c.Locals("request_id"), "code", code)
		} else {
			api.Log.Error("request_failed", "request_id", c.Locals("request_id"), "code", code, "cause", err.Error())
		}
	}
	return c.Status(status).JSON(fiber.Map{"error": fiber.Map{"code": code, "message": message, "fields": fields}, "request_id": c.Locals("request_id")})
}
func parseAuditFilter(c fiber.Ctx, defaultLimit, maxLimit int) (audit.Filter, error) {
	f := audit.Filter{
		ActorID:   c.Query("actor_id"),
		PatientID: c.Query("patient_id"),
		Action:    c.Query("action"),
		Outcome:   c.Query("outcome"),
		Before:    9223372036854775807,
		Limit:     defaultLimit,
	}
	if e := optUUID(c, "actor_id", "patient_id"); e != nil {
		return f, e
	}
	if e := oneOf(c, "outcome", "success", "denied", "failure", "error"); e != nil {
		return f, e
	}
	if before := c.Query("before"); before != "" {
		n, e := strconv.ParseInt(before, 10, 64)
		if e != nil || n < 1 {
			return f, platform.Fail(422, "validation", "Invalid before cursor")
		}
		f.Before = n
	}
	if raw := c.Query("limit"); raw != "" {
		n, e := strconv.Atoi(raw)
		if e != nil || n < 1 || n > maxLimit {
			return f, platform.Fail(422, "validation", "Invalid limit")
		}
		f.Limit = n
	}
	if raw := c.Query("from"); raw != "" {
		t, e := time.Parse(time.RFC3339, raw)
		if e != nil {
			if d, e2 := time.Parse("2006-01-02", raw); e2 == nil {
				t = d
			} else {
				return f, platform.Fail(422, "validation", "from must be RFC3339 or YYYY-MM-DD")
			}
		}
		f.From = &t
	}
	if raw := c.Query("to"); raw != "" {
		t, e := time.Parse(time.RFC3339, raw)
		if e != nil {
			if d, e2 := time.Parse("2006-01-02", raw); e2 == nil {
				t = d.Add(24*time.Hour - time.Nanosecond)
			} else {
				return f, platform.Fail(422, "validation", "to must be RFC3339 or YYYY-MM-DD")
			}
		}
		f.To = &t
	}
	return f, nil
}
func (api API) audit(c fiber.Ctx) error {
	f, e := parseAuditFilter(c, 50, 100)
	if e != nil {
		return e
	}
	out, e := (audit.Service{DB: api.DB}).List(c.Context(), actor(c), f, rid(c))
	if e != nil {
		return e
	}
	return c.JSON(fiber.Map{"items": out})
}
func (api API) auditExport(c fiber.Ctx) error {
	f, e := parseAuditFilter(c, 500, 1000)
	if e != nil {
		return e
	}
	out, e := (audit.Service{DB: api.DB}).Export(c.Context(), actor(c), f, rid(c))
	if e != nil {
		return e
	}
	return c.JSON(fiber.Map{"items": out, "count": len(out), "exported_at": time.Now().UTC().Format(time.RFC3339)})
}
