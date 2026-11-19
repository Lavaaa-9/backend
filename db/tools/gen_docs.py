"""Generates docs/data_dictionary.md and docs/erd/*.mmd from the live catalog (run against a migrated DB)."""
import subprocess, json, collections, pathlib, sys
DB = sys.argv[1] if len(sys.argv) > 1 else 'spartial_test'
ROOT = pathlib.Path(__file__).resolve().parent.parent
SCHEMAS = ['org','iam','patient','clinical','diag','pharmacy','billing','telehealth','document','comms','integration','gov','audit','ref']
def q(sql):
    out = subprocess.run(['psql','-X','-tA','-d',DB,'-c',f"SELECT coalesce(json_agg(zz),'[]') FROM ({sql}) zz"], capture_output=True, text=True, check=True).stdout
    return json.loads(out)
S = "('" + "','".join(SCHEMAS) + "')"
tables = q(f"""SELECT n.nspname s, c.relname t, obj_description(c.oid) cmt, c.relrowsecurity rls, c.relforcerowsecurity frc
  FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.relkind IN ('r','p') AND n.nspname IN {S}
  AND NOT c.relispartition ORDER BY 1,2""")
cols = q(f"""SELECT table_schema s, table_name t, column_name c, ordinal_position o,
  CASE WHEN data_type='USER-DEFINED' THEN udt_name WHEN data_type='ARRAY' THEN udt_name ELSE data_type END
   || coalesce('('||character_maximum_length||')','') || CASE WHEN data_type='numeric' AND numeric_precision IS NOT NULL THEN '('||numeric_precision||','||numeric_scale||')' ELSE '' END ty,
  is_nullable n, column_default d, is_generated g, generation_expression ge
  FROM information_schema.columns WHERE table_schema IN {S} ORDER BY 1,2,4""")
pk = q(f"""SELECT n.nspname s, c.relname t, a.attname col FROM pg_index i JOIN pg_class c ON c.oid=i.indrelid JOIN pg_namespace n ON n.oid=c.relnamespace
  JOIN pg_attribute a ON a.attrelid=c.oid AND a.attnum = ANY(i.indkey) WHERE i.indisprimary AND n.nspname IN {S}""")
fks = q(f"""SELECT n.nspname s, c.relname t, con.conname name, pg_get_constraintdef(con.oid) def, fn.nspname fs, f.relname ft,
  (SELECT array_agg(a.attname ORDER BY k.ord) FROM unnest(con.conkey) WITH ORDINALITY k(attnum,ord) JOIN pg_attribute a ON a.attrelid=con.conrelid AND a.attnum=k.attnum) cols,
  (SELECT bool_and(a.attnotnull) FROM unnest(con.conkey) k(attnum) JOIN pg_attribute a ON a.attrelid=con.conrelid AND a.attnum=k.attnum) mandatory
  FROM pg_constraint con JOIN pg_class c ON c.oid=con.conrelid JOIN pg_namespace n ON n.oid=c.relnamespace
  JOIN pg_class f ON f.oid=con.confrelid JOIN pg_namespace fn ON fn.oid=f.relnamespace
  WHERE con.contype='f' AND n.nspname IN {S} AND NOT c.relispartition ORDER BY 1,2,3""")
cons = q(f"""SELECT n.nspname s, c.relname t, con.conname name, con.contype ty, pg_get_constraintdef(con.oid) def
  FROM pg_constraint con JOIN pg_class c ON c.oid=con.conrelid JOIN pg_namespace n ON n.oid=c.relnamespace
  WHERE con.contype IN ('c','u','x') AND n.nspname IN {S} AND NOT c.relispartition ORDER BY 1,2,3""")
idx = q(f"""SELECT schemaname s, tablename t, indexname i, indexdef d FROM pg_indexes WHERE schemaname IN {S}
  AND tablename NOT LIKE 'event_2%' AND tablename <> 'event_default' ORDER BY 1,2,3""")
trg = q(f"""SELECT n.nspname s, c.relname t, string_agg(t.tgname, ', ' ORDER BY t.tgname) names FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid
  JOIN pg_namespace n ON n.oid=c.relnamespace WHERE NOT t.tgisinternal AND n.nspname IN {S} AND NOT c.relispartition GROUP BY 1,2""")
pol = q("""SELECT schemaname s, tablename t, string_agg(policyname || CASE WHEN permissive='RESTRICTIVE' THEN '(R)' ELSE '' END, ', ' ORDER BY policyname) p FROM pg_policies GROUP BY 1,2""")
P = collections.defaultdict(set); [P[(r['s'],r['t'])].add(r['col']) for r in pk]
C = collections.defaultdict(list); [C[(r['s'],r['t'])].append(r) for r in cols]
F = collections.defaultdict(list); [F[(r['s'],r['t'])].append(r) for r in fks]
K = collections.defaultdict(list); [K[(r['s'],r['t'])].append(r) for r in cons]
I = collections.defaultdict(list); [I[(r['s'],r['t'])].append(r) for r in idx]
TR = {(r['s'],r['t']): r['names'] for r in trg}; PO = {(r['s'],r['t']): r['p'] for r in pol}
fkcol = collections.defaultdict(dict)
for r in fks:
    for c in r['cols']: fkcol[(r['s'],r['t'])].setdefault(c, f"{r['fs']}.{r['ft']}")
