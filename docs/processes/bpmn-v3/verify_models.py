"""Verify the decisions that motivated the version-3 redesign."""
from pathlib import Path
from xml.etree import ElementTree as E
import json
import re
import sys

root=Path(sys.argv[1])
index=json.loads((root/'model-index.json').read_text('utf-8'))
models={m['id']:m for m in index['models']}
ns={'b':'http://www.omg.org/spec/BPMN/20100524/MODEL'}
checks=[]

def check(name, condition):
    assert condition, name
    checks.append(name)

check('18 distinct editable models', len(models)==18)
check('all 47 requirements retain their status',len(index['requirements'])==47 and all(x['status'] for x in index['requirements'].values()))
check('40 original checklist rows remain traceable',set(index['rows'])=={str(x) for x in range(2,42)})
overview=E.parse(root/'P00.bpmn').getroot()
check('eight independent template calendar waits',len(overview.findall('.//b:timerEventDefinition',ns))==8)
flow=models['P00']['edges']
check('calendar branches are not chained',len([e for e in flow if e['src']=='fork'])==8 and not any(e['src'].startswith('send') and not e['dst'].startswith('e') for e in flow))
check('no checklist completion condition in scheduling',not any(re.search('complet|mark|ready|approved',e['condition'],re.I) for pid in ('P00','P15') for e in models[pid]['edges']))
check('scheduled messages do not use model lane',all(n['role']!='M' for n in models['P15']['nodes'].values()))
check('constructor is not dependent on AI',all(n['role']!='M' for n in models['P01']['nodes'].values()))
check('role-specific content and assignments exist',all(k in models['P15']['nodes'] for k in ('targets','build','save')))
check('external link path is the default access path',any(e['src']=='mode' and e['dst']=='link' and e['condition']=='default' for e in models['P04']['edges']))
check('integrated access remains conditional',all('OQ08' in models[pid]['scope'] or 'OQ08' in models[pid]['open_questions'] for pid in ('P04','P05','P06','P07')))
check('opening a link never automatically marks readiness',any(e['src']=='mark' and e['dst']=='e' and e['condition']=='default' for e in models['P07']['edges']))
check('meeting results stay outside AI generation',all(n['role']!='M' for pid in ('P10','P11','P12') for n in models[pid]['nodes'].values()))
check('after-end reminder does not depend on hiring decision',not any('decision' in e['condition'].lower() for e in models['P13']['edges']))
check('RAG search follows permissions',any(e['src']=='filter' and e['dst']=='retrieve' for e in models['P14']['edges']))
check('private chat decision is closed','OQ01' not in models['P14']['open_questions'])
check('scenario library and draft return modelled',{'list','resume','autosave','replacement'} <= set(models['P01']['nodes']))
check('stable stage suggestions modelled',{'stage','selection','random','reuse','chips'} <= set(models['P14']['nodes']))
check('delivery failures terminate independently',{'e','wait','done'} <= {k for k,n in models['P16']['nodes'].items() if n['kind']=='end'})
for pid,m in models.items():
    xml=E.parse(root/(pid+'.bpmn')).getroot()
    check(pid+' is non-executable',xml.find('b:process',ns).get('isExecutable')=='false')
    actual={x.get('id') for x in xml.findall('.//b:process/*',ns)}
    check(pid+' node references exist',all(pid+'_'+n in actual for n in m['nodes']))
report={'passed':len(checks),'checks':checks,'models':len(models),'requirements':len(index['requirements'])}
(root/'semantic-checks.json').write_text(json.dumps(report,ensure_ascii=False,indent=2),'utf-8')
print(json.dumps(report,ensure_ascii=False))
