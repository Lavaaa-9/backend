-- =============================================================================
-- 0007_billing.sql — payers, price list, invoices, payments, allocations,
-- receipts, refunds, cashier shifts.            [PHI-linked financial data]
--
-- Money: numeric(14,2), single currency per org (TZS). Derived totals
-- (invoice.subtotal/total/amount_paid) are maintained only by SECURITY DEFINER
-- triggers; the API cannot write them directly.
-- =============================================================================

CREATE TABLE billing.payer (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id      uuid NOT NULL REFERENCES org.organization(id),
  code        text NOT NULL CHECK (code ~ '^[A-Z0-9_-]{2,30}$'),
  name        text NOT NULL,
  payer_type  text NOT NULL CHECK (payer_type IN ('self_pay','insurance','corporate','government')),
  status      text NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive')),
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  UNIQUE (org_id, code)
);
SELECT sec.attach_standard_triggers('billing.payer', false);
ALTER TABLE clinical.encounter ADD CONSTRAINT fk_encounter_payer FOREIGN KEY (org_id, payer_id) REFERENCES billing.payer (org_id, id);

-- Patient insurance coverage (scope to be confirmed: NHIF / private schemes).
CREATE TABLE billing.coverage (
  id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id            uuid NOT NULL,
  patient_id        uuid NOT NULL,
  payer_id          uuid NOT NULL,
  member_no_ciphertext bytea NOT NULL,
  member_no_bidx    bytea NOT NULL CHECK (length(member_no_bidx) = 32),
  member_no_last4   text,
  key_ref           text NOT NULL,
  scheme_name       text,
  valid_from        date NOT NULL,
  valid_to          date,
  status            text NOT NULL DEFAULT 'active' CHECK (status IN ('active','expired','suspended','cancelled')),
  last_verified_at  timestamptz,
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient (org_id, id),
  FOREIGN KEY (org_id, payer_id) REFERENCES billing.payer (org_id, id),
  CONSTRAINT ck_coverage_dates CHECK (valid_to IS NULL OR valid_to >= valid_from)
);
CREATE UNIQUE INDEX ux_coverage_member ON billing.coverage (payer_id, member_no_bidx) WHERE status = 'active';
CREATE INDEX ix_coverage_patient ON billing.coverage (patient_id);
SELECT sec.attach_standard_triggers('billing.coverage');
SELECT audit.attach_capture('billing.coverage', ARRAY['member_no_ciphertext','member_no_bidx']);

CREATE TABLE billing.service_catalog (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id       uuid NOT NULL REFERENCES org.organization(id),
  code         text NOT NULL CHECK (code ~ '^[A-Z0-9_-]{2,30}$'),
  name         text NOT NULL,
  category     text NOT NULL CHECK (category IN ('registration','consultation','laboratory','imaging','pharmacy',
                                                 'procedure','telemedicine','other')),
  diag_test_id uuid,
  drug_id      uuid,
  status       text NOT NULL DEFAULT 'active' CHECK (status IN ('active','retired')),
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  UNIQUE (org_id, code),
  FOREIGN KEY (org_id, diag_test_id) REFERENCES diag.test_catalog (org_id, id),
  FOREIGN KEY (org_id, drug_id) REFERENCES pharmacy.drug (org_id, id),
  CONSTRAINT ck_service_link CHECK (
    (category IN ('laboratory','imaging') AND diag_test_id IS NOT NULL AND drug_id IS NULL) OR
    (category = 'pharmacy' AND drug_id IS NOT NULL AND diag_test_id IS NULL) OR
    (category NOT IN ('laboratory','imaging','pharmacy') AND diag_test_id IS NULL AND drug_id IS NULL))
);
CREATE UNIQUE INDEX ux_service_test ON billing.service_catalog (diag_test_id) WHERE diag_test_id IS NOT NULL AND status = 'active';
CREATE UNIQUE INDEX ux_service_drug ON billing.service_catalog (drug_id) WHERE drug_id IS NOT NULL AND status = 'active';
SELECT sec.attach_standard_triggers('billing.service_catalog', false);

