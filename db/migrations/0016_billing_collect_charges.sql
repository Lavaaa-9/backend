-- 0016: let billing staff pull a visit's unbilled charges onto a draft invoice.
-- Cashiers cannot read clinical tables (by design), so the join across procedures,
-- dispensing and diagnostics runs inside a SECURITY DEFINER function that still
-- requires billing.invoice.manage, stays within the caller's tenant, and only
-- touches draft invoices. Each source is billed once (unique indexes) and items
-- whose service has no price for the invoice's payer are skipped.
CREATE OR REPLACE FUNCTION billing.collect_charges(p_invoice uuid) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE v_inv billing.invoice; v_n integer := 0; v_c integer;
BEGIN
  IF NOT sec.has_perm('billing.invoice.manage') THEN
    RAISE EXCEPTION 'permission billing.invoice.manage required' USING ERRCODE = '42501'; END IF;
  SELECT * INTO v_inv FROM billing.invoice WHERE id = p_invoice AND org_id = sec.current_org_id() FOR UPDATE;
  IF NOT FOUND THEN RAISE EXCEPTION 'invoice not found' USING ERRCODE = 'SPV01'; END IF;
  IF v_inv.status <> 'draft' THEN
    RAISE EXCEPTION 'charges can only be added while the invoice is a draft' USING ERRCODE = 'SPB01'; END IF;
  IF v_inv.encounter_id IS NULL THEN
    RAISE EXCEPTION 'this invoice is not linked to a visit' USING ERRCODE = 'SPV01'; END IF;

  INSERT INTO billing.invoice_line (org_id, invoice_id, service_id, source_type, procedure_id, description, quantity, unit_price)
  SELECT v_inv.org_id, v_inv.id, p.service_id, 'procedure', p.id, p.description, p.quantity, billing.price_for(p.service_id, v_inv.payer_id)
  FROM clinical.procedure p
  WHERE p.encounter_id = v_inv.encounter_id AND p.status = 'performed' AND p.service_id IS NOT NULL
    AND billing.price_for(p.service_id, v_inv.payer_id) IS NOT NULL
    AND NOT EXISTS (SELECT 1 FROM billing.invoice_line l WHERE l.procedure_id = p.id AND NOT l.voided);
  GET DIAGNOSTICS v_c = ROW_COUNT; v_n := v_n + v_c;

  INSERT INTO billing.invoice_line (org_id, invoice_id, service_id, source_type, dispense_item_id, description, quantity, unit_price)
  SELECT v_inv.org_id, v_inv.id, sc.id, 'pharmacy', di.id, d.generic_name || ' ' || d.strength, di.quantity, billing.price_for(sc.id, v_inv.payer_id)
  FROM pharmacy.dispense_item di
  JOIN pharmacy.dispense x ON x.id = di.dispense_id AND x.status = 'completed'
  JOIN pharmacy.prescription p ON p.id = x.prescription_id AND p.encounter_id = v_inv.encounter_id
  JOIN pharmacy.prescription_item pi ON pi.id = di.prescription_item_id
  JOIN pharmacy.drug d ON d.id = pi.drug_id
  JOIN billing.service_catalog sc ON sc.drug_id = d.id AND sc.status = 'active'
  WHERE billing.price_for(sc.id, v_inv.payer_id) IS NOT NULL
    AND NOT EXISTS (SELECT 1 FROM billing.invoice_line l WHERE l.dispense_item_id = di.id AND NOT l.voided);
  GET DIAGNOSTICS v_c = ROW_COUNT; v_n := v_n + v_c;

  INSERT INTO billing.invoice_line (org_id, invoice_id, service_id, source_type, diag_request_item_id, description, quantity, unit_price)
  SELECT v_inv.org_id, v_inv.id, sc.id, 'diagnostic', i.id, t.name, 1, billing.price_for(sc.id, v_inv.payer_id)
  FROM diag.request_item i
  JOIN diag.request r ON r.id = i.request_id AND r.encounter_id = v_inv.encounter_id
  JOIN diag.test_catalog t ON t.id = i.test_id
  JOIN billing.service_catalog sc ON sc.diag_test_id = t.id AND sc.status = 'active'
  WHERE i.status NOT IN ('cancelled', 'rejected') AND billing.price_for(sc.id, v_inv.payer_id) IS NOT NULL
    AND NOT EXISTS (SELECT 1 FROM billing.invoice_line l WHERE l.diag_request_item_id = i.id AND NOT l.voided);
  GET DIAGNOSTICS v_c = ROW_COUNT; v_n := v_n + v_c;
  RETURN v_n;
END $$;
GRANT EXECUTE ON FUNCTION billing.collect_charges(uuid) TO spartial_app;
