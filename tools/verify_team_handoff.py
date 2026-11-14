"""Stdlib-only traceability and contract integrity gate, also run in CI."""
from pathlib import Path
import json,re,sys
from xml.etree import ElementTree as E
R=Path(__file__).resolve().parents[1];D=R/'docs'
catalog=json.loads((D/'product/frontend-functions.json').read_text('utf-8'))['features']
api=json.loads((D/'architecture/openapi.yaml').read_text('utf-8'))
ops={op['operationId'] for path in api['paths'].values() for op in path.values()}
assert len(ops)==sum(len(p) for p in api['paths'].values()),'Duplicate operationId'
schemas=api['components']['schemas']
for ref in re.findall(r'"\$ref":\s*"([^"]+)"',json.dumps(api)):
    assert ref.startswith('#/components/schemas/') and ref.rsplit('/',1)[-1] in schemas,ref
ids=set();criteria=set();pids={f'P{i:02}' for i in range(18)}
for f in catalog:
    assert f['id'] not in ids;ids.add(f['id'])
    assert f['check'] not in criteria;criteria.add(f['check'])
    assert set(f['api'])<=ops,(f['id'],set(f['api'])-ops)
    assert set(f['bpmn'])<=pids
    assert f['behaviour'] and f['limits'] and f['actor']
for pid in pids:
    p=R/'output/bpmn-v3/models'/f'{pid}.bpmn';root=E.parse(p).getroot();allids={e.get('id') for e in root.iter()}
    for el in root.iter():
        for attr in ('sourceRef','targetRef','bpmnElement'):
            if el.get(attr):assert el.get(attr) in allids,(pid,attr,el.get(attr))
assert 'OQ01' not in json.loads((R/'output/bpmn-v3/models/model-index.json').read_text('utf-8'))['open_questions']
for rel in ('product/team-specification.md','product/frontend-functional-specification.md','architecture/server-handoff.md','product/traceability.csv'):
    assert (D/rel).is_file(),rel
assert (R/'output/ai-buddy-team-specification.docx').stat().st_size>100000
for doc in (R/'README.md',D/'architecture/server-handoff.md',R/'frontend/design/implementation-handoff.md'):
    for target in re.findall(r'\]\(([^)]+)\)',doc.read_text('utf-8')):
        if not target.startswith(('http:','https:','#')):assert (doc.parent/target.split('#')[0]).exists(),(doc,target)
report={'features':len(catalog),'proposed_api_operations':len(ops),'schemas':len(schemas),'bpmn':18,'status':'passed','note':'Structural gate; does not claim UI/server test execution or business correctness.'}
print(json.dumps(report,ensure_ascii=False))