CREATE TABLE billing.price (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id      uuid NOT NULL,
  service_id  uuid NOT NULL,
  payer_id    uuid,                                -- NULL = default (cash) price
  unit_price  numeric(14,2) NOT NULL CHECK (unit_price >= 0),
  effective   tstzrange NOT NULL CHECK (NOT isempty(effective) AND lower(effective) IS NOT NULL),
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  FOREIGN KEY (org_id, service_id) REFERENCES billing.service_catalog (org_id, id),
  FOREIGN KEY (org_id, payer_id) REFERENCES billing.payer (org_id, id),
  CONSTRAINT ex_price_overlap EXCLUDE USING gist (
    service_id WITH =, (coalesce(payer_id, '00000000-0000-0000-0000-000000000000'::uuid)) WITH =, effective WITH &&)
);
SELECT audit.attach_capture('billing.price');

CREATE OR REPLACE FUNCTION billing.price_for(p_service uuid, p_payer uuid DEFAULT NULL, p_at timestamptz DEFAULT now())
RETURNS numeric LANGUAGE sql STABLE AS $$
  SELECT unit_price FROM billing.price
  WHERE service_id = p_service AND effective @> p_at AND (payer_id = p_payer OR payer_id IS NULL)
  ORDER BY (payer_id IS NULL)          -- payer-specific price wins over default
  LIMIT 1;
$$;

-- Cashier shifts (daily cash reconciliation, SOC 2 processing integrity).
CREATE TABLE billing.cashier_shift (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id         uuid NOT NULL,
  facility_id    uuid NOT NULL,
  membership_id  uuid NOT NULL,
  opened_at      timestamptz NOT NULL DEFAULT now(),
  opening_float  numeric(14,2) NOT NULL DEFAULT 0 CHECK (opening_float >= 0),
  closed_at      timestamptz,
  counted_cash   numeric(14,2) CHECK (counted_cash >= 0),
  expected_cash  numeric(14,2),
  variance       numeric(14,2) GENERATED ALWAYS AS (counted_cash - expected_cash) STORED,
  status         text NOT NULL DEFAULT 'open' CHECK (status IN ('open','closed','reconciled')),
  reconciled_by  uuid REFERENCES iam.user_account(id),
  reconciled_at  timestamptz,
  variance_note  text,
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  FOREIGN KEY (org_id, facility_id) REFERENCES org.facility (org_id, id),
  FOREIGN KEY (org_id, membership_id) REFERENCES iam.membership (org_id, id),
  CONSTRAINT ck_shift_closed CHECK (status = 'open' OR (closed_at IS NOT NULL AND counted_cash IS NOT NULL AND expected_cash IS NOT NULL)),
  CONSTRAINT ck_shift_reconciled CHECK ((status = 'reconciled') = (reconciled_at IS NOT NULL))
);
CREATE UNIQUE INDEX ux_shift_open ON billing.cashier_shift (membership_id) WHERE status = 'open';
SELECT sec.attach_standard_triggers('billing.cashier_shift');

CREATE OR REPLACE FUNCTION billing.shift_rules() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE v_user uuid;
BEGIN
  IF NEW.status = 'closed' AND OLD.status = 'open' THEN
    NEW.closed_at := now();
    SELECT NEW.opening_float + coalesce(sum(amount), 0) INTO NEW.expected_cash
      FROM billing.payment WHERE cashier_shift_id = NEW.id AND method = 'cash' AND status = 'completed';
  END IF;
  IF NEW.status = 'reconciled' AND OLD.status = 'closed' THEN
    SELECT user_id INTO v_user FROM iam.membership WHERE id = NEW.membership_id;
    IF NEW.reconciled_by IS NULL OR NEW.reconciled_by = v_user THEN
      RAISE EXCEPTION 'a shift must be reconciled by someone other than the cashier' USING ERRCODE = 'SPV01'; END IF;
    NEW.reconciled_at := now();
    IF NEW.counted_cash <> NEW.expected_cash AND NEW.variance_note IS NULL THEN
      RAISE EXCEPTION 'variance note required when counted cash differs' USING ERRCODE = 'SPV01'; END IF;
  END IF;
  RETURN NEW;
END $$;

