package httpapi

import (
	"encoding/json"

	"github.com/gofiber/fiber/v3"
	"spartial.local/backend/internal/modules/billing"
	"spartial.local/backend/internal/modules/clinical"
	"spartial.local/backend/internal/modules/diagnostics"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/modules/pharmacy"
	"spartial.local/backend/internal/platform"
)

type reply = fiber.Map

func items(v []platform.Raw, e error) (any, error) {
	if e != nil {
		return nil, e
	}
	return reply{"items": v}, nil
}

// paged wraps a page of documents with the cursor for the next page.
func paged(n int) func(v []platform.Raw, e error) (any, error) {
	return func(v []platform.Raw, e error) (any, error) { return pagedOf(v, n, e) }
}

func pagedOf(v []platform.Raw, n int, e error) (any, error) {
	if e != nil {
		return nil, e
	}
	next := ""
	if len(v) == n && n > 0 {
		var last struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(v[len(v)-1], &last)
		next = last.ID
	}
	return reply{"items": v, "next_cursor": next}, nil
}

func uuidParam(c fiber.Ctx, names ...string) error {
	for _, n := range names {
		if e := validID(c.Params(n)); e != nil {
			return e
		}
	}
	return nil
}

// withBody binds a JSON body, validates :id path params, and sends fn's result.
func withBody[B any](code int, fn func(c fiber.Ctx, a identity.Actor, id string, b B) (any, error)) fiber.Handler {
	return func(c fiber.Ctx) error {
		if e := uuidParam(c, "id"); c.Params("id") != "" && e != nil {
			return e
		}
		var b B
		if e := bind(c, &b); e != nil {
			return e
		}
		out, e := fn(c, actor(c), c.Params("id"), b)
		if e != nil {
			return e
		}
		return c.Status(code).JSON(out)
	}
}

func noBody(fn func(c fiber.Ctx, a identity.Actor, id string) (any, error)) fiber.Handler {
	return func(c fiber.Ctx) error {
		if c.Params("id") != "" {
			if e := uuidParam(c, "id"); e != nil {
				return e
			}
		}
		out, e := fn(c, actor(c), c.Params("id"))
		if e != nil {
			return e
		}
		return c.JSON(out)
	}
}

func optUUID(c fiber.Ctx, names ...string) error {
	for _, n := range names {
		if v := c.Query(n); v != "" {
			if e := validID(v); e != nil {
				return e
			}
		}
	}
	return nil
}

func oneOf(c fiber.Ctx, name string, allowed ...string) error {
	v := c.Query(name)
	if v == "" {
		return nil
	}
	for _, a := range allowed {
		if a == v {
			return nil
		}
	}
	return platform.Fail(422, "validation", "Invalid "+name)
}

