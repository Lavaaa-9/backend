package httpapi

import (
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"spartial.local/backend/internal/modules/admin"
	"spartial.local/backend/internal/modules/approvals"
	"spartial.local/backend/internal/modules/billing"
	"spartial.local/backend/internal/modules/clinical"
	"spartial.local/backend/internal/modules/comms"
	"spartial.local/backend/internal/modules/documents"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/modules/patients"
	"spartial.local/backend/internal/modules/privacy"
	"spartial.local/backend/internal/modules/telehealth"
	"spartial.local/backend/internal/platform"
)

func optDate(c fiber.Ctx, name string) error {
	v := c.Query(name)
	if v == "" {
		return nil
	}
	if _, e := time.Parse("2006-01-02", v); e != nil {
		return platform.Fail(422, "validation", name+" must be YYYY-MM-DD")
	}
	return nil
}

// mountPatientStatic registers static /patients/... routes before /patients/:id.
func (api API) mountPatientStatic(staff fiber.Router) {
	pt := api.Patients
	staff.Post("/patients/duplicates/check", api.require("patient.read"), withBody(200, func(c fiber.Ctx, a identity.Actor, _ string, b patients.DuplicateCheckInput) (any, error) {
		return items(pt.CheckDuplicates(c.Context(), a, b, rid(c)))
	}))
	staff.Get("/patients/duplicates", api.require("patient.read"), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		if e := oneOf(c, "status", "open", "confirmed_distinct", "confirmed_duplicate", "merged"); e != nil {
			return nil, e
		}
		return items(pt.ListDuplicateReviews(c.Context(), a, c.Query("status"), rid(c)))
	}))
	staff.Post("/patients/duplicates", api.require(""), withBody(201, func(c fiber.Ctx, a identity.Actor, _ string, b patients.DuplicateReviewInput) (any, error) {
		return pt.CreateDuplicateReview(c.Context(), a, b, rid(c))
	}))
	staff.Post("/patients/duplicates/:id/resolve", api.require(""), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b patients.DuplicateResolveInput) (any, error) {
		return pt.ResolveDuplicateReview(c.Context(), a, id, b, rid(c))
	}))
	staff.Post("/patients/merge", api.require("patient.merge"), withBody(200, func(c fiber.Ctx, a identity.Actor, _ string, b patients.MergeInput) (any, error) {
		return pt.MergePatients(c.Context(), a, b, rid(c))
	}))
}