-- Invoices -------------------------------------------------------------------
CREATE TABLE billing.invoice (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id         uuid NOT NULL,
  facility_id    uuid NOT NULL,
  patient_id     uuid NOT NULL,
  encounter_id   uuid,
  payer_id       uuid NOT NULL,
  invoice_no     text,
  status         text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','issued','partially_paid','paid','void')),
  currency_code  char(3) NOT NULL DEFAULT 'TZS',
  subtotal       numeric(14,2) NOT NULL DEFAULT 0 CHECK (subtotal >= 0),
  discount_total numeric(14,2) NOT NULL DEFAULT 0 CHECK (discount_total >= 0),
  total          numeric(14,2) NOT NULL DEFAULT 0 CHECK (total >= 0),
  amount_paid    numeric(14,2) NOT NULL DEFAULT 0 CHECK (amount_paid >= 0),
  balance        numeric(14,2) GENERATED ALWAYS AS (total - amount_paid) STORED,
  issued_at      timestamptz,
  issued_by      uuid REFERENCES iam.user_account(id),
  due_date       date,
  voided_at      timestamptz,
  voided_by      uuid REFERENCES iam.user_account(id),
  void_reason    text,
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  UNIQUE (org_id, invoice_no),
  FOREIGN KEY (org_id, facility_id) REFERENCES org.facility (org_id, id),
  FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient (org_id, id),
  FOREIGN KEY (org_id, encounter_id, patient_id) REFERENCES clinical.encounter (org_id, id, patient_id),
  FOREIGN KEY (org_id, payer_id) REFERENCES billing.payer (org_id, id),
  CONSTRAINT ck_inv_totals CHECK (total = subtotal - discount_total),
  CONSTRAINT ck_inv_paid CHECK (amount_paid <= total),
  CONSTRAINT ck_inv_issued CHECK (status = 'draft' OR (invoice_no IS NOT NULL AND issued_at IS NOT NULL) OR (status = 'void' AND invoice_no IS NULL)),
  CONSTRAINT ck_inv_status_paid CHECK (status <> 'paid' OR amount_paid = total),
  CONSTRAINT ck_inv_void CHECK ((status = 'void') = (voided_at IS NOT NULL) AND (status <> 'void' OR (void_reason IS NOT NULL AND amount_paid = 0)))
);
CREATE UNIQUE INDEX ux_invoice_draft ON billing.invoice (encounter_id, payer_id) WHERE status = 'draft' AND encounter_id IS NOT NULL;
CREATE INDEX ix_invoice_patient ON billing.invoice (patient_id, created_at DESC);
CREATE INDEX ix_invoice_open ON billing.invoice (org_id, facility_id, issued_at) WHERE status IN ('issued','partially_paid');
CREATE INDEX ix_invoice_issued_day ON billing.invoice (org_id, issued_at) WHERE status <> 'draft';
SELECT sec.attach_standard_triggers('billing.invoice');
SELECT audit.attach_capture('billing.invoice');

CREATE OR REPLACE FUNCTION billing.invoice_rules() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF (NEW.subtotal, NEW.discount_total, NEW.total, NEW.amount_paid) IS DISTINCT FROM (0::numeric, 0::numeric, 0::numeric, 0::numeric)
       AND NOT sec.is_internal() THEN
      RAISE EXCEPTION 'invoice totals are derived from lines' USING ERRCODE = 'SPI01'; END IF;
    RETURN NEW;
  END IF;
  IF NOT sec.is_internal() AND (NEW.subtotal, NEW.discount_total, NEW.total, NEW.amount_paid, NEW.invoice_no)
       IS DISTINCT FROM (OLD.subtotal, OLD.discount_total, OLD.total, OLD.amount_paid, OLD.invoice_no) THEN
    RAISE EXCEPTION 'invoice totals and number are system-maintained' USING ERRCODE = 'SPI01'; END IF;
  IF OLD.status <> 'draft' AND NOT sec.is_internal()
     AND (NEW.patient_id, NEW.encounter_id, NEW.payer_id, NEW.facility_id) IS DISTINCT FROM
         (OLD.patient_id, OLD.encounter_id, OLD.payer_id, OLD.facility_id) THEN
    RAISE EXCEPTION 'issued invoices cannot be re-pointed' USING ERRCODE = 'SPI01'; END IF;
  -- draft -> issued: number allocation (gapless, per org per year)
  IF OLD.status = 'draft' AND NEW.status = 'issued' THEN
    IF NOT EXISTS (SELECT 1 FROM billing.invoice_line WHERE invoice_id = NEW.id AND NOT voided) THEN
      RAISE EXCEPTION 'cannot issue an empty invoice' USING ERRCODE = 'SPB01'; END IF;
    NEW.invoice_no := 'INV-' || to_char(org.local_date(NEW.org_id), 'YYYY') || '-' ||
                      lpad(org.next_value(NEW.org_id, 'invoice', to_char(org.local_date(NEW.org_id), 'YYYY'))::text, 6, '0');
    NEW.issued_at := now();
    NEW.issued_by := coalesce(NEW.issued_by, sec.current_user_id());
    IF NEW.total = 0 THEN NEW.status := 'paid'; END IF;   -- fully discounted / zero-rated visit
  END IF;
  IF NEW.status = 'void' AND OLD.status <> 'void' THEN
    IF OLD.amount_paid > 0 THEN
      RAISE EXCEPTION 'reverse or refund payments before voiding' USING ERRCODE = 'SPB01'; END IF;
    NEW.voided_at := now();
    NEW.voided_by := coalesce(NEW.voided_by, sec.current_user_id());
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_invoice_rules BEFORE INSERT OR UPDATE ON billing.invoice FOR EACH ROW EXECUTE FUNCTION billing.invoice_rules();

