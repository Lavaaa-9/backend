"""Generates migrations/0011_reference_seed.sql (state machines, permissions, system roles, ICD-10 demo subset)."""
import pathlib
T = {
 'iam.membership': {'_init':['invited','active'],'invited':['active','ended'],'active':['suspended','ended'],'suspended':['active','ended']},
 'iam.access_review': {'_init':['open'],'open':['completed','cancelled']},
 'patient.patient': {'_init':['active'],'active':['inactive','deceased','merged'],'inactive':['active','deceased','merged']},
 'patient.contact_point': {'_init':['active'],'active':['inactive'],'inactive':['active']},
 'patient.identifier': {'_init':['active'],'active':['inactive','entered_in_error'],'inactive':['active']},
 'patient.next_of_kin': {'_init':['active'],'active':['inactive'],'inactive':['active']},
 'patient.consent': {'_init':['granted'],'granted':['withdrawn']},
 'patient.portal_account': {'_init':['pending_verification','active'],'pending_verification':['active','revoked'],'active':['suspended','revoked'],'suspended':['active','revoked']},
 'patient.duplicate_review': {'_init':['open'],'open':['confirmed_distinct','confirmed_duplicate'],'confirmed_duplicate':['merged','confirmed_distinct']},
 'clinical.allergy': {'_init':['active'],'active':['inactive','resolved','entered_in_error'],'inactive':['active','entered_in_error'],'resolved':['active']},
 'clinical.problem': {'_init':['active'],'active':['inactive','resolved','entered_in_error'],'inactive':['active','resolved'],'resolved':['active']},
 'clinical.history_entry': {'_init':['active'],'active':['entered_in_error']},
 'clinical.appointment': {'_init':['requested','booked'],'requested':['booked','rejected','cancelled'],'booked':['confirmed','checked_in','cancelled','no_show'],'confirmed':['checked_in','cancelled','no_show'],'checked_in':['completed']},
 'clinical.encounter': {'_init':['registered'],'registered':['in_progress','cancelled','entered_in_error'],'in_progress':['completed','entered_in_error']},
 'clinical.queue_ticket': {'_init':['waiting'],'waiting':['called','in_service','cancelled','transferred'],'called':['in_service','waiting','skipped','cancelled'],'skipped':['waiting','cancelled'],'in_service':['completed','transferred']},
 'clinical.triage_assessment': {'_init':['draft','final'],'draft':['final','entered_in_error'],'final':['amended','entered_in_error']},
 'clinical.consultation': {'_init':['draft'],'draft':['signed','entered_in_error'],'signed':['amended','entered_in_error']},
 'clinical.encounter_diagnosis': {'_init':['active'],'active':['entered_in_error']},
 'clinical.follow_up_plan': {'_init':['planned'],'planned':['booked','cancelled','missed'],'booked':['completed','cancelled','missed','planned']},
 'document.document': {'_init':['pending_upload'],'pending_upload':['scanning','deleted'],'scanning':['available','quarantined'],'available':['deleted'],'quarantined':['deleted']},
 'diag.request': {'_init':['requested'],'requested':['in_progress','partially_resulted','completed','cancelled'],'in_progress':['partially_resulted','completed','cancelled'],'partially_resulted':['completed']},
 'diag.request_item': {'_init':['requested'],'requested':['specimen_collected','in_progress','cancelled','rejected'],'specimen_collected':['in_progress','resulted','verified','rejected'],'in_progress':['resulted','verified'],'resulted':['verified'],'rejected':['requested']},
 'diag.specimen': {'_init':['collected'],'collected':['received','rejected'],'received':['consumed','rejected']},
 'diag.result': {'_init':['preliminary','final'],'preliminary':['final','entered_in_error'],'final':['corrected','entered_in_error']},
 'pharmacy.stock_batch': {'_init':['active','quarantined'],'active':['quarantined','expired','depleted'],'depleted':['active'],'quarantined':['active','expired']},
 'pharmacy.prescription': {'_init':['draft'],'draft':['active','cancelled'],'active':['partially_dispensed','dispensed','cancelled','expired'],'partially_dispensed':['dispensed','active','expired','cancelled'],'dispensed':['partially_dispensed','active']},
 'pharmacy.prescription_item': {'_init':['pending'],'pending':['partially_dispensed','dispensed','cancelled'],'partially_dispensed':['dispensed','pending','cancelled'],'dispensed':['partially_dispensed','pending']},
 'pharmacy.dispense': {'_init':['completed'],'completed':['reversed']},
 'billing.coverage': {'_init':['active'],'active':['expired','suspended','cancelled'],'suspended':['active','cancelled']},
 'billing.cashier_shift': {'_init':['open'],'open':['closed'],'closed':['reconciled']},
 'billing.invoice': {'_init':['draft'],'draft':['issued','paid','void'],'issued':['partially_paid','paid','void'],'partially_paid':['paid','issued'],'paid':['partially_paid','issued']},
 'billing.payment': {'_init':['pending','completed'],'pending':['completed','failed','cancelled'],'completed':['reversed']},
 'billing.refund': {'_init':['requested'],'requested':['approved','rejected'],'approved':['paid']},
 'telehealth.session': {'_init':['scheduled'],'scheduled':['waiting_room','in_progress','cancelled'],'waiting_room':['in_progress','no_show','cancelled','failed'],'in_progress':['completed','failed'],'failed':['scheduled']},
 'comms.notification_template': {'_init':['draft'],'draft':['active'],'active':['retired']},
 'comms.message': {'_init':['queued'],'queued':['sending','cancelled'],'sending':['sent','failed'],'sent':['delivered','failed'],'failed':['queued']},
 'gov.privacy_request': {'_init':['received'],'received':['identity_verified','withdrawn','denied'],'identity_verified':['in_progress','denied','withdrawn'],'in_progress':['fulfilled','partially_fulfilled','denied']},
}
# transitions that need a specific permission beyond table write access
TP = {
 ('billing.payment','completed','reversed'):'billing.payment.reverse',
 ('billing.invoice','issued','void'):'billing.invoice.void', ('billing.invoice','draft','void'):'billing.invoice.void',
 ('billing.refund','requested','approved'):'billing.refund.approve', ('billing.refund','requested','rejected'):'billing.refund.approve',
 ('billing.cashier_shift','closed','reconciled'):'billing.shift.reconcile',
 ('diag.result','preliminary','final'):'diag.result.verify', ('diag.result','_init','final'):'diag.result.verify',
 ('diag.result','final','corrected'):'diag.result.verify',
 ('patient.patient','active','merged'):'patient.merge', ('patient.patient','inactive','merged'):'patient.merge',
 ('patient.duplicate_review','confirmed_duplicate','merged'):'patient.merge',
 ('document.document','available','deleted'):'document.delete', ('document.document','quarantined','deleted'):'document.delete',
 ('pharmacy.prescription','draft','active'):'pharmacy.prescribe',
 ('clinical.consultation','draft','signed'):'consultation.write',
}
# code: (description, is_phi, is_privileged)
P = {
 'patient.read':('View patient demographics and contacts',1,0),'patient.create':('Register patients',1,0),
 'patient.update':('Edit patient demographics, contacts, consent',1,0),'patient.merge':('Merge duplicate patient records',1,1),
 'patient.export':('Export patient records (privacy requests)',1,1),'patient.identifier.reveal':('Decrypt national/insurance identifiers',1,1),
 'document.read':('View/download patient documents',1,0),'document.upload':('Upload patient documents',1,0),'document.delete':('Delete patient documents',1,1),
 'appointment.read':('View appointments',1,0),'appointment.manage':('Book, reschedule, cancel appointments',1,0),
 'encounter.read':('View visit list/status',1,0),'encounter.manage':('Check in, start, close visits',1,0),
 'queue.read':('View queues',1,0),'queue.manage':('Call, serve, transfer queue tickets',1,0),
 'clinical.read':('Read clinical records (triage, notes, diagnoses, history)',1,0),
 'clinical.history.write':('Record medical history and allergies',1,0),'triage.write':('Record triage and vital signs',1,0),
 'consultation.write':('Write consultation notes and diagnoses',1,0),
 'diag.request.create':('Order laboratory/diagnostic tests',1,0),'diag.request.read':('View diagnostic requests',1,0),
 'diag.specimen.manage':('Collect/receive/reject specimens',1,0),'diag.result.enter':('Enter results',1,0),
 'diag.result.verify':('Verify/release results',1,0),'diag.result.read':('Read results',1,0),'diag.catalog.manage':('Manage test catalogue',0,0),
 'pharmacy.prescribe':('Write and sign prescriptions',1,0),'pharmacy.read':('View prescriptions and stock',1,0),
 'pharmacy.dispense':('Dispense and reverse dispensing',1,0),'pharmacy.stock.manage':('Receive/adjust stock',0,0),'pharmacy.catalog.manage':('Manage drug catalogue',0,0),
 'billing.read':('View invoices and payments',1,0),'billing.invoice.manage':('Create/edit/issue invoices',1,0),
 'billing.invoice.void':('Void invoices',1,1),'billing.payment.record':('Record payments',1,0),'billing.payment.reverse':('Reverse payments',1,1),
 'billing.refund.request':('Request refunds',1,0),'billing.refund.approve':('Approve refunds',1,1),'billing.price.manage':('Manage price list and payers',0,1),
 'billing.shift.reconcile':('Reconcile cashier shifts',0,1),
 'report.view':('View management reports',0,0),'report.export':('Export reports',0,1),
 'telehealth.schedule':('Schedule telemedicine sessions',1,0),'telehealth.conduct':('Conduct telemedicine sessions',1,0),
 'comms.read':('View message delivery log',1,0),'comms.template.manage':('Manage notification templates',0,0),
 'admin.org.manage':('Organization settings and integrations',0,1),'admin.facility.manage':('Facilities, departments, service points',0,1),
 'admin.user.manage':('Staff accounts and role assignment',0,1),'admin.role.manage':('Define roles and permissions',0,1),
 'admin.access_review':('Run periodic access reviews',0,1),'audit.read':('Read audit trail',1,1),
 'break_glass.use':('Emergency access to a patient record',1,1),'privacy.request.manage':('Handle data-subject requests and legal holds',1,1),
}
# role: (name, parent, perms)
R = {
 'administrator':('System administrator',None,['admin.org.manage','admin.facility.manage','admin.user.manage','admin.role.manage','admin.access_review','comms.template.manage','diag.catalog.manage','pharmacy.catalog.manage','billing.price.manage','report.view']),
 'receptionist':('Reception / registration',None,['patient.read','patient.create','patient.update','appointment.read','appointment.manage','encounter.read','encounter.manage','queue.read','queue.manage','document.read','document.upload','billing.read','telehealth.schedule']),
 'nurse':('Nurse / triage',None,['patient.read','clinical.read','clinical.history.write','triage.write','encounter.read','encounter.manage','queue.read','queue.manage','appointment.read','diag.request.read','diag.result.read','document.read','document.upload','pharmacy.read']),
 'doctor':('Doctor / clinician','nurse',['consultation.write','diag.request.create','pharmacy.prescribe','telehealth.conduct','telehealth.schedule','appointment.manage','break_glass.use']),
 'lab_technician':('Laboratory technician',None,['patient.read','diag.request.read','diag.specimen.manage','diag.result.enter','diag.result.read','queue.read','queue.manage','document.upload','document.read']),
 'lab_supervisor':('Laboratory supervisor','lab_technician',['diag.result.verify','diag.catalog.manage','report.view']),
 'pharmacist':('Pharmacist',None,['patient.read','pharmacy.read','pharmacy.dispense','pharmacy.stock.manage','queue.read','queue.manage','billing.read']),
 'cashier':('Billing / cashier',None,['patient.read','billing.read','billing.invoice.manage','billing.payment.record','billing.refund.request','queue.read','queue.manage','encounter.read']),
 'billing_supervisor':('Billing supervisor','cashier',['billing.invoice.void','billing.payment.reverse','billing.refund.approve','billing.shift.reconcile','billing.price.manage','report.view','report.export']),
 'manager':('Management',None,['report.view','report.export','encounter.read','appointment.read','billing.read','queue.read']),
 'compliance_officer':('Privacy / compliance officer',None,['audit.read','admin.access_review','privacy.request.manage','patient.read','patient.export','report.view','comms.read']),
}
ICD = [('A09','Infectious gastroenteritis and colitis, unspecified'),('B50.9','Plasmodium falciparum malaria, unspecified'),
 ('B54','Unspecified malaria'),('E11.9','Type 2 diabetes mellitus without complications'),('I10','Essential (primary) hypertension'),
 ('J06.9','Acute upper respiratory infection, unspecified'),('J18.9','Pneumonia, unspecified organism'),('K29.7','Gastritis, unspecified'),
 ('N39.0','Urinary tract infection, site not specified'),('R50.9','Fever, unspecified'),('Z00.0','General adult medical examination'),
 ('O80','Single spontaneous delivery'),('A01.0','Typhoid fever'),('M54.5','Low back pain'),('L30.9','Dermatitis, unspecified')]
