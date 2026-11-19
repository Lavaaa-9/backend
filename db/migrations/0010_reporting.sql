-- =============================================================================
-- 0010_reporting.sql — management reporting views.
-- security_invoker = true → the caller's RLS applies (a manager without
-- clinical.read cannot see diagnosis-level data through a view).
-- Materialized views are deliberately NOT used for tenant data: PostgreSQL
-- does not apply RLS to materialized views. If volume later requires them,
-- they must live behind a security_invoker view that filters org_id.
-- =============================================================================

CREATE VIEW reporting.v_daily_visits WITH (security_invoker = true) AS
SELECT e.org_id, e.facility_id,
       (e.arrived_at AT TIME ZONE o.timezone)::date AS visit_date,
       e.encounter_type,
       count(*) FILTER (WHERE e.status <> 'entered_in_error')        AS visits,
       count(*) FILTER (WHERE e.status = 'completed')                AS completed,
       count(*) FILTER (WHERE e.status = 'cancelled')                AS cancelled,
       count(DISTINCT e.patient_id)                                  AS unique_patients
FROM clinical.encounter e JOIN org.organization o ON o.id = e.org_id
GROUP BY 1, 2, 3, 4;

CREATE VIEW reporting.v_daily_revenue WITH (security_invoker = true) AS
SELECT p.org_id, p.facility_id,
       (p.received_at AT TIME ZONE o.timezone)::date AS payment_date,
       p.method, p.provider,
       count(*)      AS payments,
       sum(p.amount) AS amount
FROM billing.payment p JOIN org.organization o ON o.id = p.org_id
WHERE p.status = 'completed'
GROUP BY 1, 2, 3, 4, 5;

CREATE VIEW reporting.v_receivables WITH (security_invoker = true) AS
SELECT i.org_id, i.facility_id, i.payer_id, i.status,
       count(*) AS invoices, sum(i.total) AS billed, sum(i.amount_paid) AS collected, sum(i.balance) AS outstanding,
       sum(i.balance) FILTER (WHERE i.issued_at < now() - interval '30 days') AS outstanding_over_30d
FROM billing.invoice i
WHERE i.status IN ('issued','partially_paid','paid')
GROUP BY 1, 2, 3, 4;

-- Phase-4 gate: "management report matches underlying records".
-- Any row returned is a reconciliation defect.
CREATE VIEW reporting.v_billing_reconciliation_exceptions WITH (security_invoker = true) AS
SELECT i.org_id, i.id AS invoice_id, i.invoice_no, 'line_total_mismatch' AS exception,
       i.total AS recorded, coalesce(l.sum_lines, 0) AS computed
FROM billing.invoice i
LEFT JOIN (SELECT invoice_id, sum(line_total) AS sum_lines FROM billing.invoice_line WHERE NOT voided GROUP BY 1) l
       ON l.invoice_id = i.id
WHERE i.status <> 'void' AND i.total <> coalesce(l.sum_lines, 0)
UNION ALL
SELECT i.org_id, i.id, i.invoice_no, 'paid_amount_mismatch', i.amount_paid, coalesce(a.paid, 0)
FROM billing.invoice i
LEFT JOIN (SELECT a.invoice_id, sum(a.amount) AS paid FROM billing.payment_allocation a
           JOIN billing.payment p ON p.id = a.payment_id AND p.status = 'completed' GROUP BY 1) a
       ON a.invoice_id = i.id
WHERE i.amount_paid <> coalesce(a.paid, 0)
UNION ALL
SELECT p.org_id, p.id, NULL, 'completed_payment_without_receipt', p.amount, NULL
FROM billing.payment p LEFT JOIN billing.receipt r ON r.payment_id = p.id
WHERE p.status = 'completed' AND r.id IS NULL;

CREATE VIEW reporting.v_queue_wait_times WITH (security_invoker = true) AS
SELECT q.org_id, q.facility_id, q.department_id, q.queue_date,
       count(*) AS tickets,
       percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM q.service_started_at - q.created_at) / 60) AS median_wait_min,
       percentile_cont(0.9) WITHIN GROUP (ORDER BY extract(epoch FROM q.service_started_at - q.created_at) / 60) AS p90_wait_min
FROM clinical.queue_ticket q
WHERE q.service_started_at IS NOT NULL
GROUP BY 1, 2, 3, 4;

CREATE VIEW reporting.v_stock_expiry WITH (security_invoker = true) AS
SELECT b.org_id, b.facility_id, d.code, d.generic_name, d.strength, b.batch_no, b.expiry_date, b.quantity_on_hand
FROM pharmacy.stock_batch b JOIN pharmacy.drug d ON d.id = b.drug_id
WHERE b.status = 'active' AND b.quantity_on_hand > 0 AND b.expiry_date <= current_date + 90;

-- Morbidity by ICD-10 (requires clinical.read through RLS).
CREATE VIEW reporting.v_diagnosis_counts WITH (security_invoker = true) AS
SELECT d.org_id, date_trunc('month', d.created_at)::date AS month, d.icd10_code, c.title, count(*) AS cases
FROM clinical.encounter_diagnosis d JOIN ref.icd10_code c ON c.code = d.icd10_code
WHERE d.status = 'active' AND d.diagnosis_type = 'final'
GROUP BY 1, 2, 3, 4;

-- Compliance: unreviewed break-glass events and stale access.
CREATE VIEW reporting.v_break_glass_pending_review WITH (security_invoker = true) AS
SELECT b.org_id, b.id, b.membership_id, b.patient_id, b.started_at, b.expires_at, b.reason
FROM iam.break_glass_access b WHERE b.reviewed_at IS NULL;

GRANT SELECT ON ALL TABLES IN SCHEMA reporting TO spartial_app;