CREATE TABLE billing.invoice_line (
  id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id               uuid NOT NULL,
  invoice_id           uuid NOT NULL,
  service_id           uuid NOT NULL,
  source_type          text NOT NULL CHECK (source_type IN ('registration','consultation','diagnostic','pharmacy',
                                                            'procedure','telemedicine','manual')),
  consultation_id      uuid,
  diag_request_item_id uuid,
  dispense_item_id     uuid,
  description          text NOT NULL,
  quantity             numeric(10,2) NOT NULL CHECK (quantity > 0),
  unit_price           numeric(14,2) NOT NULL CHECK (unit_price >= 0),
  discount_amount      numeric(14,2) NOT NULL DEFAULT 0 CHECK (discount_amount >= 0),
  discount_reason      text,
  line_total           numeric(14,2) GENERATED ALWAYS AS (round(quantity * unit_price, 2) - discount_amount) STORED,
  voided               boolean NOT NULL DEFAULT false,
  created_at           timestamptz NOT NULL DEFAULT now(),
  created_by           uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  UNIQUE (org_id, id),
  FOREIGN KEY (org_id, invoice_id) REFERENCES billing.invoice (org_id, id),
  FOREIGN KEY (org_id, service_id) REFERENCES billing.service_catalog (org_id, id),
  FOREIGN KEY (org_id, consultation_id) REFERENCES clinical.consultation (org_id, id),
  FOREIGN KEY (org_id, diag_request_item_id) REFERENCES diag.request_item (org_id, id),
  FOREIGN KEY (org_id, dispense_item_id) REFERENCES pharmacy.dispense_item (org_id, id),
  CONSTRAINT ck_line_source CHECK (
    (source_type = 'diagnostic'   AND diag_request_item_id IS NOT NULL AND num_nonnulls(consultation_id, dispense_item_id) = 0) OR
    (source_type = 'pharmacy'     AND dispense_item_id IS NOT NULL AND num_nonnulls(consultation_id, diag_request_item_id) = 0) OR
    (source_type = 'consultation' AND num_nonnulls(diag_request_item_id, dispense_item_id) = 0) OR
    (source_type IN ('registration','procedure','telemedicine','manual') AND num_nonnulls(consultation_id, diag_request_item_id, dispense_item_id) = 0)),
  CONSTRAINT ck_line_discount CHECK (discount_amount <= round(quantity * unit_price, 2)),
  CONSTRAINT ck_line_discount_reason CHECK (discount_amount = 0 OR discount_reason IS NOT NULL)
);
-- A clinical activity is billed at most once.
CREATE UNIQUE INDEX ux_line_diag     ON billing.invoice_line (diag_request_item_id) WHERE diag_request_item_id IS NOT NULL AND NOT voided;
CREATE UNIQUE INDEX ux_line_dispense ON billing.invoice_line (dispense_item_id)     WHERE dispense_item_id IS NOT NULL AND NOT voided;
CREATE UNIQUE INDEX ux_line_consult  ON billing.invoice_line (consultation_id)      WHERE consultation_id IS NOT NULL AND NOT voided;
CREATE INDEX ix_line_invoice ON billing.invoice_line (invoice_id);
SELECT audit.attach_capture('billing.invoice_line');

-- Resolves the patient of a billed clinical item regardless of whether the
-- cashier may read clinical tables (cashiers must not read clinical notes).
CREATE OR REPLACE FUNCTION billing.source_patient(p_consultation uuid, p_diag_item uuid, p_dispense_item uuid)
RETURNS uuid LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog AS $$
  SELECT CASE
    WHEN p_diag_item IS NOT NULL THEN (SELECT patient_id FROM diag.request_item WHERE id = p_diag_item)
    WHEN p_dispense_item IS NOT NULL THEN (SELECT d.patient_id FROM pharmacy.dispense_item di
                                           JOIN pharmacy.dispense d ON d.id = di.dispense_id WHERE di.id = p_dispense_item)
    WHEN p_consultation IS NOT NULL THEN (SELECT patient_id FROM clinical.consultation WHERE id = p_consultation)
  END;