// mountExt registers the clinical, diagnostics, pharmacy and billing routes.
func (api API) mountExt(staff fiber.Router) {
	cl, dg, rx, bl := api.Clinical, api.Diag, api.Pharmacy, api.Billing

	// ---- clinical
	staff.Post("/encounters/:id/triage", api.require("triage.write"), withBody(201, func(c fiber.Ctx, a identity.Actor, id string, b clinical.TriageInput) (any, error) {
		return cl.RecordTriage(c.Context(), a, id, b, c.Get("Idempotency-Key"), rid(c))
	}))
	staff.Get("/encounters/:id/triage", api.require("clinical.read"), noBody(func(c fiber.Ctx, a identity.Actor, id string) (any, error) {
		return items(cl.ListTriage(c.Context(), a, id, rid(c)))
	}))
	staff.Post("/triage/:id/finalise", api.require("triage.write"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b clinical.VersionInput) (any, error) {
		return cl.FinaliseTriage(c.Context(), a, id, b, rid(c))
	}))
	staff.Post("/triage/:id/void", api.require("triage.write"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b clinical.VoidInput) (any, error) {
		return cl.VoidTriage(c.Context(), a, id, b, rid(c))
	}))
	staff.Post("/encounters/:id/consultations", api.require("consultation.write"), withBody(201, func(c fiber.Ctx, a identity.Actor, id string, b clinical.ConsultationInput) (any, error) {
		return cl.CreateConsultation(c.Context(), a, id, b, rid(c))
	}))
	staff.Get("/encounters/:id/consultations", api.require("clinical.read"), noBody(func(c fiber.Ctx, a identity.Actor, id string) (any, error) {
		return items(cl.ListConsultations(c.Context(), a, id, rid(c)))
	}))
	staff.Put("/consultations/:id", api.require("consultation.write"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b clinical.ConsultationUpdate) (any, error) {
		return cl.UpdateConsultation(c.Context(), a, id, b, rid(c))
	}))
	staff.Post("/consultations/:id/sign", api.require("consultation.write"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b clinical.VersionInput) (any, error) {
		return cl.SignConsultation(c.Context(), a, id, b, rid(c))
	}))
	staff.Post("/consultations/:id/void", api.require("consultation.write"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b clinical.VoidInput) (any, error) {
		return cl.VoidConsultation(c.Context(), a, id, b, rid(c))
	}))
	staff.Post("/encounters/:id/diagnoses", api.require("consultation.write"), withBody(201, func(c fiber.Ctx, a identity.Actor, id string, b clinical.DiagnosisInput) (any, error) {
		return cl.AddDiagnosis(c.Context(), a, id, b, rid(c))
	}))
	staff.Get("/encounters/:id/diagnoses", api.require("clinical.read"), noBody(func(c fiber.Ctx, a identity.Actor, id string) (any, error) {
		return items(cl.ListDiagnoses(c.Context(), a, id, rid(c)))
	}))
	staff.Post("/diagnoses/:id/void", api.require("consultation.write"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b clinical.VoidInput) (any, error) {
		return cl.VoidDiagnosis(c.Context(), a, id, b, rid(c))
	}))
	staff.Get("/icd10", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		return items(cl.ICD10(c.Context(), a, c.Query("q"), rid(c)))
	}))
	staff.Post("/encounters/:id/follow-ups", api.require(""), withBody(201, func(c fiber.Ctx, a identity.Actor, id string, b clinical.FollowUpInput) (any, error) {
		return cl.AddFollowUp(c.Context(), a, id, b, rid(c))
	}))
	staff.Get("/encounters/:id/follow-ups", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, id string) (any, error) {
		return items(cl.ListFollowUps(c.Context(), a, id, rid(c)))
	}))
	staff.Post("/follow-ups/:id/cancel", api.require(""), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b clinical.VersionInput) (any, error) {
		return cl.CancelFollowUp(c.Context(), a, id, b, rid(c))
	}))
	staff.Get("/departments", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		return items(cl.Departments(c.Context(), a, rid(c)))
	}))
	staff.Post("/queue", api.require("queue.manage"), withBody(201, func(c fiber.Ctx, a identity.Actor, _ string, b clinical.QueueInput) (any, error) {
		return cl.JoinQueue(c.Context(), a, b, rid(c))
	}))
	staff.Get("/queue", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		if e := optUUID(c, "department_id"); e != nil {
			return nil, e
		}
		return items(cl.Queue(c.Context(), a, c.Query("department_id"), rid(c)))
	}))
	staff.Post("/queue/:id/status", api.require("queue.manage"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b clinical.QueueStatus) (any, error) {
		return cl.QueueStatus(c.Context(), a, id, b, rid(c))
	}))

	// ---- diagnostics
	staff.Get("/diagnostics/tests", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		if e := oneOf(c, "category", "laboratory", "imaging", "other_diagnostic"); e != nil {
			return nil, e
		}
		return items(dg.Tests(c.Context(), a, c.Query("category"), rid(c)))
	}))
	staff.Post("/diagnostics/tests", api.require("diag.catalog.manage"), withBody(201, func(c fiber.Ctx, a identity.Actor, _ string, b diagnostics.TestInput) (any, error) {
		return dg.CreateTest(c.Context(), a, b, rid(c))
	}))
	staff.Post("/diagnostics/components/:id/ranges", api.require("diag.catalog.manage"), withBody(201, func(c fiber.Ctx, a identity.Actor, id string, b diagnostics.RangeInput) (any, error) {
		return dg.AddRange(c.Context(), a, id, b, rid(c))
	}))
	staff.Post("/encounters/:id/diagnostic-requests", api.require("diag.request.create"), withBody(201, func(c fiber.Ctx, a identity.Actor, id string, b diagnostics.RequestInput) (any, error) {
		return dg.CreateRequest(c.Context(), a, id, b, c.Get("Idempotency-Key"), rid(c))
	}))
	staff.Get("/diagnostic-requests", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		after, n, e := page(c)
		if e != nil {
			return nil, e
		}
		if e = optUUID(c, "patient_id", "encounter_id"); e != nil {
			return nil, e
		}
		if e = oneOf(c, "status", "requested", "in_progress", "partially_resulted", "completed", "cancelled"); e != nil {
			return nil, e
		}
		return paged(n)(dg.Requests(c.Context(), a, c.Query("status"), c.Query("patient_id"), c.Query("encounter_id"), after, rid(c), n))
	}))
	staff.Get("/diagnostic-requests/:id", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, id string) (any, error) {
		return dg.Request(c.Context(), a, id, rid(c))
	}))
	staff.Post("/diagnostic-requests/:id/cancel", api.require("diag.request.create"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b diagnostics.CancelInput) (any, error) {
		return dg.CancelRequest(c.Context(), a, id, b, rid(c))
	}))
	staff.Post("/diagnostic-items/:id/specimens", api.require("diag.specimen.manage"), withBody(201, func(c fiber.Ctx, a identity.Actor, id string, b diagnostics.SpecimenInput) (any, error) {
		return dg.CollectSpecimen(c.Context(), a, id, b, rid(c))
	}))
	staff.Post("/diagnostic-items/:id/recollect", api.require("diag.specimen.manage"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b diagnostics.VersionInput) (any, error) {
		return dg.Recollect(c.Context(), a, id, b, rid(c))
	}))
	staff.Post("/specimens/:id/receive", api.require("diag.specimen.manage"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b diagnostics.VersionInput) (any, error) {
		return dg.ReceiveSpecimen(c.Context(), a, id, b, rid(c))
	}))
	staff.Post("/specimens/:id/reject", api.require("diag.specimen.manage"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b diagnostics.RejectInput) (any, error) {
		return dg.RejectSpecimen(c.Context(), a, id, b, rid(c))
	}))
	staff.Post("/diagnostic-items/:id/results", api.require("diag.result.enter"), withBody(201, func(c fiber.Ctx, a identity.Actor, id string, b diagnostics.ResultsInput) (any, error) {
		return dg.EnterResults(c.Context(), a, id, b, rid(c))
	}))
	staff.Post("/results/:id/verify", api.require("diag.result.verify"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b diagnostics.VersionInput) (any, error) {
		return dg.VerifyResult(c.Context(), a, id, b, rid(c))
	}))
	staff.Post("/results/:id/void", api.require(""), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b diagnostics.VoidInput) (any, error) {
		return dg.VoidResult(c.Context(), a, id, b, rid(c))
	}))

	// ---- pharmacy
	staff.Get("/pharmacy/drugs", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		return items(rx.Drugs(c.Context(), a, c.Query("q"), rid(c)))
	}))
	staff.Post("/pharmacy/drugs", api.require("pharmacy.catalog.manage"), withBody(201, func(c fiber.Ctx, a identity.Actor, _ string, b pharmacy.DrugInput) (any, error) {
		return rx.CreateDrug(c.Context(), a, b, rid(c))
	}))
	staff.Get("/pharmacy/stock", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		if e := optUUID(c, "facility_id", "drug_id"); e != nil {
			return nil, e
		}
		return items(rx.Stock(c.Context(), a, c.Query("facility_id"), c.Query("drug_id"), rid(c)))
	}))
	staff.Post("/pharmacy/stock/receipts", api.require("pharmacy.stock.manage"), withBody(201, func(c fiber.Ctx, a identity.Actor, _ string, b pharmacy.BatchInput) (any, error) {
		return rx.Receive(c.Context(), a, b, rid(c))
	}))
	staff.Post("/pharmacy/stock/batches/:id/adjust", api.require("pharmacy.stock.manage"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b pharmacy.AdjustInput) (any, error) {
		return rx.Adjust(c.Context(), a, id, b, rid(c))
	}))
	staff.Get("/pharmacy/stock/batches/:id/movements", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, id string) (any, error) {
		return items(rx.Movements(c.Context(), a, id, rid(c)))
	}))
	staff.Post("/encounters/:id/prescriptions", api.require("pharmacy.prescribe"), withBody(201, func(c fiber.Ctx, a identity.Actor, id string, b pharmacy.PrescriptionInput) (any, error) {
		return rx.Prescribe(c.Context(), a, id, b, c.Get("Idempotency-Key"), rid(c))
	}))
	staff.Get("/prescriptions", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		after, n, e := page(c)
		if e != nil {
			return nil, e
		}
		if e = optUUID(c, "patient_id", "encounter_id"); e != nil {
			return nil, e
		}
		if e = oneOf(c, "status", "draft", "active", "partially_dispensed", "dispensed", "cancelled", "expired"); e != nil {
			return nil, e
		}
		return paged(n)(rx.Prescriptions(c.Context(), a, c.Query("status"), c.Query("patient_id"), c.Query("encounter_id"), after, rid(c), n))
	}))
	staff.Get("/prescriptions/:id", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, id string) (any, error) {
		return rx.Prescription(c.Context(), a, id, rid(c))
	}))
	staff.Post("/prescriptions/:id/sign", api.require("pharmacy.prescribe"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b pharmacy.VersionInput) (any, error) {
		return rx.Sign(c.Context(), a, id, b, rid(c))
	}))
	staff.Post("/prescriptions/:id/cancel", api.require("pharmacy.prescribe"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b pharmacy.CancelInput) (any, error) {
		return rx.CancelRx(c.Context(), a, id, b, rid(c))
	}))
	staff.Post("/prescriptions/:id/dispense", api.require("pharmacy.dispense"), withBody(201, func(c fiber.Ctx, a identity.Actor, id string, b pharmacy.DispenseInput) (any, error) {
		return rx.Dispense(c.Context(), a, id, b, c.Get("Idempotency-Key"), rid(c))
	}))
	staff.Post("/dispenses/:id/reverse", api.require("pharmacy.dispense"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b pharmacy.ReverseInput) (any, error) {
		return rx.ReverseDispense(c.Context(), a, id, b, rid(c))
	}))

	// ---- billing
	staff.Get("/billing/payers", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		return items(bl.Payers(c.Context(), a, rid(c)))
	}))
	staff.Post("/billing/payers", api.require("billing.price.manage"), withBody(201, func(c fiber.Ctx, a identity.Actor, _ string, b billing.PayerInput) (any, error) {
		return bl.CreatePayer(c.Context(), a, b, rid(c))
	}))
	staff.Get("/billing/services", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		return items(bl.Services(c.Context(), a, c.Query("category"), rid(c)))
	}))
	staff.Post("/billing/services", api.require("billing.price.manage"), withBody(201, func(c fiber.Ctx, a identity.Actor, _ string, b billing.ServiceInput) (any, error) {
		return bl.CreateService(c.Context(), a, b, rid(c))
	}))
	staff.Get("/billing/services/:id/prices", api.require(""), noBody(func(c fiber.Ctx, a identity.Actor, id string) (any, error) {
		return items(bl.Prices(c.Context(), a, id, rid(c)))
	}))
	staff.Post("/billing/services/:id/prices", api.require("billing.price.manage"), withBody(201, func(c fiber.Ctx, a identity.Actor, id string, b billing.PriceInput) (any, error) {
		return bl.SetPrice(c.Context(), a, id, b, rid(c))
	}))
	staff.Post("/billing/invoices", api.require("billing.invoice.manage"), withBody(201, func(c fiber.Ctx, a identity.Actor, _ string, b billing.InvoiceInput) (any, error) {
		return bl.CreateInvoice(c.Context(), a, b, rid(c))
	}))
	staff.Get("/billing/invoices", api.require("billing.read"), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		after, n, e := page(c)
		if e != nil {
			return nil, e
		}
		if e = optUUID(c, "patient_id", "encounter_id"); e != nil {
			return nil, e
		}
		if e = oneOf(c, "status", "draft", "issued", "partially_paid", "paid", "void"); e != nil {
			return nil, e
		}
		return paged(n)(bl.Invoices(c.Context(), a, c.Query("status"), c.Query("patient_id"), c.Query("encounter_id"), after, rid(c), n))
	}))
	staff.Get("/billing/invoices/:id", api.require("billing.read"), noBody(func(c fiber.Ctx, a identity.Actor, id string) (any, error) {
		return bl.Invoice(c.Context(), a, id, rid(c))
	}))
	staff.Post("/billing/invoices/:id/lines", api.require("billing.invoice.manage"), withBody(201, func(c fiber.Ctx, a identity.Actor, id string, b billing.LineInput) (any, error) {
		return bl.AddLine(c.Context(), a, id, b, rid(c))
	}))
	staff.Post("/billing/invoice-lines/:id/remove", api.require("billing.invoice.manage"), noBody(func(c fiber.Ctx, a identity.Actor, id string) (any, error) {
		return bl.RemoveLine(c.Context(), a, id, rid(c))
	}))
	staff.Post("/billing/invoices/:id/collect-charges", api.require("billing.invoice.manage"), noBody(func(c fiber.Ctx, a identity.Actor, id string) (any, error) {
		return bl.CollectCharges(c.Context(), a, id, rid(c))
	}))
	staff.Post("/billing/invoices/:id/issue", api.require("billing.invoice.manage"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b billing.VersionInput) (any, error) {
		return bl.Issue(c.Context(), a, id, b, rid(c))
	}))
	staff.Post("/billing/invoices/:id/void", api.require("billing.invoice.void"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b billing.VoidInput) (any, error) {
		return bl.VoidInvoice(c.Context(), a, id, b, rid(c))
	}))
	staff.Get("/billing/shifts", api.require("billing.read"), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		if e := oneOf(c, "status", "open", "closed", "reconciled"); e != nil {
			return nil, e
		}
		return items(bl.Shifts(c.Context(), a, c.Query("status"), rid(c)))
	}))
	staff.Post("/billing/shifts", api.require("billing.payment.record"), withBody(201, func(c fiber.Ctx, a identity.Actor, _ string, b billing.ShiftOpen) (any, error) {
		return bl.OpenShift(c.Context(), a, b, rid(c))
	}))
	staff.Post("/billing/shifts/:id/close", api.require("billing.payment.record"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b billing.ShiftClose) (any, error) {
		return bl.CloseShift(c.Context(), a, id, b, rid(c))
	}))
	staff.Post("/billing/shifts/:id/reconcile", api.require("billing.shift.reconcile"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b billing.ShiftReconcile) (any, error) {
		return bl.ReconcileShift(c.Context(), a, id, b, rid(c))
	}))
	staff.Post("/billing/payments", api.require("billing.payment.record"), withBody(201, func(c fiber.Ctx, a identity.Actor, _ string, b billing.PaymentInput) (any, error) {
		return bl.RecordPayment(c.Context(), a, b, c.Get("Idempotency-Key"), rid(c))
	}))
	staff.Get("/billing/payments/:id", api.require("billing.read"), noBody(func(c fiber.Ctx, a identity.Actor, id string) (any, error) {
		return bl.Payment(c.Context(), a, id, rid(c))
	}))
	staff.Post("/billing/payments/:id/reverse", api.require("billing.payment.reverse"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b billing.ReverseInput) (any, error) {
		return bl.ReversePayment(c.Context(), a, id, b, rid(c))
	}))
	staff.Post("/billing/payments/:id/print-receipt", api.require("billing.read"), noBody(func(c fiber.Ctx, a identity.Actor, id string) (any, error) {
		return bl.PrintReceipt(c.Context(), a, id, rid(c))
	}))
	staff.Post("/billing/payments/:id/refunds", api.require("billing.refund.request"), withBody(201, func(c fiber.Ctx, a identity.Actor, id string, b billing.RefundInput) (any, error) {
		return bl.RequestRefund(c.Context(), a, id, b, rid(c))
	}))
	staff.Get("/billing/refunds", api.require("billing.read"), noBody(func(c fiber.Ctx, a identity.Actor, _ string) (any, error) {
		if e := oneOf(c, "status", "requested", "approved", "rejected", "paid"); e != nil {
			return nil, e
		}
		return items(bl.Refunds(c.Context(), a, c.Query("status"), rid(c)))
	}))
	staff.Post("/billing/refunds/:id/decision", api.require("billing.refund.approve"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b billing.RefundDecision) (any, error) {
		return bl.DecideRefund(c.Context(), a, id, b, rid(c))
	}))
	staff.Post("/billing/refunds/:id/pay", api.require("billing.refund.approve"), withBody(200, func(c fiber.Ctx, a identity.Actor, id string, b billing.RefundPaid) (any, error) {
		return bl.PayRefund(c.Context(), a, id, b, rid(c))
	}))
}
