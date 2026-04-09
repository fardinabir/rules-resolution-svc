-- migrations/dml/001_seed_data.sql
-- Idempotent seed: steps, defaults, overrides, override_history.
-- Skips entirely if the steps table already has rows (guard against re-seeding).

DO $$
BEGIN
  IF (SELECT COUNT(*) FROM steps) > 0 THEN
    RAISE NOTICE 'Seed data already present — skipping DML seed.';
    RETURN;
  END IF;

  -- ── Steps ────────────────────────────────────────────────────────────────────
  INSERT INTO steps (key, name, description, position) VALUES
    ('title-search',    'Title Search',    'Research property ownership, liens, encumbrances, and tax status. Verify chain of title.',                                  1),
    ('file-complaint',  'File Complaint',  'Prepare and file the foreclosure complaint (judicial) or notice of default (non-judicial) with the court.',                 2),
    ('serve-borrower',  'Serve Borrower',  'Serve the borrower and all named defendants with the complaint and summons via process server.',                             3),
    ('obtain-judgment', 'Obtain Judgment', 'Obtain a judgment of foreclosure from the court authorizing the sale of the property.',                                     4),
    ('schedule-sale',   'Schedule Sale',   'Schedule the foreclosure sale date, coordinate publication requirements, and notify all parties.',                          5),
    ('conduct-sale',    'Conduct Sale',    'Conduct the foreclosure auction, process bids, and file the certificate of sale.',                                           6);

  -- ── Defaults (6 steps × 6 traits = 36 rows) ─────────────────────────────────
  INSERT INTO defaults (step_key, trait_key, value) VALUES
    ('title-search',    'slaHours',          '720'),
    ('title-search',    'requiredDocuments', '["title_commitment","tax_certificate"]'),
    ('title-search',    'feeAmount',         '35000'),
    ('title-search',    'feeAuthRequired',   'false'),
    ('title-search',    'assignedRole',      '"processor"'),
    ('title-search',    'templateId',        '"title-review-standard-v1"'),

    ('file-complaint',  'slaHours',          '480'),
    ('file-complaint',  'requiredDocuments', '["complaint","summons","lis_pendens","cover_sheet"]'),
    ('file-complaint',  'feeAmount',         '65000'),
    ('file-complaint',  'feeAuthRequired',   'false'),
    ('file-complaint',  'assignedRole',      '"attorney"'),
    ('file-complaint',  'templateId',        '"complaint-standard-v1"'),

    ('serve-borrower',  'slaHours',          '2880'),
    ('serve-borrower',  'requiredDocuments', '["affidavit_of_service","return_of_service"]'),
    ('serve-borrower',  'feeAmount',         '25000'),
    ('serve-borrower',  'feeAuthRequired',   'false'),
    ('serve-borrower',  'assignedRole',      '"processor"'),
    ('serve-borrower',  'templateId',        '"service-standard-v1"'),

    ('obtain-judgment', 'slaHours',          '4320'),
    ('obtain-judgment', 'requiredDocuments', '["motion_for_judgment","affidavit_of_indebtedness","proposed_judgment"]'),
    ('obtain-judgment', 'feeAmount',         '45000'),
    ('obtain-judgment', 'feeAuthRequired',   'false'),
    ('obtain-judgment', 'assignedRole',      '"attorney"'),
    ('obtain-judgment', 'templateId',        '"judgment-standard-v1"'),

    ('schedule-sale',   'slaHours',          '1440'),
    ('schedule-sale',   'requiredDocuments', '["notice_of_sale","publication_proof"]'),
    ('schedule-sale',   'feeAmount',         '30000'),
    ('schedule-sale',   'feeAuthRequired',   'false'),
    ('schedule-sale',   'assignedRole',      '"processor"'),
    ('schedule-sale',   'templateId',        '"sale-notice-standard-v1"'),

    ('conduct-sale',    'slaHours',          '720'),
    ('conduct-sale',    'requiredDocuments', '["certificate_of_sale","sale_report"]'),
    ('conduct-sale',    'feeAmount',         '50000'),
    ('conduct-sale',    'feeAuthRequired',   'false'),
    ('conduct-sale',    'assignedRole',      '"attorney"'),
    ('conduct-sale',    'templateId',        '"sale-report-standard-v1"');

  -- ── Overrides (49 rows) ───────────────────────────────────────────────────────
  -- Columns: id, step_key, trait_key, state, client, investor, case_type,
  --          specificity, value, effective_date, expires_date,
  --          status, description, created_at, created_by, updated_at, updated_by

  -- State-only overrides (specificity 1)
  INSERT INTO overrides
    (id, step_key, trait_key, state, client, investor, case_type, specificity, value, effective_date, expires_date, status, description, created_at, created_by, updated_at, updated_by)
  VALUES
    ('ovr-001','file-complaint','slaHours','FL',NULL,NULL,NULL,1,'360','2025-01-01',NULL,'active','Florida filing deadline — 15 days (shorter than default 20)',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-002','file-complaint','requiredDocuments','FL',NULL,NULL,NULL,1,'["complaint","summons","lis_pendens","cover_sheet","verification_of_complaint"]','2025-01-01',NULL,'active','Florida requires verification of complaint',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-003','serve-borrower','slaHours','FL',NULL,NULL,NULL,1,'2160','2025-01-01',NULL,'active','Florida 90-day service window (vs 120 default)',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-004','schedule-sale','requiredDocuments','FL',NULL,NULL,NULL,1,'["notice_of_sale","publication_proof","owners_affidavit"]','2025-01-01',NULL,'active','Florida requires owner''s affidavit for sale scheduling',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-005','file-complaint','slaHours','TX',NULL,NULL,NULL,1,'336','2025-01-01',NULL,'active','Texas filing deadline — 14 days',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-006','file-complaint','requiredDocuments','TX',NULL,NULL,NULL,1,'["complaint","summons","cover_sheet","notice_of_acceleration","notice_to_cure"]','2025-01-01',NULL,'active','Texas requires notice of acceleration and notice to cure',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-007','serve-borrower','slaHours','TX',NULL,NULL,NULL,1,'1440','2025-01-01',NULL,'active','Texas 60-day service window',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-008','file-complaint','slaHours','NY',NULL,NULL,NULL,1,'720','2025-01-01',NULL,'active','New York filing deadline — 30 days (longer, more complex process)',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-009','file-complaint','requiredDocuments','NY',NULL,NULL,NULL,1,'["complaint","summons","lis_pendens","cover_sheet","rpapl_1303_notice","90_day_notice","certificate_of_merit"]','2025-01-01',NULL,'active','New York requires RPAPL 1303 notice, 90-day notice, and certificate of merit',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-010','obtain-judgment','slaHours','NY',NULL,NULL,NULL,1,'8640','2025-01-01',NULL,'active','New York judgment timeline — 360 days (court backlog)',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-011','serve-borrower','slaHours','IL',NULL,NULL,NULL,1,'2160','2025-01-01',NULL,'active','Illinois 90-day service window',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-012','title-search','feeAmount','NY',NULL,NULL,NULL,1,'45000','2025-01-01',NULL,'active','New York title searches cost more (complex recording system)',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-013','file-complaint','feeAmount','NY',NULL,NULL,NULL,1,'85000','2025-01-01',NULL,'active','New York filing fees higher due to additional required filings',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-014','conduct-sale','templateId','FL',NULL,NULL,NULL,1,'"sale-report-fl-v2"','2025-01-01',NULL,'active','Florida-specific sale report template (includes surplus funds disclosure)',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-015','obtain-judgment','requiredDocuments','OH',NULL,NULL,NULL,1,'["motion_for_judgment","affidavit_of_indebtedness","proposed_judgment","military_affidavit","affidavit_of_compliance"]','2025-01-01',NULL,'active','Ohio requires military affidavit and compliance affidavit for judgment',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-049','schedule-sale','slaHours','FL',NULL,NULL,NULL,1,'1080','2025-01-01',NULL,'active','Florida sale scheduling — 45-day window (tighter than default 60)',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-050','conduct-sale','requiredDocuments','FL',NULL,NULL,NULL,1,'["certificate_of_sale","sale_report","surplus_funds_disclosure"]','2025-01-01',NULL,'active','Florida requires surplus funds disclosure at sale',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-052','title-search','requiredDocuments','IL',NULL,NULL,NULL,1,'["title_commitment","tax_certificate","municipal_lien_search"]','2025-01-01',NULL,'active','Illinois requires municipal lien search with title',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-054','serve-borrower','feeAmount','NY',NULL,NULL,NULL,1,'35000','2025-01-01',NULL,'active','New York service fees higher (process server costs)',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-055','title-search','slaHours','OH',NULL,NULL,NULL,1,'504','2025-01-01',NULL,'active','Ohio title searches — 21-day deadline',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-056','file-complaint','slaHours','OH',NULL,NULL,NULL,1,'480','2025-01-01',NULL,'active','Ohio filing deadline — 20 days (same as default but explicit)',NOW(),'admin@pearsonspecter.com',NOW(),'seed');

  -- Client-only overrides (specificity 1)
  INSERT INTO overrides
    (id, step_key, trait_key, state, client, investor, case_type, specificity, value, effective_date, expires_date, status, description, created_at, created_by, updated_at, updated_by)
  VALUES
    ('ovr-030','title-search','feeAuthRequired',NULL,'Chase',NULL,NULL,1,'true','2025-01-01',NULL,'active','Chase requires fee authorization for all steps (global policy)',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-031','file-complaint','feeAuthRequired',NULL,'Chase',NULL,NULL,1,'true','2025-01-01',NULL,'active','Chase requires fee authorization for all steps',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-032','serve-borrower','feeAuthRequired',NULL,'Chase',NULL,NULL,1,'true','2025-01-01',NULL,'active','Chase requires fee authorization for all steps',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-033','obtain-judgment','feeAuthRequired',NULL,'Chase',NULL,NULL,1,'true','2025-01-01',NULL,'active','Chase requires fee authorization for all steps',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-045','schedule-sale','feeAuthRequired',NULL,'Chase',NULL,NULL,1,'true','2025-01-01',NULL,'active','Chase requires fee auth for sale scheduling',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-046','conduct-sale','feeAuthRequired',NULL,'Chase',NULL,NULL,1,'true','2025-01-01',NULL,'active','Chase requires fee auth for sale conduct',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-024','title-search','templateId',NULL,'WellsFargo',NULL,NULL,1,'"title-review-wf-v2"','2025-03-01',NULL,'active','Wells Fargo custom title review template (all states)',NOW(),'admin@pearsonspecter.com',NOW(),'seed');

  -- Investor-only overrides (specificity 1)
  INSERT INTO overrides
    (id, step_key, trait_key, state, client, investor, case_type, specificity, value, effective_date, expires_date, status, description, created_at, created_by, updated_at, updated_by)
  VALUES
    ('ovr-038','file-complaint','requiredDocuments',NULL,NULL,'FHA',NULL,1,'["complaint","summons","lis_pendens","cover_sheet","hud_face_sheet"]','2025-03-01',NULL,'active','FHA loans require HUD face sheet in all states',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-039','file-complaint','requiredDocuments',NULL,NULL,'VA',NULL,1,'["complaint","summons","lis_pendens","cover_sheet","va_loan_summary","va_appraisal"]','2025-03-01',NULL,'active','VA loans require VA loan summary and appraisal',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-040','obtain-judgment','assignedRole',NULL,NULL,'FHA',NULL,1,'"attorney"','2025-01-01',NULL,'active','FHA judgment motions always require attorney handling',NOW(),'admin@pearsonspecter.com',NOW(),'seed');

  -- caseType-only overrides (specificity 1)
  INSERT INTO overrides
    (id, step_key, trait_key, state, client, investor, case_type, specificity, value, effective_date, expires_date, status, description, created_at, created_by, updated_at, updated_by)
  VALUES
    ('ovr-043','obtain-judgment','slaHours',NULL,NULL,NULL,'FC-NonJudicial',1,'0','2025-01-01',NULL,'active','Non-judicial cases skip the judgment step (no court judgment needed)',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-044','obtain-judgment','feeAmount',NULL,NULL,NULL,'FC-NonJudicial',1,'0','2025-01-01',NULL,'active','No fee for judgment in non-judicial (step is skipped)',NOW(),'admin@pearsonspecter.com',NOW(),'seed');

  -- State+Client overrides (specificity 2)
  INSERT INTO overrides
    (id, step_key, trait_key, state, client, investor, case_type, specificity, value, effective_date, expires_date, status, description, created_at, created_by, updated_at, updated_by)
  VALUES
    ('ovr-020','file-complaint','slaHours','FL','Chase',NULL,NULL,2,'240','2025-06-01',NULL,'active','Chase in Florida — aggressive 10-day filing deadline',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-021','file-complaint','slaHours','FL','WellsFargo',NULL,NULL,2,'336','2025-06-01',NULL,'active','Wells Fargo in Florida — 14-day filing deadline',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-022','title-search','feeAmount','NY','Chase',NULL,NULL,2,'42500','2025-06-01',NULL,'active','Chase negotiated a lower NY title search fee',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-023','serve-borrower','templateId','FL','WellsFargo',NULL,NULL,2,'"service-fl-wf-v2"','2025-06-01',NULL,'active','Wells Fargo Florida service template (custom affidavit format)',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-025','file-complaint','templateId','FL','Chase',NULL,NULL,2,'"complaint-fl-chase-v2"','2025-06-01',NULL,'active','Chase Florida complaint template',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-026','obtain-judgment','slaHours','FL','Chase',NULL,NULL,2,'2880','2025-06-01',NULL,'active','Chase Florida judgment — 120-day target (Chase pushes harder)',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-048','title-search','slaHours','FL','Nationstar',NULL,NULL,2,'480','2025-06-01',NULL,'active','Nationstar Florida title searches — 20-day deadline (Nationstar wants faster turnaround)',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-051','file-complaint','slaHours','IL','WellsFargo',NULL,NULL,2,'360','2025-06-01',NULL,'active','Wells Fargo Illinois filing — 15-day deadline',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-053','file-complaint','feeAmount','FL','Chase',NULL,NULL,2,'60000','2025-06-01',NULL,'active','Chase Florida filing fee — negotiated rate',NOW(),'admin@pearsonspecter.com',NOW(),'seed');

  -- State+caseType overrides (specificity 2)
  INSERT INTO overrides
    (id, step_key, trait_key, state, client, investor, case_type, specificity, value, effective_date, expires_date, status, description, created_at, created_by, updated_at, updated_by)
  VALUES
    ('ovr-041','serve-borrower','requiredDocuments','TX',NULL,NULL,'FC-NonJudicial',2,'["notice_of_substitute_trustee_sale","affidavit_of_mailing","certified_mail_receipt"]','2025-01-01',NULL,'active','Texas non-judicial — different service documents (trustee sale notice, not personal service)',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-042','file-complaint','slaHours','TX',NULL,NULL,'FC-NonJudicial',2,'240','2025-01-01',NULL,'active','Texas non-judicial filing — faster timeline (no court involvement)',NOW(),'admin@pearsonspecter.com',NOW(),'seed');

  -- State+Client+Investor overrides (specificity 3)
  INSERT INTO overrides
    (id, step_key, trait_key, state, client, investor, case_type, specificity, value, effective_date, expires_date, status, description, created_at, created_by, updated_at, updated_by)
  VALUES
    ('ovr-034','file-complaint','slaHours','FL','Chase','FHA',NULL,3,'168','2025-09-01',NULL,'active','FHA loans via Chase in Florida — 7-day filing deadline (FHA acceleration timeline)',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-035','file-complaint','feeAmount','FL','Chase','FHA',NULL,3,'55000','2025-09-01',NULL,'active','FHA Chase Florida filing — reduced fee (high volume discount)',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-036','file-complaint','requiredDocuments','FL','Chase','FHA',NULL,3,'["complaint","summons","lis_pendens","cover_sheet","verification_of_complaint","hud_face_sheet","fha_servicing_history"]','2025-09-01',NULL,'active','FHA Chase Florida requires HUD face sheet and FHA servicing history in addition to FL requirements',NOW(),'admin@pearsonspecter.com',NOW(),'seed'),
    ('ovr-037','file-complaint','templateId','FL','Chase','FHA',NULL,3,'"complaint-fl-chase-fha-v3"','2025-09-01',NULL,'active','FHA-specific complaint template for Chase in Florida',NOW(),'admin@pearsonspecter.com',NOW(),'seed');

  -- State+Client+Investor+caseType overrides (specificity 4)
  INSERT INTO overrides
    (id, step_key, trait_key, state, client, investor, case_type, specificity, value, effective_date, expires_date, status, description, created_at, created_by, updated_at, updated_by)
  VALUES
    ('ovr-047','file-complaint','templateId','FL','Chase','FannieMae','FC-Judicial',4,'"complaint-fl-chase-fnma-judicial-v3"','2025-11-01',NULL,'active','Fannie Mae judicial foreclosure via Chase in Florida — uses specific complaint template with FNMA language',NOW(),'admin@pearsonspecter.com',NOW(),'seed');

  -- ── Override history (one "created" entry per override) ───────────────────────
  INSERT INTO override_history (override_id, action, changed_by, changed_at, snapshot_after)
  SELECT
    id,
    'created',
    created_by,
    created_at,
    jsonb_build_object('id', id, 'stepKey', step_key, 'traitKey', trait_key, 'status', status, 'createdBy', created_by)
  FROM overrides;

  RAISE NOTICE 'Seed complete: 6 steps, 36 defaults, 49 overrides.';
END;
$$;