$$;

CREATE OR REPLACE FUNCTION billing.line_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE v_inv billing.invoice; v_src_patient uuid; v_row billing.invoice_line;
BEGIN
  v_row := coalesce(NEW, OLD);
  SELECT * INTO v_inv FROM billing.invoice WHERE id = v_row.invoice_id;
  IF sec.is_internal() THEN RETURN coalesce(NEW, OLD); END IF;
  IF v_inv.status <> 'draft' THEN
    RAISE EXCEPTION 'invoice % is %; lines can only change while draft', v_inv.id, v_inv.status USING ERRCODE = 'SPB01'; END IF;
  IF TG_OP <> 'DELETE' THEN
    -- the billed activity must belong to the invoice's patient
    v_src_patient := billing.source_patient(NEW.consultation_id, NEW.diag_request_item_id, NEW.dispense_item_id);
    IF v_src_patient IS NOT NULL AND v_src_patient <> v_inv.patient_id THEN
      RAISE EXCEPTION 'billed item belongs to a different patient' USING ERRCODE = 'SPX01'; END IF;
  END IF;
  RETURN coalesce(NEW, OLD);
END $$;
CREATE TRIGGER trg_line_guard BEFORE INSERT OR UPDATE OR DELETE ON billing.invoice_line FOR EACH ROW EXECUTE FUNCTION billing.line_guard();

CREATE OR REPLACE FUNCTION billing.recalc_invoice(p_invoice uuid) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE v_sub numeric(14,2); v_disc numeric(14,2); v_paid numeric(14,2); v_status text; v_new_status text;
BEGIN
  SELECT status INTO v_status FROM billing.invoice WHERE id = p_invoice FOR UPDATE;
  SELECT coalesce(sum(round(quantity * unit_price, 2)), 0), coalesce(sum(discount_amount), 0)
    INTO v_sub, v_disc FROM billing.invoice_line WHERE invoice_id = p_invoice AND NOT voided;
  SELECT coalesce(sum(a.amount), 0) INTO v_paid
    FROM billing.payment_allocation a JOIN billing.payment p ON p.id = a.payment_id
   WHERE a.invoice_id = p_invoice AND p.status = 'completed';
  v_new_status := CASE WHEN v_status IN ('draft','void') THEN v_status
                       WHEN v_paid = 0 AND (v_sub - v_disc) > 0 THEN 'issued'
                       WHEN v_paid < v_sub - v_disc THEN 'partially_paid'
                       ELSE 'paid' END;
  UPDATE billing.invoice SET subtotal = v_sub, discount_total = v_disc, total = v_sub - v_disc,
                             amount_paid = v_paid, status = v_new_status
   WHERE id = p_invoice
     AND (subtotal, discount_total, amount_paid, status) IS DISTINCT FROM (v_sub, v_disc, v_paid, v_new_status);
END $$;

CREATE OR REPLACE FUNCTION billing.on_line_change() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
BEGIN PERFORM billing.recalc_invoice(coalesce(NEW.invoice_id, OLD.invoice_id)); RETURN NULL; END $$;
CREATE TRIGGER trg_line_recalc AFTER INSERT OR UPDATE OR DELETE ON billing.invoice_line FOR EACH ROW EXECUTE FUNCTION billing.on_line_change();

CREATE OR REPLACE FUNCTION billing.on_invoice_void() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
BEGIN
  IF NEW.status = 'void' AND OLD.status <> 'void' THEN
    UPDATE billing.invoice_line SET voided = true WHERE invoice_id = NEW.id AND NOT voided;  -- frees items for re-billing
  END IF;
  RETURN NULL;
END $$;
CREATE TRIGGER trg_invoice_void AFTER UPDATE OF status ON billing.invoice FOR EACH ROW EXECUTE FUNCTION billing.on_invoice_void();