// mountComplete registers the remaining backend modules: identifiers, contacts, next-of-kin,
// consents, clinical history, appointments, telehealth, documents, insurance coverages,
// break-glass, privacy, governance, access reviews, approvals, reports, and comms.
func (api API) mountComplete(staff fiber.Router) {
	pt := api.Patients
	cl := api.Clinical
	th := api.Telehealth
	dc := api.Documents
	bl := api.Billing
	pr := api.Privacy
	ap := api.Approvals
	rp := api.Reports
	cm := api.Comms

	// ---- patient identifiers
	staff.Post("/patients/:id/identifiers", api.require(""), withBody(201, func(c fiber.Ctx, a identity.Actor, id string, b patients.IdentifierInput) (any, error) {
		return pt.AddIdentifier(c.Context(), a, id, b, c.Get("Idempotency-Key"), rid(c))
	}))
	staff.Get("/patients/:id/identifiers", api.require("patient.read"), noBody(func(c fiber.Ctx, a identity.Actor, id string) (any, error) {
		return items(pt.ListIdentifiers(c.Context(), a, id, rid(c)))
	}))
	staff.Post("/patients/:id/identifiers/:identifier_id/reveal", api.require("patient.identifier.reveal"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b patients.RevealInput) (any, error) {
		if e := uuidParam(c, "identifier_id"); e != nil {
			return nil, e
		}
		return pt.RevealIdentifier(c.Context(), a, id, c.Params("identifier_id"), b, rid(c))
	}))
	staff.Post("/patients/:id/identifiers/:identifier_id/status", api.require("patient.update"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b patients.IdentifierStatusInput) (any, error) {
		if e := uuidParam(c, "identifier_id"); e != nil {
			return nil, e
		}
		return pt.SetIdentifierStatus(c.Context(), a, id, c.Params("identifier_id"), b, rid(c))
	}))
	staff.Post("/patient-identifiers/search", api.require("patient.read"), withBody(200, func(c fiber.Ctx, a identity.Actor, _ string, b patients.IdentifierInput) (any, error) {
		ids, e := pt.SearchIdentifier(c.Context(), a, b, rid(c))
		if e != nil {
			return nil, e
		}
		return reply{"patient_ids": ids}, nil
	}))

	// ---- patient contacts, next-of-kin, consents
	staff.Get("/patients/:id/contacts", api.require("patient.read"), noBody(func(c fiber.Ctx, a identity.Actor, id string) (any, error) {
		return items(pt.ListContacts(c.Context(), a, id, rid(c)))
	}))
	staff.Post("/patients/:id/contacts", api.require(""), withBody(201, func(c fiber.Ctx, a identity.Actor, id string, b patients.ContactInput) (any, error) {
		return pt.AddContact(c.Context(), a, id, b, rid(c))
	}))
	staff.Put("/patients/:id/contacts/:contact_id", api.require("patient.update"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b patients.ContactUpdate) (any, error) {
		if e := uuidParam(c, "contact_id"); e != nil {
			return nil, e
		}
		return pt.UpdateContact(c.Context(), a, id, c.Params("contact_id"), b, rid(c))
	}))
	staff.Get("/patients/:id/next-of-kin", api.require("patient.read"), noBody(func(c fiber.Ctx, a identity.Actor, id string) (any, error) {
		return items(pt.ListNextOfKin(c.Context(), a, id, rid(c)))
	}))
	staff.Post("/patients/:id/next-of-kin", api.require(""), withBody(201, func(c fiber.Ctx, a identity.Actor, id string, b patients.NextOfKinInput) (any, error) {
		return pt.AddNextOfKin(c.Context(), a, id, b, rid(c))
	}))
	staff.Put("/patients/:id/next-of-kin/:nok_id", api.require("patient.update"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b patients.NextOfKinUpdate) (any, error) {
		if e := uuidParam(c, "nok_id"); e != nil {
			return nil, e
		}
		return pt.UpdateNextOfKin(c.Context(), a, id, c.Params("nok_id"), b, rid(c))
	}))
	staff.Get("/patients/:id/consents", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, id string) (any, error) {
		return items(pt.ListConsents(c.Context(), a, id, rid(c)))
	}))
	staff.Post("/patients/:id/consents", api.require(""), withBody(201, func(c fiber.Ctx, a identity.Actor, id string, b patients.ConsentInput) (any, error) {
		return pt.GrantConsent(c.Context(), a, id, b, rid(c))
	}))
	staff.Post("/consents/:id/withdraw", api.require(""), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b patients.WithdrawConsentInput) (any, error) {
		return pt.WithdrawConsent(c.Context(), a, id, b, rid(c))
	}))

	// ---- clinical history: allergies, problems, history entries
	staff.Get("/patients/:id/allergies", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, id string) (any, error) {
		return items(cl.ListAllergies(c.Context(), a, id, rid(c)))
	}))
	staff.Post("/patients/:id/allergies", api.require(""), withBody(201, func(c fiber.Ctx, a identity.Actor, id string, b clinical.AllergyInput) (any, error) {
		return cl.AddAllergy(c.Context(), a, id, b, rid(c))
	}))
	staff.Put("/allergies/:id", api.require(""), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b clinical.AllergyUpdate) (any, error) {
		return cl.UpdateAllergy(c.Context(), a, id, b, rid(c))
	}))
	staff.Get("/patients/:id/problems", api.require("clinical.read"), noBody(func(c fiber.Ctx, a identity.Actor, id string) (any, error) {
		return items(cl.ListProblems(c.Context(), a, id, rid(c)))
	}))
	staff.Post("/patients/:id/problems", api.require(""), withBody(201, func(c fiber.Ctx, a identity.Actor, id string, b clinical.ProblemInput) (any, error) {
		return cl.AddProblem(c.Context(), a, id, b, rid(c))
	}))
	staff.Put("/problems/:id", api.require(""), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b clinical.ProblemUpdate) (any, error) {
		return cl.UpdateProblem(c.Context(), a, id, b, rid(c))
	}))
	staff.Get("/patients/:id/history", api.require("clinical.read"), noBody(func(c fiber.Ctx, a identity.Actor, id string) (any, error) {
		if e := oneOf(c, "history_type", "past_medical", "surgical", "family", "social", "obstetric", "immunization", "other"); e != nil {
			return nil, e
		}
		return items(cl.ListHistory(c.Context(), a, id, c.Query("history_type"), rid(c)))
	}))
	staff.Post("/patients/:id/history", api.require(""), withBody(201, func(c fiber.Ctx, a identity.Actor, id string, b clinical.HistoryInput) (any, error) {
		return cl.AddHistory(c.Context(), a, id, b, rid(c))
	}))
	staff.Post("/history/:id/void", api.require(""), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b clinical.VoidInput) (any, error) {
		return cl.VoidHistory(c.Context(), a, id, b, rid(c))
	}))

	// ---- org departments & service points
	staff.Post("/departments", api.require("admin.facility.manage"), withBody(201, func(c fiber.Ctx, a identity.Actor, _ string, b clinical.DepartmentInput) (any, error) {
		return cl.CreateDepartment(c.Context(), a, b, rid(c))
	}))
	staff.Get("/departments/:id", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, id string) (any, error) {
		return cl.GetDepartment(c.Context(), a, id, rid(c))
	}))
	staff.Put("/departments/:id", api.require("admin.facility.manage"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b clinical.DepartmentUpdate) (any, error) {
		return cl.UpdateDepartment(c.Context(), a, id, b, rid(c))
	}))
	staff.Post("/departments/:id/status", api.require("admin.facility.manage"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b clinical.DepartmentStatusInput) (any, error) {
		return cl.SetDepartmentStatus(c.Context(), a, id, b, rid(c))
	}))
	staff.Get("/service-points", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		if e := optUUID(c, "department_id"); e != nil {
			return nil, e
		}
		return items(cl.ServicePoints(c.Context(), a, c.Query("department_id"), rid(c)))
	}))
	staff.Post("/service-points", api.require("admin.facility.manage"), withBody(201, func(c fiber.Ctx, a identity.Actor, _ string, b clinical.ServicePointInput) (any, error) {
		return cl.CreateServicePoint(c.Context(), a, b, rid(c))
	}))

	// ---- appointments & telehealth
	staff.Post("/appointments", api.require("appointment.manage"), withBody(201, func(c fiber.Ctx, a identity.Actor, _ string, b telehealth.AppointmentInput) (any, error) {
		return th.CreateAppointment(c.Context(), a, b, c.Get("Idempotency-Key"), rid(c))
	}))
	staff.Get("/appointments", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		after, n, e := page(c)
		if e != nil {
			return nil, e
		}
		if e = optUUID(c, "patient_id", "department_id"); e != nil {
			return nil, e
		}
		if e = oneOf(c, "status", "requested", "booked", "confirmed", "checked_in", "completed", "cancelled", "no_show", "rejected"); e != nil {
			return nil, e
		}
		return paged(n)(th.ListAppointments(c.Context(), a, c.Query("patient_id"), c.Query("department_id"), c.Query("status"), after, rid(c), n))
	}))
	staff.Get("/appointments/:id", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, id string) (any, error) {
		return th.GetAppointment(c.Context(), a, id, rid(c))
	}))
	staff.Put("/appointments/:id", api.require("appointment.manage"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b telehealth.AppointmentUpdate) (any, error) {
		return th.UpdateAppointment(c.Context(), a, id, b, rid(c))
	}))
	staff.Post("/appointments/:id/status", api.require("appointment.manage"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b telehealth.AppointmentStatusInput) (any, error) {
		return th.ChangeAppointmentStatus(c.Context(), a, id, b, rid(c))
	}))
	staff.Post("/telehealth/sessions", api.require(""), withBody(201, func(c fiber.Ctx, a identity.Actor, _ string, b telehealth.SessionInput) (any, error) {
		return th.CreateSession(c.Context(), a, b, rid(c))
	}))
	staff.Get("/telehealth/sessions", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		if e := optUUID(c, "patient_id"); e != nil {
			return nil, e
		}
		if e := oneOf(c, "status", "scheduled", "waiting_room", "in_progress", "completed", "cancelled", "failed", "no_show"); e != nil {
			return nil, e
		}
		return items(th.ListSessions(c.Context(), a, c.Query("patient_id"), c.Query("status"), rid(c)))
	}))
	staff.Get("/telehealth/sessions/:id", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, id string) (any, error) {
		return th.GetSession(c.Context(), a, id, rid(c))
	}))
	staff.Post("/telehealth/sessions/:id/status", api.require(""), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b telehealth.SessionStatusInput) (any, error) {
		return th.ChangeSessionStatus(c.Context(), a, id, b, rid(c))
	}))
	staff.Post("/telehealth/sessions/:id/events", api.require("telehealth.conduct"), withBody(201, func(c fiber.Ctx, a identity.Actor, id string, b telehealth.SessionEventInput) (any, error) {
		return th.AddSessionEvent(c.Context(), a, id, b, rid(c))
	}))

	// ---- documents
	staff.Post("/patients/:id/documents", api.require("document.upload"), withBody(201, func(c fiber.Ctx, a identity.Actor, id string, b documents.CreateInput) (any, error) {
		return dc.Register(c.Context(), a, id, b, c.Get("Idempotency-Key"), rid(c))
	}))
	staff.Get("/patients/:id/documents", api.require("document.read"), noBody(func(c fiber.Ctx, a identity.Actor, id string) (any, error) {
		return items(dc.List(c.Context(), a, id, c.Query("include_deleted") == "true", rid(c)))
	}))
	staff.Get("/documents/:id", api.require("document.read"), noBody(func(c fiber.Ctx, a identity.Actor, id string) (any, error) {
		return dc.Get(c.Context(), a, id, rid(c))
	}))
	staff.Post("/documents/:id/status", api.require(""), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b documents.StatusInput) (any, error) {
		return dc.UpdateStatus(c.Context(), a, id, b, rid(c))
	}))
	staff.Post("/documents/:id/delete", api.require("document.delete"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b documents.DeleteInput) (any, error) {
		return dc.Delete(c.Context(), a, id, b, rid(c))
	}))
	staff.Get("/documents/:id/download", api.require("document.read"), noBody(func(c fiber.Ctx, a identity.Actor, id string) (any, error) {
		return dc.Download(c.Context(), a, id, rid(c))
	}))

	// ---- billing insurance coverages
	staff.Post("/patients/:id/coverages", api.require(""), withBody(201, func(c fiber.Ctx, a identity.Actor, id string, b billing.CoverageInput) (any, error) {
		return bl.AddCoverage(c.Context(), a, id, b, rid(c))
	}))
	staff.Get("/patients/:id/coverages", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, id string) (any, error) {
		return items(bl.ListCoverages(c.Context(), a, id, rid(c)))
	}))
	staff.Post("/coverages/:id/reveal", api.require("patient.identifier.reveal"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b billing.RevealInput) (any, error) {
		return bl.RevealCoverage(c.Context(), a, id, b, rid(c))
	}))
	staff.Put("/coverages/:id", api.require(""), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b billing.CoverageUpdate) (any, error) {
		return bl.UpdateCoverage(c.Context(), a, id, b, rid(c))
	}))

	// ---- break-glass, privacy reports, governance, access reviews
	staff.Post("/patients/:id/break-glass", api.require("break_glass.use"), withBody(201, func(c fiber.Ctx, a identity.Actor, id string, b privacy.BreakGlassInput) (any, error) {
		return pr.OpenBreakGlass(c.Context(), a, id, b, c.Get("Idempotency-Key"), rid(c))
	}))
	staff.Post("/break-glass/:id/end", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, id string) (any, error) {
		return pr.EndBreakGlass(c.Context(), a, id, rid(c))
	}))
	staff.Get("/privacy/break-glass", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		after, n, e := page(c)
		if e != nil {
			return nil, e
		}
		return paged(n)(pr.ListBreakGlass(c.Context(), a, c.Query("pending") == "true", after, rid(c), n))
	}))
	staff.Post("/privacy/break-glass/:id/review", api.require("audit.read"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b privacy.BreakGlassReview) (any, error) {
		return pr.ReviewBreakGlass(c.Context(), a, id, b, rid(c))
	}))
	staff.Get("/privacy/patients/:id/access", api.require("audit.read"), noBody(func(c fiber.Ctx, a identity.Actor, id string) (any, error) {
		before := int64(9223372036854775807)
		if raw := c.Query("before"); raw != "" {
			v, e := strconv.ParseInt(raw, 10, 64)
			if e != nil || v < 1 {
				return nil, platform.Fail(422, "validation", "Invalid before cursor")
			}
			before = v
		}
		return items(pr.PatientAccess(c.Context(), a, id, before, rid(c)))
	}))
	staff.Get("/privacy/alerts", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		after, n, e := page(c)
		if e != nil {
			return nil, e
		}
		if e = oneOf(c, "status", "open", "acknowledged", "resolved", "false_positive"); e != nil {
			return nil, e
		}
		return paged(n)(pr.Alerts(c.Context(), a, c.Query("status"), after, rid(c), n))
	}))
	staff.Post("/privacy/alerts/detect", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		n, e := pr.DetectAlerts(c.Context(), a, api.Config.PrivacyReadThreshold, api.Config.PrivacyMFAThreshold, rid(c))
		if e != nil {
			return nil, e
		}
		return reply{"new_alerts": n}, nil
	}))
	staff.Post("/privacy/alerts/:id/review", api.require(""), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b privacy.AlertReviewInput) (any, error) {
		return pr.ReviewAlert(c.Context(), a, id, b, rid(c))
	}))
	staff.Post("/privacy/requests", api.require("privacy.request.manage"), withBody(201, func(c fiber.Ctx, a identity.Actor, _ string, b privacy.PrivacyRequestInput) (any, error) {
		return pr.CreatePrivacyRequest(c.Context(), a, b, rid(c))
	}))
	staff.Get("/privacy/requests", api.require("privacy.request.manage"), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		if e := optUUID(c, "patient_id"); e != nil {
			return nil, e
		}
		if e := oneOf(c, "status", "received", "identity_verified", "in_progress", "fulfilled", "partially_fulfilled", "denied", "withdrawn"); e != nil {
			return nil, e
		}
		return items(pr.ListPrivacyRequests(c.Context(), a, c.Query("status"), c.Query("patient_id"), rid(c)))
	}))
	staff.Post("/privacy/requests/:id/status", api.require("privacy.request.manage"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b privacy.PrivacyRequestStatus) (any, error) {
		return pr.UpdatePrivacyRequestStatus(c.Context(), a, id, b, rid(c))
	}))
	staff.Post("/privacy/legal-holds", api.require("privacy.request.manage"), withBody(201, func(c fiber.Ctx, a identity.Actor, _ string, b privacy.LegalHoldInput) (any, error) {
		return pr.PlaceLegalHold(c.Context(), a, b, rid(c))
	}))
	staff.Get("/privacy/legal-holds", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		return items(pr.ListLegalHolds(c.Context(), a, c.Query("active") == "true", rid(c)))
	}))
	staff.Post("/privacy/legal-holds/:id/release", api.require("privacy.request.manage"), noBody(func(c fiber.Ctx, a identity.Actor, id string) (any, error) {
		return pr.ReleaseLegalHold(c.Context(), a, id, rid(c))
	}))
	staff.Post("/access-reviews", api.require("admin.access_review"), withBody(201, func(c fiber.Ctx, a identity.Actor, _ string, b privacy.AccessReviewInput) (any, error) {
		return pr.CreateAccessReview(c.Context(), a, b, rid(c))
	}))
	staff.Get("/access-reviews", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		return items(pr.ListAccessReviews(c.Context(), a, rid(c)))
	}))
	staff.Get("/access-reviews/:id", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, id string) (any, error) {
		return pr.GetAccessReview(c.Context(), a, id, rid(c))
	}))
	staff.Post("/access-reviews/:id/items/:item_id/decide", api.require("admin.access_review"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b privacy.AccessReviewDecision) (any, error) {
		if e := uuidParam(c, "item_id"); e != nil {
			return nil, e
		}
		return pr.DecideAccessReviewItem(c.Context(), a, id, c.Params("item_id"), b, rid(c))
	}))
	staff.Post("/access-reviews/:id/complete", api.require("admin.access_review"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b privacy.AccessReviewComplete) (any, error) {
		return pr.CompleteAccessReview(c.Context(), a, id, b, rid(c))
	}))

	// ---- dual-control approvals
	staff.Post("/approvals", api.require("approval.request"), withBody(201, func(c fiber.Ctx, a identity.Actor, _ string, b approvals.Input) (any, error) {
		return ap.Create(c.Context(), a, b, c.Get("Idempotency-Key"), rid(c))
	}))
	staff.Get("/approvals", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		after, n, e := page(c)
		if e != nil {
			return nil, e
		}
		if e = oneOf(c, "status", "pending", "approved", "rejected"); e != nil {
			return nil, e
		}
		return paged(n)(ap.List(c.Context(), a, c.Query("status"), after, rid(c), n))
	}))
	staff.Post("/approvals/:id/approve", api.require("approval.review"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b approvals.Decision) (any, error) {
		return ap.Decide(c.Context(), a, id, b, true, rid(c))
	}))
	staff.Post("/approvals/:id/reject", api.require("approval.review"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b approvals.Decision) (any, error) {
		return ap.Decide(c.Context(), a, id, b, false, rid(c))
	}))

	// ---- management reporting views
	staff.Get("/reports/daily-visits", api.require("report.view"), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		if e := optDate(c, "from"); e != nil {
			return nil, e
		}
		if e := optDate(c, "to"); e != nil {
			return nil, e
		}
		return items(rp.DailyVisits(c.Context(), a, c.Query("from"), c.Query("to"), rid(c)))
	}))
	staff.Get("/reports/daily-revenue", api.require("report.view"), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		if e := optDate(c, "from"); e != nil {
			return nil, e
		}
		if e := optDate(c, "to"); e != nil {
			return nil, e
		}
		return items(rp.DailyRevenue(c.Context(), a, c.Query("from"), c.Query("to"), rid(c)))
	}))
	staff.Get("/reports/receivables", api.require("report.view"), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		return items(rp.Receivables(c.Context(), a, rid(c)))
	}))
	staff.Get("/reports/billing-reconciliation", api.require("report.view"), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		return items(rp.BillingReconciliation(c.Context(), a, rid(c)))
	}))
	staff.Get("/reports/queue-wait-times", api.require("report.view"), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		return items(rp.QueueWaitTimes(c.Context(), a, rid(c)))
	}))
	staff.Get("/reports/stock-expiry", api.require("report.view"), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		return items(rp.StockExpiry(c.Context(), a, rid(c)))
	}))
	staff.Get("/reports/diagnosis-counts", api.require("report.view"), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		return items(rp.DiagnosisCounts(c.Context(), a, rid(c)))
	}))
	staff.Get("/reports/break-glass-pending", api.require("audit.read"), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		return items(rp.BreakGlassPending(c.Context(), a, rid(c)))
	}))

	// ---- communications (templates, delivery logs, outbox)
	staff.Get("/comms/templates", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		if e := oneOf(c, "channel", "sms", "whatsapp", "email", "push"); e != nil {
			return nil, e
		}
		return items(cm.Templates(c.Context(), a, c.Query("channel"), rid(c)))
	}))
	staff.Post("/comms/templates", api.require("comms.template.manage"), withBody(201, func(c fiber.Ctx, a identity.Actor, _ string, b comms.TemplateInput) (any, error) {
		return cm.CreateTemplate(c.Context(), a, b, rid(c))
	}))
	staff.Post("/comms/templates/:id/activate", api.require("comms.template.manage"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b comms.VersionInput) (any, error) {
		return cm.ActivateTemplate(c.Context(), a, id, b, rid(c))
	}))
	staff.Get("/comms/messages", api.require("comms.read"), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		if e := optUUID(c, "patient_id"); e != nil {
			return nil, e
		}
		if e := oneOf(c, "status", "queued", "sending", "sent", "delivered", "failed", "cancelled"); e != nil {
			return nil, e
		}
		return items(cm.Messages(c.Context(), a, c.Query("patient_id"), c.Query("status"), rid(c)))
	}))
	staff.Get("/comms/outbox", api.require("comms.read"), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		if e := oneOf(c, "status", "pending", "processing", "succeeded", "failed", "dead"); e != nil {
			return nil, e
		}
		return items(cm.Outbox(c.Context(), a, c.Query("status"), rid(c)))
	}))
	staff.Post("/comms/outbox", api.require(""), withBody(201, func(c fiber.Ctx, a identity.Actor, _ string, b comms.OutboxInput) (any, error) {
		return cm.EnqueueOutbox(c.Context(), a, b, rid(c))
	}))

	// ---- patient portal accounts
	staff.Get("/patients/:id/portal-accounts", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, id string) (any, error) {
		return items(pt.ListPortalAccounts(c.Context(), a, id, rid(c)))
	}))
	staff.Post("/patients/:id/portal-accounts", api.require(""), withBody(201, func(c fiber.Ctx, a identity.Actor, id string, b patients.PortalAccountInput) (any, error) {
		return pt.CreatePortalAccount(c.Context(), a, id, b, rid(c))
	}))
	staff.Post("/portal-accounts/:id/status", api.require(""), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b patients.PortalAccountStatus) (any, error) {
		return pt.ChangePortalAccountStatus(c.Context(), a, id, b, rid(c))
	}))

	// ---- organization settings, staff-departments, RBAC roles, API credentials, integrations, governance
	ad := api.Admin
	staff.Get("/organization", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		return ad.GetOrganization(c.Context(), a, rid(c))
	}))
	staff.Put("/organization", api.require("admin.org.manage"), withBody(200, func(c fiber.Ctx, a identity.Actor, _ string, b admin.OrgUpdate) (any, error) {
		return ad.UpdateOrganization(c.Context(), a, b, rid(c))
	}))
	staff.Get("/staff-departments", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		if e := optUUID(c, "department_id", "membership_id"); e != nil {
			return nil, e
		}
		return items(ad.ListStaffDepartments(c.Context(), a, c.Query("department_id"), c.Query("membership_id"), rid(c)))
	}))
	staff.Post("/staff-departments", api.require("admin.user.manage"), withBody(201, func(c fiber.Ctx, a identity.Actor, _ string, b admin.StaffDepartmentInput) (any, error) {
		return ad.AssignStaffDepartment(c.Context(), a, b, rid(c))
	}))
	staff.Post("/staff-departments/remove", api.require("admin.user.manage"), withBody(200, func(c fiber.Ctx, a identity.Actor, _ string, b admin.StaffDepartmentInput) (any, error) {
		if e := ad.RemoveStaffDepartment(c.Context(), a, b, rid(c)); e != nil {
			return nil, e
		}
		return reply{"removed": true}, nil
	}))
	staff.Get("/permissions", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		return items(ad.ListPermissions(c.Context(), a, rid(c)))
	}))
	staff.Get("/roles", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		return items(ad.ListRoles(c.Context(), a, rid(c)))
	}))
	staff.Post("/roles", api.require("admin.role.manage"), withBody(201, func(c fiber.Ctx, a identity.Actor, _ string, b admin.RoleInput) (any, error) {
		return ad.CreateRole(c.Context(), a, b, rid(c))
	}))
	staff.Put("/roles/:id", api.require("admin.role.manage"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b admin.RoleUpdate) (any, error) {
		return ad.UpdateRole(c.Context(), a, id, b, rid(c))
	}))
	staff.Get("/api-credentials", api.require("admin.user.manage"), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		return items(ad.ListAPICredentials(c.Context(), a, rid(c)))
	}))
	staff.Post("/api-credentials", api.require("admin.user.manage"), withBody(201, func(c fiber.Ctx, a identity.Actor, _ string, b admin.APICredentialInput) (any, error) {
		return ad.CreateAPICredential(c.Context(), a, b, rid(c))
	}))
	staff.Post("/api-credentials/:id/revoke", api.require("admin.user.manage"), noBody(func(c fiber.Ctx, a identity.Actor, id string) (any, error) {
		return ad.RevokeAPICredential(c.Context(), a, id, rid(c))
	}))
	staff.Get("/integrations/providers", api.require("admin.org.manage"), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		return items(ad.ListProviders(c.Context(), a, rid(c)))
	}))
	staff.Post("/integrations/providers", api.require("admin.org.manage"), withBody(201, func(c fiber.Ctx, a identity.Actor, _ string, b admin.ProviderConfigInput) (any, error) {
		return ad.CreateProvider(c.Context(), a, b, rid(c))
	}))
	staff.Put("/integrations/providers/:id", api.require("admin.org.manage"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b admin.ProviderConfigUpdate) (any, error) {
		return ad.UpdateProvider(c.Context(), a, id, b, rid(c))
	}))
	staff.Get("/governance/data-categories", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		return items(ad.DataCategories(c.Context(), a, rid(c)))
	}))
}