md = ["# Spartial HMS — Data Dictionary", "", f"Generated from the live PostgreSQL 16 catalog of the migrated test database by `tools/gen_docs.py`. {len(tables)} tables.", "",
      "Legend: **PK** primary key · **FK→** foreign key target · NN = NOT NULL · (R) = restrictive RLS policy.", ""]
for sch in SCHEMAS:
    ts = [t for t in tables if t['s'] == sch]
    if not ts: continue
    md += [f"## Schema `{sch}`", ""]
    for t in ts:
        key = (t['s'], t['t'])
        md += [f"### `{sch}.{t['t']}`", ""]
        if t['cmt']: md += [t['cmt'], ""]
        md += [f"RLS: {'forced' if t['frc'] else ('enabled' if t['rls'] else 'off (no direct app access or global)')}"
               + (f" — policies: {PO.get(key)}" if PO.get(key) else "") + "  ", f"Triggers: {TR.get(key, '—')}", "",
               "| Column | Type | Null | Default / generated | Key |", "|---|---|---|---|---|"]
        for c in C[key]:
            d = (f"GENERATED: `{c['ge']}`" if c['g'] == 'ALWAYS' else (f"`{c['d']}`" if c['d'] else '')).replace('|','\\|')
            k = ' '.join(x for x in ['**PK**' if c['c'] in P[key] else '', f"FK→ `{fkcol[key][c['c']]}`" if c['c'] in fkcol[key] else ''] if x)
            md.append(f"| `{c['c']}` | {c['ty']} | {'NN' if c['n']=='NO' else 'null'} | {d} | {k} |")
        if K[key]:
            md += ["", "Constraints:", ""] + [f"- `{x['name']}` ({ {'c':'check','u':'unique','x':'exclusion'}[x['ty']] }): `{x['def'].replace('|','\\|')}`" for x in K[key]]
        if F[key]:
            md += ["", "Foreign keys:", ""] + [f"- `{x['name']}`: `{x['def']}`" for x in F[key]]
        ix = [x for x in I[key]]
        if ix: md += ["", "Indexes:", ""] + [f"- `{x['i']}`: `{x['d'].split(' USING ')[-1] if ' USING ' in x['d'] else x['d']}`" + (" (unique)" if 'UNIQUE' in x['d'] else '') for x in ix]
        md.append("")
(ROOT/'docs').mkdir(exist_ok=True)
(ROOT/'docs/data_dictionary.md').write_text("\n".join(md))
# ERDs per domain (entities shown with PK/FK columns only, for legibility)
erd = ROOT/'docs/erd'; erd.mkdir(exist_ok=True)
DOM = {'identity_org':['org','iam'],'patient':['patient','document','gov'],'clinical':['clinical','telehealth'],
       'diagnostics_pharmacy':['diag','pharmacy'],'billing':['billing'],'platform':['comms','integration','audit']}
SKIP_TARGET = {('iam','user_account')}   # created_by/updated_by everywhere; omitted for legibility
for name, schs in DOM.items():
    lines = ["erDiagram"]; ents = set()
    for t in tables:
        if t['s'] not in schs: continue
        key=(t['s'],t['t']); ents.add(key)
        attrs=[]
        for c in C[key]:
            if c['c'] in P[key] or (c['c'] in fkcol[key] and tuple(fkcol[key][c['c']].split('.')) not in SKIP_TARGET) or c['c'] in ('status','org_id'):
                tag = 'PK' if c['c'] in P[key] else ('FK' if c['c'] in fkcol[key] else '')
                attrs.append(f"        {c['ty'].split('(')[0].replace(' ','_')} {c['c']} {tag}".rstrip())
        lines.append(f"    {t['s']}_{t['t']} {{"); lines += attrs; lines.append("    }")
    seen=set()
    for r in fks:
        if r['s'] not in schs or (r['fs'],r['ft']) in SKIP_TARGET: continue
        if r['ft'] == 'organization' and name != 'identity_org': continue
        a=f"{r['fs']}_{r['ft']}"; b=f"{r['s']}_{r['t']}"
        if (a,b) in seen: continue
        seen.add((a,b))
        if (r['fs'],r['ft']) not in ents: lines.append(f"    {a} {{\n    }}")
        card = '||--o{' if r['mandatory'] else '|o--o{'
        lines.append(f"    {a} {card} {b} : \"{','.join(r['cols']).replace('org_id,','')}\"")
    (erd/f"{name}.mmd").write_text("\n".join(lines)+"\n")
print(len(tables), "tables documented;", sum(len(v) for v in C.values()), "columns")