-- Payments -------------------------------------------------------------------
CREATE TABLE billing.payment (
  id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id             uuid NOT NULL,
  facility_id        uuid NOT NULL,
  patient_id         uuid,                     -- NULL for insurer bulk remittance
  payer_id           uuid,
  method             text NOT NULL CHECK (method IN ('cash','mobile_money','card','bank_transfer','insurance')),
  provider           text CHECK (provider IN ('mpesa','airtel_money','mixx_by_yas','halopesa','selcom','azampay','bank','nhif','other')),
  provider_reference text,
  amount             numeric(14,2) NOT NULL CHECK (amount > 0),
  currency_code      char(3) NOT NULL DEFAULT 'TZS',
  status             text NOT NULL CHECK (status IN ('pending','completed','failed','cancelled','reversed')),
  idempotency_key    uuid NOT NULL,
  cashier_shift_id   uuid,
  received_by_membership_id uuid,
  received_at        timestamptz,
  failure_reason     text,
  reversed_at        timestamptz,
  reversal_reason    text,
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  UNIQUE (org_id, idempotency_key),
  FOREIGN KEY (org_id, facility_id) REFERENCES org.facility (org_id, id),
  FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient (org_id, id),
  FOREIGN KEY (org_id, payer_id) REFERENCES billing.payer (org_id, id),
  FOREIGN KEY (org_id, cashier_shift_id) REFERENCES billing.cashier_shift (org_id, id),
  FOREIGN KEY (org_id, received_by_membership_id) REFERENCES iam.membership (org_id, id),
  CONSTRAINT ck_pay_cash_shift CHECK (method <> 'cash' OR cashier_shift_id IS NOT NULL),
  CONSTRAINT ck_pay_electronic_ref CHECK (method IN ('cash','insurance') OR provider IS NOT NULL),
  CONSTRAINT ck_pay_completed CHECK (status NOT IN ('completed','reversed') OR received_at IS NOT NULL),
  CONSTRAINT ck_pay_completed_ref CHECK (status <> 'completed' OR method = 'cash' OR provider_reference IS NOT NULL),
  CONSTRAINT ck_pay_failed CHECK (status <> 'failed' OR failure_reason IS NOT NULL),
  CONSTRAINT ck_pay_reversed CHECK ((status = 'reversed') = (reversed_at IS NOT NULL) AND (status <> 'reversed' OR reversal_reason IS NOT NULL)),
  CONSTRAINT ck_pay_party CHECK (num_nonnulls(patient_id, payer_id) >= 1)
);
CREATE UNIQUE INDEX ux_payment_provider_ref ON billing.payment (org_id, provider, provider_reference) WHERE provider_reference IS NOT NULL;
CREATE INDEX ix_payment_day ON billing.payment (org_id, facility_id, received_at) WHERE status = 'completed';
CREATE INDEX ix_payment_pending ON billing.payment (created_at) WHERE status = 'pending';
CREATE INDEX ix_payment_shift ON billing.payment (cashier_shift_id) WHERE cashier_shift_id IS NOT NULL;
SELECT sec.attach_standard_triggers('billing.payment');
SELECT audit.attach_capture('billing.payment');

CREATE OR REPLACE FUNCTION billing.payment_rules() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE v_shift billing.cashier_shift;
BEGIN
  IF TG_OP = 'UPDATE' AND NOT sec.is_internal() AND
     (NEW.amount, NEW.method, NEW.patient_id, NEW.payer_id, NEW.idempotency_key, NEW.cashier_shift_id)
     IS DISTINCT FROM (OLD.amount, OLD.method, OLD.patient_id, OLD.payer_id, OLD.idempotency_key, OLD.cashier_shift_id) THEN
    RAISE EXCEPTION 'payment financial fields are immutable' USING ERRCODE = 'SPI01'; END IF;
  IF NEW.cashier_shift_id IS NOT NULL AND (TG_OP = 'INSERT') THEN
    SELECT * INTO v_shift FROM billing.cashier_shift WHERE id = NEW.cashier_shift_id;
    IF v_shift.status <> 'open' THEN RAISE EXCEPTION 'cashier shift is not open' USING ERRCODE = 'SPB01'; END IF;
    IF v_shift.membership_id IS DISTINCT FROM NEW.received_by_membership_id THEN
      RAISE EXCEPTION 'cash must be received into the receiving cashier''s own shift' USING ERRCODE = 'SPB01'; END IF;
  END IF;
  IF NEW.status = 'completed' AND (TG_OP = 'INSERT' OR OLD.status <> 'completed') THEN
    NEW.received_at := coalesce(NEW.received_at, now()); END IF;
  IF NEW.status = 'reversed' AND OLD.status <> 'reversed' THEN NEW.reversed_at := now(); END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_payment_rules BEFORE INSERT OR UPDATE ON billing.payment FOR EACH ROW EXECUTE FUNCTION billing.payment_rules();

