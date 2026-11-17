package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
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
	"spartial.local/backend/internal/modules/facilities"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/modules/patients"
	"spartial.local/backend/internal/modules/privacy"
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
	api := API{db, identity.Service{Repo: identity.Repository{DB: db}, DummyHash: dummy, Keys: cfg.Keys, Issuer: "Spartial Polyclinic"}, patients.Service{DB: db, Location: location}, facilities.Service{DB: db}, cfg, log}
	app := fiber.New(fiber.Config{AppName: "Spartial API", BodyLimit: 64 * 1024, Immutable: true, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, StructValidator: structValidator{validator.New()}, JSONDecoder: strictJSON, ErrorHandler: api.handleError})
	app.Use(func(c fiber.Ctx) error {
		id := uuid.NewString()
		c.Locals("request_id", id)
		c.Set("X-Request-ID", id)
		c.Set("Cache-Control", "no-store")
		c.Set("X-Content-Type-Options", "nosniff")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		c.SetContext(ctx)
		start := time.Now()
		err := c.Next()
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
		log.Info("http_request", "request_id", id, "method", c.Method(), "route", c.Route().Path, "status", status, "duration_ms", time.Since(start).Milliseconds())
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
		v, e := api.Identity.Repo.Sessions(c.Context(), actor(c).ID)
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
		t, e := api.Identity.VerifyMFA(c.Context(), in, c.IP(), rid(c))
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
		// Recovery codes are shown exactly once; only their digests are stored.
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
	staff := app.Group("/api/v1/staff", api.authenticate)
	staff.Get("/facilities", api.require("facilities.read"), func(c fiber.Ctx) error {
		v, e := api.Facilities.List(c.Context(), actor(c))
		if e != nil {
			return e
		}
		return c.JSON(fiber.Map{"items": v})
	})
	staff.Post("/facilities", api.require("facilities.manage"), func(c fiber.Ctx) error {
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
	staff.Get("/users", api.require("staff.read"), func(c fiber.Ctx) error {
		after, n, e := page(c)
		if e != nil {
			return e
		}
		v, e := api.Identity.Repo.List(c.Context(), after, n)
		if e != nil {
			return e
		}
		next := ""
		if len(v) == n {
			next = v[len(v)-1].ID
		}
		return c.JSON(fiber.Map{"items": v, "next_cursor": next})
	})
	staff.Post("/users", api.require("staff.manage"), func(c fiber.Ctx) error {
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
	staff.Put("/users/:id", api.require("staff.manage"), func(c fiber.Ctx) error {
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
	staff.Post("/patients", api.require("patients.write"), func(c fiber.Ctx) error {
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
	staff.Get("/patients", api.require("patients.read"), func(c fiber.Ctx) error {
		after, n, e := page(c)
		if e != nil {
			return e
		}
		v, e := api.Patients.List(c.Context(), actor(c), c.Query("q"), after, rid(c), n)
		if e != nil {
			return e
		}
		next := ""
		if len(v) == n {
			next = v[len(v)-1].ID
		}
		return c.JSON(fiber.Map{"items": v, "next_cursor": next})
	})
	staff.Get("/patients/:id", api.require("patients.read"), func(c fiber.Ctx) error {
		if e := validID(c.Params("id")); e != nil {
			return e
		}
		p, e := api.Patients.Get(c.Context(), actor(c), c.Params("id"), rid(c))
		if e != nil {
			return e
		}
		return c.JSON(p)
	})
	staff.Put("/patients/:id", api.require("patients.write"), func(c fiber.Ctx) error {
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
	staff.Get("/audit", api.require("audit.read"), api.audit)
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
	a, e := api.Identity.Repo.Actor(c.Context(), strings.TrimPrefix(h, "Bearer "))
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
func (api API) require(permission string) fiber.Handler {
	return func(c fiber.Ctx) error {
		a := actor(c)
		if !a.Can(permission) {
			if e := api.deny(c, a, ""); e != nil {
				return e
			}
			return platform.Forbidden()
		}
		// Enforce assurance on staff permission routes; authentication/MFA bootstrap stays available.
		if api.Config.MFAEnforcePrivileged && a.Privileged && !a.MFAVerified {
			if e := api.deny(c, a, "mfa_required"); e != nil {
				return e
			}
			return platform.Fail(403, "mfa_required", "This role requires a session verified with a second factor; enroll at /api/v1/auth/mfa/enroll or sign in again")
		}
		return c.Next()
	}
}
func (api API) deny(c fiber.Ctx, a identity.Actor, reason string) error {
	tx, e := api.DB.Begin(c.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(c.Context())
	if e = platform.AuditReason(c.Context(), tx, a.ID, "access.denied", "", "", rid(c), "denied", reason); e != nil {
		return e
	}
	return tx.Commit(c.Context())
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
	t, challenge, e := api.Identity.Login(c.Context(), in, c.IP(), rid(c))
	if e != nil {
		return e
	}
	if challenge != nil {
		// No session exists yet; the client must POST /auth/mfa/verify with this challenge.
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
		api.Log.Error("request_failed", "request_id", c.Locals("request_id"), "code", code)
	}
	return c.Status(status).JSON(fiber.Map{"error": fiber.Map{"code": code, "message": message, "fields": fields}, "request_id": c.Locals("request_id")})
}
func (api API) audit(c fiber.Ctx) error {
	// Globally privileged privacy-review permission; no clinical payloads are returned.
	before := c.Query("before")
	var cursor int64 = 9223372036854775807
	if before != "" {
		n, e := strconv.ParseInt(before, 10, 64)
		if e != nil || n < 1 {
			return platform.Fail(422, "validation", "Invalid before cursor")
		}
		cursor = n
	}
	out, e := (privacy.Service{DB: api.DB}).List(c.Context(), actor(c), cursor, rid(c))
	if e != nil {
		return e
	}
	return c.JSON(fiber.Map{"items": out})
}