DC = [('patient_demographics','Patient identity, contacts, next of kin','phi','records_manager'),
 ('clinical_record','Triage, consultations, diagnoses, history, results, prescriptions','phi','medical_director'),
 ('patient_documents','Uploaded documents and images','phi','records_manager'),
 ('financial_record','Invoices, payments, receipts','phi','finance_manager'),
 ('audit_trail','Audit events and seals','phi_restricted','compliance_officer'),
 ('security_logs','Sessions, authentication challenges','confidential','it_security'),
 ('communications','Message delivery records','pii','operations_manager'),
 ('telemedicine_metadata','Session scheduling and connection events','phi','medical_director'),
 ('staff_accounts','Staff identities, memberships, roles','pii','hr_manager')]
q=lambda s: "'"+s.replace("'","''")+"'"
out=["-- GENERATED by tools/gen_seed.py — do not edit by hand.","-- 0011_reference_seed.sql: state machines, permissions, system roles, ICD-10 demo subset, data categories.",""]
out.append("INSERT INTO ref.state_transition (entity, from_status, to_status, required_perm) VALUES")
for k in TP: assert k[2] in T[k[0]][k[1]], k
rows=[f"  ({q(e)}, {q(f)}, {q(t)}, {q(TP[(e,f,t)]) if (e,f,t) in TP else 'NULL'})" for e,m in T.items() for f,ts in m.items() for t in ts]
out.append(",\n".join(rows)+"\nON CONFLICT DO NOTHING;\n")
out.append("INSERT INTO ref.permission (code, resource, action, description, is_phi, is_privileged) VALUES")
rows=[]
for c,(d,phi,pr) in P.items():
    res,act=c.split('.',1)
    rows.append(f"  ({q(c)}, {q(res)}, {q(act)}, {q(d)}, {str(bool(phi)).lower()}, {str(bool(pr)).lower()})")