CREATE TABLE billing.payment_allocation (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id       uuid NOT NULL,
  payment_id   uuid NOT NULL,
  invoice_id   uuid NOT NULL,
  amount       numeric(14,2) NOT NULL CHECK (amount > 0),
  allocated_at timestamptz NOT NULL DEFAULT now(),
  allocated_by uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  FOREIGN KEY (org_id, payment_id) REFERENCES billing.payment (org_id, id),
  FOREIGN KEY (org_id, invoice_id) REFERENCES billing.invoice (org_id, id),
  UNIQUE (payment_id, invoice_id)
);
CREATE INDEX ix_alloc_invoice ON billing.payment_allocation (invoice_id);
CREATE TRIGGER trg_alloc_append_only BEFORE UPDATE OR DELETE ON billing.payment_allocation FOR EACH ROW EXECUTE FUNCTION sec.forbid_mutation();

CREATE OR REPLACE FUNCTION billing.allocation_rules() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE v_pay billing.payment; v_inv billing.invoice; v_allocated numeric(14,2);
BEGIN
  SELECT * INTO v_pay FROM billing.payment WHERE id = NEW.payment_id FOR UPDATE;   -- serialise per payment
  SELECT * INTO v_inv FROM billing.invoice WHERE id = NEW.invoice_id FOR UPDATE;   -- serialise per invoice
  IF v_pay.status <> 'completed' THEN
    RAISE EXCEPTION 'only completed payments can be allocated' USING ERRCODE = 'SPB01'; END IF;
  IF v_inv.status NOT IN ('issued','partially_paid') THEN
    RAISE EXCEPTION 'invoice % is % and cannot accept payment', v_inv.invoice_no, v_inv.status USING ERRCODE = 'SPB01'; END IF;
  IF v_pay.patient_id IS NOT NULL AND v_pay.patient_id <> v_inv.patient_id THEN
    RAISE EXCEPTION 'payment and invoice belong to different patients' USING ERRCODE = 'SPX01'; END IF;
  IF NEW.amount > v_inv.balance THEN
    RAISE EXCEPTION 'allocation % exceeds invoice balance %', NEW.amount, v_inv.balance USING ERRCODE = 'SPB01'; END IF;
  SELECT coalesce(sum(amount), 0) INTO v_allocated FROM billing.payment_allocation WHERE payment_id = NEW.payment_id;
  IF v_allocated + NEW.amount > v_pay.amount THEN
    RAISE EXCEPTION 'allocations exceed payment amount' USING ERRCODE = 'SPB01'; END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_alloc_rules BEFORE INSERT ON billing.payment_allocation FOR EACH ROW EXECUTE FUNCTION billing.allocation_rules();

CREATE OR REPLACE FUNCTION billing.on_allocation() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
BEGIN PERFORM billing.recalc_invoice(NEW.invoice_id); RETURN NULL; END $$;
CREATE TRIGGER trg_alloc_recalc AFTER INSERT ON billing.payment_allocation FOR EACH ROW EXECUTE FUNCTION billing.on_allocation();

-- Receipts: exactly one per completed payment, gapless numbering.
CREATE TABLE billing.receipt (
  id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id             uuid NOT NULL,
  payment_id         uuid NOT NULL UNIQUE,
  receipt_no         text NOT NULL,
  issued_at          timestamptz NOT NULL DEFAULT now(),
  issued_by          uuid REFERENCES iam.user_account(id),
  print_count        integer NOT NULL DEFAULT 0 CHECK (print_count >= 0),
  fiscal_receipt_ref text,            -- TRA VFD/EFD reference when integrated (pending decision)
  UNIQUE (org_id, receipt_no),
  FOREIGN KEY (org_id, payment_id) REFERENCES billing.payment (org_id, id)
);
CREATE OR REPLACE FUNCTION billing.receipt_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF (to_jsonb(NEW) - ARRAY['print_count','fiscal_receipt_ref']) IS DISTINCT FROM (to_jsonb(OLD) - ARRAY['print_count','fiscal_receipt_ref']) THEN
    RAISE EXCEPTION 'receipts are immutable' USING ERRCODE = 'SPI01'; END IF;
  IF NEW.print_count < OLD.print_count THEN RAISE EXCEPTION 'print_count cannot decrease' USING ERRCODE = 'SPI01'; END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_receipt_guard BEFORE UPDATE ON billing.receipt FOR EACH ROW EXECUTE FUNCTION billing.receipt_guard();