out.append(",\n".join(rows)+"\nON CONFLICT (code) DO UPDATE SET description = EXCLUDED.description, is_phi = EXCLUDED.is_phi, is_privileged = EXCLUDED.is_privileged;\n")
for code,(name,parent,perms) in R.items():
    for p in perms: assert p in P,(code,p)
    out.append(f"INSERT INTO iam.role (org_id, code, name, is_system, parent_role_id) VALUES (NULL, {q(code)}, {q(name)}, true, "
               + (f"(SELECT id FROM iam.role WHERE org_id IS NULL AND code = {q(parent)})" if parent else "NULL") + ") ON CONFLICT DO NOTHING;")
out.append("")
out.append("INSERT INTO iam.role_permission (role_id, permission_code)\nSELECT r.id, v.perm FROM (VALUES")
rows=[f"  ({q(code)}, {q(p)})" for code,(_,_,perms) in R.items() for p in perms]
out.append(",\n".join(rows)+"\n) AS v(role_code, perm) JOIN iam.role r ON r.org_id IS NULL AND r.code = v.role_code\nON CONFLICT DO NOTHING;\n")
out.append("INSERT INTO ref.icd10_code (code, title) VALUES\n"+",\n".join(f"  ({q(c)}, {q(t)})" for c,t in ICD)+"\nON CONFLICT DO NOTHING;\n")
out.append("-- retention_period intentionally NULL: pending legal/business decision (see spec, Outstanding decisions).")
out.append("INSERT INTO gov.data_category (code, description, classification, owner_role) VALUES\n"+",\n".join(f"  ({q(a)}, {q(b)}, {q(c)}, {q(d)})" for a,b,c,d in DC)+"\nON CONFLICT DO NOTHING;")
pathlib.Path(__file__).resolve().parent.parent.joinpath('migrations/0011_reference_seed.sql').write_text("\n".join(out)+"\n")
print(len(T),"entities",sum(len(t) for m in T.values() for t in m.values()),"transitions",len(P),"perms",len(R),"roles")