CREATE TRIGGER trg_receipt_no_delete BEFORE DELETE ON billing.receipt FOR EACH ROW EXECUTE FUNCTION sec.forbid_mutation();

CREATE OR REPLACE FUNCTION billing.on_payment_status() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE r record;
BEGIN
  IF NEW.status = 'completed' AND (TG_OP = 'INSERT' OR OLD.status <> 'completed') THEN
    INSERT INTO billing.receipt (org_id, payment_id, receipt_no, issued_by)
    VALUES (NEW.org_id, NEW.id,
            'RCT-' || to_char(org.local_date(NEW.org_id), 'YYYY') || '-' ||
            lpad(org.next_value(NEW.org_id, 'receipt', to_char(org.local_date(NEW.org_id), 'YYYY'))::text, 7, '0'),
            sec.current_user_id())
    ON CONFLICT (payment_id) DO NOTHING;
  END IF;
  IF TG_OP = 'UPDATE' AND NEW.status IS DISTINCT FROM OLD.status THEN
    FOR r IN SELECT DISTINCT invoice_id FROM billing.payment_allocation WHERE payment_id = NEW.id LOOP
      PERFORM billing.recalc_invoice(r.invoice_id);
    END LOOP;
  END IF;
  RETURN NULL;
END $$;
CREATE TRIGGER trg_payment_status AFTER INSERT OR UPDATE OF status ON billing.payment FOR EACH ROW EXECUTE FUNCTION billing.on_payment_status();

CREATE TRIGGER trg_shift_rules BEFORE UPDATE ON billing.cashier_shift FOR EACH ROW EXECUTE FUNCTION billing.shift_rules();

-- Refunds (four-eyes) ----------------------------------------------------------
CREATE TABLE billing.refund (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id       uuid NOT NULL,
  payment_id   uuid NOT NULL,
  amount       numeric(14,2) NOT NULL CHECK (amount > 0),
  reason       text NOT NULL CHECK (length(btrim(reason)) >= 10),
  status       text NOT NULL DEFAULT 'requested' CHECK (status IN ('requested','approved','rejected','paid')),
  requested_by uuid NOT NULL DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  approved_by  uuid REFERENCES iam.user_account(id),
  decided_at   timestamptz,
  paid_at      timestamptz,
  payout_reference text,
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  FOREIGN KEY (org_id, payment_id) REFERENCES billing.payment (org_id, id),
  CONSTRAINT ck_refund_four_eyes CHECK (approved_by IS NULL OR approved_by <> requested_by),
  CONSTRAINT ck_refund_decided CHECK (status = 'requested' OR (approved_by IS NOT NULL AND decided_at IS NOT NULL)),
  CONSTRAINT ck_refund_paid CHECK ((status = 'paid') = (paid_at IS NOT NULL))
);
SELECT sec.attach_standard_triggers('billing.refund');
SELECT audit.attach_capture('billing.refund');

CREATE OR REPLACE FUNCTION billing.refund_rules() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE v_pay billing.payment; v_refunded numeric;
BEGIN
  SELECT * INTO v_pay FROM billing.payment WHERE id = NEW.payment_id FOR UPDATE;
  IF v_pay.status <> 'completed' THEN RAISE EXCEPTION 'only completed payments can be refunded' USING ERRCODE = 'SPB01'; END IF;
  SELECT coalesce(sum(amount), 0) INTO v_refunded FROM billing.refund
   WHERE payment_id = NEW.payment_id AND status <> 'rejected' AND id <> NEW.id;
  IF v_refunded + NEW.amount > v_pay.amount THEN
    RAISE EXCEPTION 'refunds exceed payment amount' USING ERRCODE = 'SPB01'; END IF;
  IF TG_OP = 'UPDATE' AND NEW.status IN ('approved','rejected') AND OLD.status = 'requested' THEN NEW.decided_at := now(); END IF;
  IF TG_OP = 'UPDATE' AND NEW.status = 'paid' AND OLD.status = 'approved' THEN NEW.paid_at := now(); END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_refund_rules BEFORE INSERT OR UPDATE ON billing.refund FOR EACH ROW EXECUTE FUNCTION billing.refund_rules();
